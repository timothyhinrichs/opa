// Copyright 2020 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

// Package linter contains utilities for linting Rego files.
package linter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/open-policy-agent/opa/loader"
	"github.com/open-policy-agent/opa/metrics"

	"github.com/open-policy-agent/opa/bundle"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/storage/inmem"
)

// Runner implements simple test discovery and execution.
type Runner struct {
	compiler    *ast.Compiler
	store       storage.Store
	runtime     *ast.Term
	failureLine bool
	timeout     time.Duration
	modules     map[string]*ast.Module
	bundles     map[string]*bundle.Bundle
	filter      string
	query       string
}

// NewRunner returns a new runner.
func NewRunner() *Runner {
	return &Runner{
		timeout: 5 * time.Second,
	}
}

// SetCompiler sets the compiler used by the runner.
func (r *Runner) SetCompiler(compiler *ast.Compiler) *Runner {
	r.compiler = compiler
	return r
}

// SetStore sets the store to execute tests over.
func (r *Runner) SetStore(store storage.Store) *Runner {
	r.store = store
	return r
}

// EnableFailureLine if set will provide the exact failure line
func (r *Runner) EnableFailureLine(yes bool) *Runner {
	r.failureLine = yes
	return r
}

// SetRuntime sets runtime information to expose to the evaluation engine.
func (r *Runner) SetRuntime(term *ast.Term) *Runner {
	r.runtime = term
	return r
}

// SetTimeout sets the timeout for the individual test cases
func (r *Runner) SetTimeout(timout time.Duration) *Runner {
	r.timeout = timout
	return r
}

// SetModules will add modules to the Runner which will be compiled then used
// for discovering and evaluating tests.
func (r *Runner) SetModules(modules map[string]*ast.Module) *Runner {
	r.modules = modules
	return r
}

// SetBundles will add bundles to the Runner which will be compiled then used
// for discovering and evaluating tests.
func (r *Runner) SetBundles(bundles map[string]*bundle.Bundle) *Runner {
	r.bundles = bundles
	return r
}

// Filter will set a test name regex filter for the test runner. Only test
// cases which match the filter will be run.
func (r *Runner) Filter(regex string) *Runner {
	r.filter = regex
	return r
}

// SetQuery controls the Rego entry point that returns the linting results
//    By default it is data.system.lint.deny
func (r *Runner) SetQuery(query string) *Runner {
	r.query = query
	return r
}

// Compile takes the provided modules and bundles and compiles them
func (r *Runner) Compile(ctx context.Context, txn storage.Transaction) error {
	// var testRegex *regexp.Regexp
	// if r.filter != "" {
	// 	var err error
	// 	testRegex, err = regexp.Compile(r.filter)
	// 	if err != nil {
	// 		return err
	// 	}
	// }

	if r.compiler == nil {
		r.compiler = ast.NewCompiler()
	}

	if r.store == nil {
		r.store = inmem.New()
	}

	if r.bundles != nil && len(r.bundles) > 0 {
		if txn == nil {
			return fmt.Errorf("unable to activate bundles: storage transaction is nil")
		}

		// Activate the bundle(s) to get their info and policies into the store
		// the actual compiled policies will overwritten later..
		opts := &bundle.ActivateOpts{
			Ctx:      ctx,
			Store:    r.store,
			Txn:      txn,
			Compiler: r.compiler,
			Metrics:  metrics.New(),
			Bundles:  r.bundles,
		}
		err := bundle.Activate(opts)
		if err != nil {
			return err
		}

		// Aggregate the bundle modules with other ones provided
		if r.modules == nil {
			r.modules = map[string]*ast.Module{}
		}
		for path, b := range r.bundles {
			for name, mod := range b.ParsedModules(path) {
				r.modules[name] = mod
			}
		}
	}

	if r.modules != nil && len(r.modules) > 0 {
		if r.compiler.Compile(r.modules); r.compiler.Failed() {
			return r.compiler.Errors
		}
	}
	return nil
}

// PrintParsed prints to stdout the JSON representing the parsed and compiled Rego that is loaded
func (r *Runner) PrintParsed() error {
	bs, err := json.MarshalIndent(r.compiler.Modules, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(bs))
	return nil
}

// Lint returns the results of running data.system.lint on the remainder of the rules
func (r *Runner) Lint(ctx context.Context, txn storage.Transaction) error {

	// evaluate JSON rego using data.system.lint
	rego := rego.New(
		rego.Store(r.store),
		rego.Transaction(txn),
		rego.Compiler(r.compiler),
		rego.Input(r.compiler.Modules),
		rego.Query(r.query),
		// rego.QueryTracer(tracer),
		rego.Runtime(r.runtime),
	)

	// t0 := time.Now()
	rs, err := rego.Eval(ctx)
	// dt := time.Since(t0)
	if err != nil {
		fmt.Printf("error: %v", err)
	}

	bs, err := json.MarshalIndent(rs, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(bs))
	return nil
}

// Load returns modules and an in-memory store for running tests.
func Load(args []string, filter loader.Filter) (map[string]*ast.Module, storage.Store, error) {
	loaded, err := loader.NewFileLoader().Filtered(args, filter)
	if err != nil {
		return nil, nil, err
	}
	store := inmem.NewFromObject(loaded.Documents)
	modules := map[string]*ast.Module{}
	ctx := context.Background()
	err = storage.Txn(ctx, store, storage.WriteParams, func(txn storage.Transaction) error {
		for _, loadedModule := range loaded.Modules {
			modules[loadedModule.Name] = loadedModule.Parsed

			// Add the policies to the store to ensure that any future bundle
			// activations will preserve them and re-compile the module with
			// the bundle modules.
			err := store.UpsertPolicy(ctx, txn, loadedModule.Name, loadedModule.Raw)
			if err != nil {
				return err
			}
		}
		return nil
	})
	return modules, store, err
}

// LoadBundles will load the given args as bundles, either tarball or directory is OK.
func LoadBundles(args []string, filter loader.Filter) (map[string]*bundle.Bundle, error) {
	bundles := map[string]*bundle.Bundle{}
	for _, bundleDir := range args {
		b, err := loader.NewFileLoader().WithSkipBundleVerification(true).AsBundle(bundleDir)
		if err != nil {
			return nil, fmt.Errorf("unable to load bundle %s: %s", bundleDir, err)
		}
		bundles[bundleDir] = b
	}

	return bundles, nil
}

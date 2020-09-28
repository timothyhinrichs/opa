// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/bundle"
	"github.com/open-policy-agent/opa/internal/runtime"
	"github.com/open-policy-agent/opa/linter"
	"github.com/open-policy-agent/opa/storage"
	"github.com/open-policy-agent/opa/storage/inmem"
)

// const (
// 	lintPrettyOutput = "pretty"
// 	lintJSONOutput   = "json"
// )

type lintCommandParams struct {
	errLimit int
	// outputFormat *util.EnumFlag
	timeout     time.Duration
	ignore      []string
	bundleMode  bool
	printParsed bool
	query       string
}

func newLintCommandParams() *lintCommandParams {
	return &lintCommandParams{
		// outputFormat: util.NewEnumFlag(lintPrettyOutput, []string{lintPrettyOutput, lintJSONOutput}),
		// explain:      newExplainFlag([]string{explainModeFails, explainModeFull, explainModeNotes}),
	}
}

var lintParams = newLintCommandParams()

var lintCommand = &cobra.Command{
	Use:   "lint",
	Short: "Lint Rego code",
	Long: `Lint Rego code.

The 'lint' command takes a file or directory path as input and executes the linter
against all matching files.  The linter is a collection of Rego files that return
error messages in a prescribed format when fed the parsed representation in JSON
of all the loaded files.

If the '--bundle' option is specified the paths will be treated as policy bundles
and loaded following standard bundle conventions. The path can be a compressed archive
file or a directory which will be treated as a bundle. Without the '--bundle' flag OPA
will recursively load ALL *.rego, *.json, and *.yaml files for linting.

Example policy (example/authz.rego):

	package authz

	allow {
		input.path = ["users"]
		input.method = "POST"
	}

	allow {
		input.path = ["users", profile_id]
		input.method = "GET"
		profile_id = input.user_id
	}

Example Linter file (lint/foo.rego):

    deny[err] {
		input.....
		err := {"message": "allow rules must use only helpers",
	            "location": input.location}
	}

Example lint run:

	$ opa lint ./example/

`,
	PreRunE: func(Cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("specify at least one file")
		}

		return nil
	},

	Run: func(cmd *cobra.Command, args []string) {
		os.Exit(opaLint(args))
	},
}

func opaLint(args []string) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	filter := loaderFilter{
		Ignore: lintParams.ignore,
	}

	var modules map[string]*ast.Module
	var bundles map[string]*bundle.Bundle
	var store storage.Store
	var err error

	if lintParams.bundleMode {
		bundles, err = linter.LoadBundles(args, filter.Apply)
		store = inmem.New()
	} else {
		modules, store, err = linter.Load(args, filter.Apply)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	txn, err := store.NewTransaction(ctx, storage.WriteParams)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	defer store.Abort(ctx, txn)

	compiler := ast.NewCompiler().
		SetErrorLimit(lintParams.errLimit).
		WithPathConflictsCheck(storage.NonEmpty(ctx, store, txn))

	info, err := runtime.Term(runtime.Params{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	runner := linter.NewRunner().
		SetCompiler(compiler).
		SetStore(store).
		SetRuntime(info).
		SetModules(modules).
		SetBundles(bundles).
		SetTimeout(lintParams.timeout).
		SetQuery(lintParams.query)

	return lint(ctx, txn, runner)
}

func lint(ctx context.Context, txn storage.Transaction, runner *linter.Runner) int {
	err := runner.Compile(ctx, txn)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if lintParams.printParsed {
		runner.PrintParsed()
	}

	err = runner.Lint(ctx, txn)

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	return 0
}

func init() {
	lintCommand.Flags().DurationVarP(&lintParams.timeout, "timeout", "t", time.Second*5, "set timeout")
	lintCommand.Flags().BoolVar(&lintParams.printParsed, "printParsed", false, "print parsed results")
	lintCommand.Flags().StringVarP(&lintParams.query, "query", "q", "data.system.lint.deny", "query to treat as entry point")
	addBundleModeFlag(lintCommand.Flags(), &lintParams.bundleMode, false)
	addMaxErrorsFlag(lintCommand.Flags(), &lintParams.errLimit)
	RootCommand.AddCommand(lintCommand)
}

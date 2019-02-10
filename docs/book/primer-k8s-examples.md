# Kubernetes Policies by Example

In Kubernetes, Admission Controllers enforce semantic validation of objects during create, update, and delete operations. With OPA you can enforce custom policies on Kubernetes objects without writing your own custom Admission Controllers.

This primer assumes you, the Kubernetes administrator, have already installed OPA as a validating admission controller on Kubernetes as described in the [OPA tutorial](https://www.openpolicyagent.org/docs/kubernetes-admission-control.html).   This guide helps you write policies through a series of examples and explains language constructs along the way.

## Overview
To get started, let's look at a common policy: ensure all images come from a trusted registry.

```
1: package kubernetes.admission
2: deny[msg] {
3:     input.request.kind.kind == "Pod"
4:     image := input.request.object.spec.containers[_].image
5:     not startswith(image, "hooli.com")
6:     msg := sprintf("image fails to come from trusted registry: %v", [image])
7: }
```

Here are a few notes explaining each line above.

* On line 1, the `package` declaration gives a (hierarchical) name `kubernetes.admission` to the rules in the remainder of the file.
* At line 2, the *head* of the rule `deny[msg]` says that the admission control request should be rejected and the user handed the error message `msg` for each of the image names that come from a registry other than "hooli.com".
* At lines 3-4, `input` is a reserved, global variable.  It is assigned a Kubernetes AdmissionReview object, which includes many fields.  The rule above uses `request.kind`, which includes the usual version/group/kind information about the object.  It also uses `request.object`, which is the YAML that the user provided to `kubectl` (augmented with defaults, timestamps, etc.).
* `input.request.kind.kind` does the obvious thing: descending through the YAML hierarchy.  The dot (.) operator never throws any errors; if the path does not exist the value of the expression is `undefined`.
* Lines 4-5 find images in the Pod that don't come from the trusted registry.  To do that, line 4 iterates over all containers with `input.request.object.spec.containers[_]`.  When you include a variable (like underscore `_`) in the path, OPA finds values for that variable that make all the conditions in the rule true.
* On line 5 the *builtin* `startswith` checks if one string is a prefix of the other.  The builtin `sprintf` on line 6 formats a string with arguments.  OPA has 50+ builtins detailed at [openpolicyagent.org/docs/language-reference.html](https://openpolicyagent.org/docs/language-reference.html).

When you write policies, you should use the OPA unit-test framework *before* sending the policies out into the OPA that is running on your cluster.  The debugging process will be much quicker and effective.

```
 1: package kubernetes.test_admission
 2: import data.kubernetes.admission
 3: unsafe_image = {"request": {
 4:     "kind": {"kind": "Pod"},
 5:     "object": {"spec": {"containers": [
 6:         {"image": "hooli.com/nginx"},
 7:         {"image": "busybox"}
 8:     ]}}
 9: }}
10: test_image_safety {
11:     count(admission.deny) == 1 with input as unsafe_image
12: }
```

* On line 2 `import data.kubernetes.admission` allows us to reference that package using the name `admission` everwhere in the package.  `import` is not strictly necessary--it simply sets up an alias; you could instead reference `data.kubernetes.admission` inside the rules.
* On line 3 `unsafe_image` is the sample input we have for OPA.  Often this is a real AdmissionReview, though those are so long that here we just hand-rolled a sample input.
* On line 10 `test_image_safety` defines a unittest.  If the rule evaluates to true the test passes; otherwise it fails.  When you use the OPA test runner, anything starting with `test` is treated as a test.
* On line 11 `admission.deny` runs the `deny` rule shown above (and all other `deny` rules in the `admission` package).
* Also on line 11 the stanza `with input as unsafe_image` sets the value of `input` to be `unsafe_image` while evaluating `count(admission.deny) == 1`.

Different packages must go into different files.  If you've created the files *image-safety.rego* and *test-image-safety.rego* then you run the tests with `opa test`.

```
$ opa test image-safety.rego test-image-safety.rego
PASS: 1/1
```


## Label Management
Some of the core Kubernetes functionality (e.g. scheduling, networking) is driven by the labels on resources.  Making sure resources have the right labels is important.

Here's how you require all resources to have a `costcenter` label.

```
# deny any resource without a costcenter label
1: deny[msg] {
2:     not input.request.object.metadata.labels.costcenter
3:     msg := "resource should include a 'costcenter' label"
4: }
```

Here are a few notes explaining each line above.

* On line 2, `input.request.metadata.labels.costcenter` does the obvious thing: descending through the YAML hierarchy.  The dot (.) operator never throws any errors; if the path does not exist the value of the expression is `undefined`.
* On line 2, negation with `not` returns `true` given either `false` or `undefined`.

Here's how you require all resources to have an `email` label where the email is from the domain `hooli.com`.

```
 1: # deny any resource without an email label
 2: deny[msg] {
 3:     not input.request.object.metadata.labels.email
 4:     msg := "resource should include a 'costcenter' label"
 5: }
 6:
 7: # deny any resource whose email label contains the wrong domain
 8: deny[msg] {
 9:     email := input.request.object.metadata.labels.email
10:     not endswith(email, "hooli.com")
11:     msg := sprintf("'email' label not from 'hooli.com': %v", [email])
12: }
```

* Lines 2 and 8 work together.  If either of the deny rules succeeds, the request is rejected.  Conceptually, `deny` is a set of error messages, and each rule contributes one (or more) error messages to that set.

Often you have a set of labels that must be all be present.  While you could write separate rules for each of them, you can use set-comprehension to do them all at once.

```
1: deny[msg] {
2:     required_labels := {"costcenter", "email"}
3:     existing_labels := {k | input.request.object.metadata.labels[k]}
4:     missing_labels := required_labels - existing_labels
5:     count(missing_labels) > 0
6:     msg := sprintf("resource is missing the following labels: %v", [missing_labels])
7: }
```

* Line 2 creates temporary variable whose value is the *set* of strings: `"costcenter"` and `"email"`.
* Line 3 is a set comprehension.  It finds the set of all keys `k` such that `input.request.object.metadata.labels[k]` is true, i.e. the dictionary `input.request.object.metadata.labels` has the key `k`.
* Line 4 is computing the set difference of `required_labels` and `existing_labels`.  OPA has a handful of builtins written as infix, e.g. +, *, -, /.  See the Language Reference for details: [openpolicyagent.org/docs/language-reference.html](https://openpolicyagent.org/docs/language-reference.html).

If some of those required labels have a set of permitted values, you could write the following policy.

```
 1: required_labels = {"costcenter": {"retail", "commercial"},
 2:                    "email": null}
 3: deny[msg] {
 4:     missing_labels := {k | required_labels[k]} -
 5:                       {k | input.request.object.metadata.labels[k]}
 6:     count(missing_labels) > 0
 7:     msg := sprintf("resource is missing the following labels: %v",
 8:                    [missing_labels])
 9: }
10: deny[msg] {
11:     permitted_values := required_labels[key]
12:     permitted_values != null
13:     value := input.request.object.metadata.labels[key]
14:     not permitted_values[value]
15:     msg := sprintf("resource label %v=%v must have one of the values: %v",
16:                    [key, value, permitted_values])
17: }
```

* Line 1 defines a variable `required_labels` with package-level scope.  All rules within the package can reference it.  You use `=` instead of `:=` for assignment at the package level.  The value is a dictionary mapping a label name to the set of permitted values or `null`.  If the value is `null`, there is no restriction on the permitted values.
* Line 3 checks for the simple existence of the proper labels, whereas the rule starting at line 10 checks the values of the required labels.
* Line 11 iterates over the key/value pairs of `required_labels`.
* Line 13 looks up the value of required label `key`.  If it does not exist, the expression is undefined, and the rule creates no error message for the user.  That's exactly what we want because the previous rule checks for missing labels.
* Line 14 checks if `value` belongs to the set `permitted_values`.


## Cheatsheet

* Membership
  * `a[7]`: check if index 7 exists in array `a`
  * `d["apple"]`: check if "apple" is a key in dictionary `d`
  * `s["apple"]`: check if "apple" belongs to set `s`
* Membership iteration
  * `d[key]`: iterate over keys in dictionary `d`
  * `a[index]`: iterate over indexes in array `a`
  * `s[value]`: iterate over values in set `s`
* Lookup
  * `value := a[7]`: lookup value of index 7 at array `a`
  * `value := d["apple"]`: lookup value of key "apple" in dictionary `d`
* Lookup iteration
  * `value := d[key]`: iterate over key/value pairs in dictionary `d`
  * `value := a[index]`: iterate over index/value pairs in array `a`
  * `value := d[_]`: iterate over values in dictionary `d`
  * `value := a[_]`: iterate over values in array `a`


## Stuff





```
deny[msg] {
    not input.request.object.spec.metadata.labels.foo
    msg = "All resources must have label 'foo'"
}
```


safe_registries = {"foo*", "bar*"}
deny[msg] {
    image := input.request.object.spec.containers[_].image
    not safe(image)
}
safe(image) {
    safe_registries[x]
    regexp(x, image)
}

deny[msg] {
    image := input.request.object.spec.containers[_].image
    matches := {registry | safe_registries[registry]; regexp(registry, image)}
    count(matches) == 0
    msg := "..."
}


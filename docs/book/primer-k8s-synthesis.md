# Kubernetes OPA Primer

In Kubernetes, Admission Controllers enforce semantic validation of objects during create, update, and delete operations. With OPA you can enforce custom policies on Kubernetes objects without recompiling or reconfiguring the Kubernetes API server or even Kubernetes Admission Controllers.

This primer assumes you, the Kubernetes administrator, have already installed OPA as a validating admission controller on Kubernetes as described in the [OPA tutorial](https://www.openpolicyagent.org/docs/kubernetes-admission-control.html).  And now you are at the point where you want to write your own policies.

OPA was designed to write policies over arbitrary JSON/YAML.  It does NOT have built-in concepts like pods, deployments, or services.  OPA just sees the JSON/YAML sent by Kubernetes API server and allows you to write whatever policy you want to make a decision.  You as the policy-author know the semantics--what that JSON/YAML represents.

## 1. Your First Rule: Image Registry Safety

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
**Policies and Packages**.
In line 1 the `package kubernetes.admission` declaration gives the (hierarchical) name `kubernetes.admission` to the rules in the remainder of the policy.  The default installation of OPA as an admission controller assumes your rules are in the package `kubernetes.admission`.

**Deny Rules**.  For admission control, you write `deny` statements.  Order does not matter.  (OPA is far more flexible than this, but we recommend writing just `deny` statements to start.)  In line 2, the *head* of the rule `deny[msg]` says that the admission control request should be rejected and the user handed the error message `msg` if the conditions in the *body* (the statements between the `{}`) are true.

`deny` is the *set* of error messages that should be returned to the user.  Each rule you write adds to that set of error messages.

For example, suppose you tried to create the Pod below with nginx and mysql images.

```
kind: Pod
apiVersion: v1
metadata:
  name: myapp
spec:
  containers:
  - image: nginx
    name: nginx-frontend
  - image: mysql
    name: mysql-backend
```

`deny` evaluates to the following set of messages.

```
[
  "image fails to come from trusted registry: nginx",
  "image fails to come from trusted registry: mysql"
]
```

<!--
i = {
  "request": {
    "kind": {"kind": "Pod"},
    "object": {"spec": {"containers": [
      {"image": "nginx"},
      {"image": "mysql"}]}}}}

deny with input as i
-->

**Input**  In OPA, `input` is a reserved, global variable whose value is the  Kubernetes AdmissionReview object that the API server hands to any admission control webhook.

AdmissionReview objects have many fields.  The rule above uses `input.request.kind`, which includes the usual group/version/kind information.  The rule also uses `input.request.object`, which is the YAML that the user provided to `kubectl` (augmented with defaults, timestamps, etc.).  The full `input` object is 50+ lines of YAML, so below we show just the relevant parts.

```yaml
apiVersion: admission.k8s.io/v1beta1
kind: AdmissionReview
request:
  kind:
    group:
    kind: Pod
    version: v1
  object:
    metadata:
      name: myapp
    spec:
      containers:
        - image: nginx
          name: nginx-frontend
        - image: mysql
          name: mysql-backend
```

**Dot notation**  In line 3 `input.request.kind.kind == "Pod"`, the expression `input.request.kind.kind` does the obvious thing: it descends through the YAML hierarchy.  The dot (.) operator never throws any errors; if the path does not exist the value of the expression is `undefined`.

<!--
{
  "apiVersion": "admission.k8s.io/v1beta1",
  "kind": "AdmissionReview",
  "request": {
    "kind": {
 "group": null,
"kind": "Pod",
"version": "v1"
},
"object": {
"metadata": {
"name": "myapp"
},
"spec": {
"containers": [
{
"image": "nginx",
"name": "nginx-frontend"
},{
"image": "mysql",
"name": "mysql-backend"
}]}}}}
-->
You can see OPA's evaluation in the REPL.

```
> input.request.kind
{
  "group": null,
  "kind": "Pod",
  "version": "v1"
}
> input.request.kind.kind
"Pod"
> input.request.object.spec.containers
[
  {
    "image": "nginx",
    "name": "nginx-frontend"
  },
  {
    "image": "mysql",
    "name": "mysql-backend"
  }
]
```

**Equality**. Lines 3,4,6 all use a form of equality.  There are 3 forms of equality in OPA.

* `x := 7` declares a local variable `x` and assigns variable `x` to the value 7.  The compiler throws an error if `x` already has a value.
* `x == 7` returns true if `x`'s value is 7.  The compiler throws an error if `x` has no value.
* `x = 7` either assigns `x` to 7 if `x` has no value or compares `x`'s value to 7 if it has a value.  The compiler never throws an error.

The recommendation for rule-writing is to use `:=` and `==` wherever possible.  Rules are easier to write and to read.  `=` is invaluable in  more advanced use cases, and outside of rules is the only supported form of equality.

**Arrays**.  Lines 4-5 find images in the Pod that don't come from the trusted registry.  To do that, they use the `[]` operator, which does what you expect: index into the array.

Continuing the example from earlier:

```
> input.request.object.spec.containers[0]
{
  "image": "nginx",
  "name": "nginx-frontend"
}
> input.request.object.spec.containers[0].image
"nginx"
```

The `[]` operators let you use variables to index into the array as well.

```
> i := 0
> input.request.object.spec.containers[i]
{
  "image": "nginx",
  "name": "nginx-frontend"
}
```

**Iteration** The containers array has an unknown number of elements, so to implement an image registry check you need to iterate over them.  Iteration in OPA requires no new syntax.  In fact, OPA it is always iterating--it's always searching for all variable assignments that make the conditions in the rule true. It's just that sometimes the search is so easy people don't think of it as search.

To iterate over the indexes in the `input.request.object.spec.containers` array, you just put a variable that has no value in for the index.  OPA will do what it always does: find values for that variable that make the conditions true.

In the REPL, OPA detects when there will be multiple answers and displays all the results in a table.

```
> input.request.object.spec.containers[j]
+---+-------------------------------------------+
| j |  input.request.object.spec.containers[j]  |
+---+-------------------------------------------+
| 0 | {"image":"nginx","name":"nginx-frontend"} |
| 1 | {"image":"mysql","name":"mysql-backend"}  |
+---+-------------------------------------------+
```

Often you don't want to invent new variable names for iteration.  OPA provides the special anonymous variable `_` for exactly that reason.  So in line (4) `image := input.request.object.spec.containers[_].image` finds all the images in the containers array and assigns each to the `image` variable one at a time.

**Builtins**.  On line 5 the *builtin* `startswith` checks if one string is a prefix of the other.  The builtin `sprintf` on line 6 formats a string with arguments.  OPA has 50+ builtins detailed at [openpolicyagent.org/docs/language-reference.html](https://openpolicyagent.org/docs/language-reference.html).
Builtins let you analyze and manipulate:

* Numbers, Strings, Regexs, Networks
* Aggregates, Arrays, Sets
* Types
* Encodings (base64, YAML, JSON, URL, JWT)
* Time



## 2. Unit Testing: Image Registry Safety
When you write policies, you should use the OPA unit-test framework *before* sending the policies out into the OPA that is running on your cluster.  The debugging process will be much quicker and effective.  Here's an example test for the policy from the last section.

```
 1: package kubernetes.test_admission
 2: import data.kubernetes.admission
 3:
 4: test_image_safety {
 5:   unsafe_image := {"request": {
 6:       "kind": {"kind": "Pod"},
 7:       "object": {"spec": {"containers": [
 8:           {"image": "hooli.com/nginx"},
 9:           {"image": "busybox"}]}}}}
10:   count(admission.deny) == 1 with input as unsafe_image
11: }
```

**Different Package**. On line 1 the `package` directive puts these tests in a different namespace than admission control policy itself.  This is the recommended best practice.

**Import**.  On line 2 `import data.kubernetes.admission` allows us to reference the admission control policy using the name `admission` everwhere in the test package.  `import` is not strictly necessary--it simply sets up an alias; you could instead reference `data.kubernetes.admission` inside the rules.

**Unit Test**.  On line 4 `test_image_safety` defines a unittest.  If the rule evaluates to true the test passes; otherwise it fails.  When you use the OPA test runner, anything in any package starting with `test` is treated as a test.

**Assignment**. On line 5 `unsafe_image` is the input we want to use for the test.  Ideally this would be a real AdmissionReview object, though those are so long that in this example we hand-rolled a partial input.

**Dot for packages**.  On line 11 we use the Dot operator on a package.  `admission.deny` runs the `deny` rule shown above (and all other `deny` rules in the `admission` package).


**Test Input**.  Also on line 11 the stanza `with input as unsafe_image` sets the value of `input` to be `unsafe_image` while evaluating `count(admission.deny) == 1`.

**Running Tests**. Different packages must go into different files.  If you've created the files *image-safety.rego* and *test-image-safety.rego* then you run the tests with `opa test`.

```
$ opa test image-safety.rego test-image-safety.rego
PASS: 1/1
```

## 3. Existing K8s Resources: Ingress Conflicts
The image-repository example is one of the simpler access control policies you might need to write for Kubernetes because you can make the decision using just the one JSON/YAML file describing the pod. But sometimes you need to know what other resources exist in the cluster to make an allow/deny decision.

For example, it’s possible to accidentally create two applications serving internet traffic using Kubernetes ingresses where one application steals traffic from the other. The policy that prevents that needs to compare a new ingress that’s being created/updated with all of the existing ingresses.  So consider the AdmissionReview `input` shown below.

```yaml
apiVersion: admission.k8s.io/v1beta1
kind: AdmissionReview
request:
  kind:
    group: extensions
    kind: Ingress
    version: v1beta1
  object:
    metadata:
      name: prod
    spec:
      rules:
      - host: initech.com
        http:
          paths:
          - path: /finance
            backend:
              serviceName: banking
              servicePort: 443
```

We want to avoid having two ingresses with the same `host`. Here is the (essence) of the policy.

```
1: package kubernetes.admission
2: deny[msg] {
3:     input.request.kind.kind == "Ingress"
4:     newhost := input.request.object.spec.rules[_].host
5:     oldhost := data.kubernetes.ingresses[namespace][name].spec.rules[_].host
6:     newhost == oldhost
7:     msg := sprintf("ingress host conflicts with ingress %v/%v", [namespace, name])
8: }
```
The first part of the rule you already understand:
* Line (3) checks if the `input` is an Ingress
* Line (4) iterates over all the rules in the `input` ingress and looks up the `host` field for each of its rules.

**Existing K8s Resources** Line (5) iterates over ingresses that already exist in Kubernetes. `data` is a global variable where (among other things) OPA has a record of the current resources inside Kubernetes.  The line

```oldhost := data.kubernetes.ingresses[namespace][name].spec.rules[_].host```

finds all ingresses in all namespaces, iterates over all the `rules` inside each of those and assigns the `host` field to the variable `oldhost`.  Whenever `newhost == oldhost`, there's a conflict, and the OPA rule includes an appropriate error message into the `deny` set.

In this case the rule uses explicit variable names `namespace` and `name` for iteration so that it can use those variables again when constructing the error message in line 7.

**Schema Differences**.  Both `input` and `data.kubernetes.ingresses[namespace][name]` represent ingresses, but they do it differently.

* `input` is a K8s AdmissionReview object.  It includes several fields in addition to the K8s Ingress object itself.
* `data.kubernetes.ingresses[namespace][name]` is a K8s Ingress object.

Here are two examples.

<div>
<div style="float: left; width: 50%; border: 1px black">
<center><b>data.kubernetes.ingresses[namespace][name]</b></center>
<pre><code>
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: prod
spec:
  rules:
  - host: initech.com
    http:
      paths:
      - path: /finance
        backend:
          serviceName: banking
          servicePort: 443
</code></pre></div>
<div style="float: left; width: 50%; padding 3px;">
<center><b>input</b></center>
<pre><code>
apiVersion: admission.k8s.io/v1beta1
kind: AdmissionReview
request:
  kind:
    group: extensions
    kind: Ingress
    version: v1beta1
  operation: CREATE
  userInfo:
    groups:
    username: alice
  object:
    metadata:
      name: prod
    spec:
      rules:
      - host: initech.com
        http:
          paths:
          - path: /finance
            backend:
              serviceName: banking
              servicePort: 443
</code></pre></div>

</div>

<br>




## 4. Cheat Sheet

Lookup data

* Check if label `foo` exists (whether or not `labels` exists): `input.request.metadata.labels.foo`
* Check if label `foo` does not exist: `not input.request.metadata.labels.foo`
* Check if label `first.name` exists: `input.request.metadata.labels["first.name"]`

Equality

* Inside of a rule assign variable `x` to the value of label `costcenter`: `x := input.request.metadata.labels.costcenter`
* Outside of a rule assign variable `whitelist` to the set `hooli.com` and `initech.com`: `whitelist = {"hooli.com", "initech.com"}`
* Check if variable `x` and variable `y` have the same value: `x == y`

Iterate over components of AdmissionReview

* Iterate over label names: `input.request.metadata.labels[name]`
* Iterate over label name/value pairs: `value := input.request.metadata.labels[name]`
* Iterate over spec containers: `container := input.request.object.spec.containers[_]`

Iterate over existing resources

* All ingresses: `data.kubernetes.ingresses[namespace][name]`
* All ingresses in namespace `prod`: `data.kubernetes.ingresses["prod"][name]` OR `data.kubernetes.ingresses.prod[name]`
* All resources: `data.kubernetes[kind][namespace][name]`
* All resources in namespace `prod`: `data.kubernetes[kind]["prod"][name]`

Sets

* Iterate over elements in the set `deny`: `deny[element]`
* Check if message "image fails to come from trusted registry: nginx" belongs to the set `deny`: `deny["image fails to come from trusted registry: nginx"]`

Packages and policies

* Name a collection of rules `kubernetes.admission`: `package kubernetes.admission`
* Within the `kubernetes.admission` package, evaluate all of the `deny` rules: `deny`
* Outside the `kubernetes.admission` package, evaluate all of its `deny` rules:  `data.kubernetes.admission.deny`
* Outside the `kubernetes.admission` package, create an alias `adm` to that package: `import data.kubernetes.admission as adm`

Testing

* Create a test: `test_NAME { ... }`
* Mock out `input` with `{"foo": "bar"}` and evaluate `deny` within package `kubernetes.admission`: `kubernetes.admission.deny with input as {"foo": "bar"}`



<!-- ## 3. Simple Policies

**Comparing Values: Label Management**
To write even the most trivial policy you need to be able to lookup and compare values in YAML/JSON.  The values in OPA are the JSON types (string, number, boolean, null, array, object) along with a set type.  To lookup/compare values OPA gives you the usual dot-operator and array referencer.

Here's how you require all resources to have a `costcenter` label.

```
# deny any resource without a costcenter label
1: deny[msg] {
2:     not input.request.object.metadata.labels.costcenter
3:     msg := "resource should include a 'costcenter' label"
4: }
```

* On line 2, `input.request.metadata.labels.costcenter` does the obvious thing: descending through the YAML hierarchy.  The dot (.) operator never throws any errors; if the path does not exist the value of the expression is `undefined`.

* On line 2, negation with `not` returns `true` given either `false` or `undefined`.

To see this in action, use the REPL (and write unit tests):

```
$ opa run
> deny[msg] {
|     not input.request.object.metadata.labels.costcenter
|     msg := "resource should include a 'costcenter' label"
| }
> deny with input as {"request": {"object": {"metadata": {"labels": {"foo": 1}}}}}
[
  "resource should include a 'costcenter' label"
]
```


You can write multiple `deny` statements, and OPA has [50+ built-in functions](https://openpolicyagent.org/docs/language-reference.html) for string-manipulation and the like.  Here's how you require all resources to have an `email` label where the email is from the domain `hooli.com`.

```
 1: # deny any resource without an email label
 2: deny[msg] {
 3:     not input.request.object.metadata.labels.email
 4:     msg := "resource should include an 'email' label"
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

In the REPL:

```
$ opa run
> # deny any resource without an email label
> deny[msg] {
|     not input.request.object.metadata.labels.email
|     msg := "resource should include an 'email' label"
|  }
| # deny any resource whose e`mail label contains the wrong domain
| deny[msg] {
|     email := input.request.object.metadata.labels.email
|     not endswith(email, "hooli.com")
|     msg := sprintf("'email' label not from 'hooli.com': %v", [email])
| }
|
Rule 'deny' defined in package repl. Type 'show' to see rules.
Rule 'deny' defined in package repl. Type 'show' to see rules.
> deny with input as {"request": {"object": {"metadata": {"labels": {"foo": 1}}}}}
[
  "resource should include an 'email' label"
]
> deny with input as {"request": {"object": null}}
[
  "resource should include an 'email' label"
]
> deny with input as {"request": {"object": {"metadata": {"labels": {"email": "alice@gmail.com"}}}}}
[
  "'email' label not from 'hooli.com': alice@gmail.com"
]
> deny with input as {"request": {"object": {"metadata": {"labels": {"email": "alice@hooli.com"}}}}}
[]
```

If you have a field name that isn't a valid identifier (e.g. it includes spaces) or is an OPA keyword (e.g. `default`) you can use the `[]` operator, just like JSON-pointer.  In fact, the `.` operator is just syntactic sugar for `[]`.

```
# check that label 'first name` exists
deny[msg] {
    not input.request.object.metadata["first name"]
    msg := "missing label `first name`"
}
```

### 2.2 Assign variables: Label Management
The objects you are interested in are sometimes buried pretty deep in YAML/JSON.  OPA lets you create your own variables to store intermediate results.  Those variables are assigned JSON values, and you can treat them the same as you treat `input`.

```
deny[msg] {
    labels := input.request.object.metadata.labels
    not startswith(labels.costcenter, "hooli.com")
    msg := "costcenter should start with 'hooli.com"
}
```

In the REPL:

```
$ opa run
> deny[msg] {
|     labels := input.request.object.metadata.labels
|     not startswith(labels.costcenter, "hooli.com")
|     msg := "costcenter should start with 'hooli.com"
| }
|
Rule 'deny' defined in package repl. Type 'show' to see rules.
> deny with input as {"request": {"object": {"metadata": {"labels": {"costcenter": "foo"}}}}}
[
  "costcenter should start with 'hooli.com"
]
> deny with input as {"request": {"object": {"metadata": {"labels": {"costcenter": "hooli.com/retail"}}}}}
[]
```

Notice that assignment is `:=`.  There is also a `=` operator that combines both comparison `==` and assignment `:=`.  It will assign or compare depending on whether the variables already have values; it can be invaluable in certain cases but we recommend starting with `:=` and `==`.  The only exception is when you assign variables in the package scope, i.e. outside of the `deny`.  Then you need to use `=` instead of `:=`. -->


## Appendix: Admission Control Flow

Here is a sample of the flow of information from the user to the API server to OPA and back.

It starts with someone running `kubectl create -f` on the following file (on a minikube cluster).

```yaml
kind: Pod
apiVersion: v1
metadata:
  name: nginx
  labels:
    app: nginx
spec:
  containers:
  - image: nginx
    name: nginx
```

OPA receives the following AdmissionReview object from the Kubernetes API server's Admission Control webhook.

```yaml
apiVersion: admission.k8s.io/v1beta1
kind: AdmissionReview
request:
  kind:
    group: ''
    kind: Pod
    version: v1
  namespace: opa
  object:
    metadata:
      creationTimestamp: '2018-10-27T02:12:20Z'
      labels:
        app: nginx
      name: nginx
      namespace: opa
      uid: bbfee96d-d98d-11e8-b280-080027868e77
    spec:
      containers:
      - image: nginx
        imagePullPolicy: Always
        name: nginx
        resources: {}
        terminationMessagePath: "/dev/termination-log"
        terminationMessagePolicy: File
        volumeMounts:
        - mountPath: "/var/run/secrets/kubernetes.io/serviceaccount"
          name: default-token-tm9v8
          readOnly: true
      dnsPolicy: ClusterFirst
      restartPolicy: Always
      schedulerName: default-scheduler
      securityContext: {}
      serviceAccount: default
      serviceAccountName: default
      terminationGracePeriodSeconds: 30
      tolerations:
      - effect: NoExecute
        key: node.kubernetes.io/not-ready
        operator: Exists
        tolerationSeconds: 300
      - effect: NoExecute
        key: node.kubernetes.io/unreachable
        operator: Exists
        tolerationSeconds: 300
      volumes:
      - name: default-token-tm9v8
        secret:
          secretName: default-token-tm9v8
    status:
      phase: Pending
      qosClass: BestEffort
  oldObject:
  operation: CREATE
  resource:
    group: ''
    resource: pods
    version: v1
  uid: bbfeef88-d98d-11e8-b280-080027868e77
  userInfo:
    groups:
    - system:masters
    - system:authenticated
    username: minikube-user
```

OPA returns to the Admission Controller Webhook the following AdmissionReview response.  The `response.status.reason` is the error message the Kubernetes API server returns to the user.  It is the concatenation of all the messages in the `deny` set defined above.  In this case, the policy that OPA evaluated requires all images to come from the `hooli.com` registry.

```yaml
apiVersion: admission.k8s.io/v1beta1
kind: AdmissionReview
response:
  allowed: false
  status:
    reason: "image fails to come from trusted registry: nginx"
]
```


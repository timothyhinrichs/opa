# Kubernetes OPA Primer

In Kubernetes, Admission Controllers enforce semantic validation of objects during create, update, and delete operations. With OPA you can enforce custom policies on Kubernetes objects without recompiling or reconfiguring the Kubernetes API server or even Kubernetes Admission Controllers.

This primer assumes you, the Kubernetes administrator, have already installed OPA as a validating admission controller on Kubernetes as described in the [OPA tutorial](https://www.openpolicyagent.org/docs/kubernetes-admission-control.html).   

## 1. Architectural overview

Once OPA is installed, every time a user tries to create, update, delete, or connect to a Kubernetes resource, OPA decides whether to allow or deny that request.  The workflow described below would be the same for ANY admission controller--it is dictated by the admission control interface.

1) End-user runs `kubectl create -f ...`.  Assume user is using the YAML below.

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

2) Kubernetes API server sends an AdmissionReview object to OPA that describes the request made by the end-user.  The API server has augmented the YAML that the user provided to `kubectl` as follows.

* **request.kind**: the group/version/kind of the resource being operated on
* **request.object**: the augmented YAML that the user provided to `kubectl`, in this case the `nginx` pod that the user is trying to create.
* **request.namespace**: the namespace for the object
* **request.oldObject**: on an update the previous version of **request.object**
* **request.operation**: the operation the user is performing: CREATE, UPDATE, DELETE, or CONNECT 
* **request.userInfo**: user identity for the creator.  Note that for templated resources this may be the kube-system.

```
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

3) OPA makes an allow/deny decision based on the policies it's been loaded with and hands that decision back to the API server.  If OPA denies the request, the API server denies the user's request and returns an error message.  If OPA allows the request, the API server continues running the remainder of the authorization and admission control pipeline to make a global decision.  

For example, if the policy loaded into OPA required all images to come from the `hooli.com` registry, OPA would deny the request above and return an AdmissionReview response with an error message.

```
apiVersion: admission.k8s.io/v1beta1
kind: AdmissionReview
response:
  allowed: false
  status:
    reason: image 'nginx' not from hooli.com
```

The remainder of this primer shows how to write OPA policies that decide whether to allow or deny validating admission control requests to enforce whatever guardrails you need to satisfy the security, compliance, and operations needs of your cluster.

## 2. Simple Policies
OPA was designed to write policies over arbitrary JSON/YAML.  It does NOT have built-in concepts like pods, deployments, services.  OPA just sees the YAML provided by the API server and allows you to write whatever policy you want to make a decision.  You as the policy-author know the semantics--what that YAML represents.  OPA just sees it as YAML and assigns it to the variable `input`.  (Technically OPA sees it as JSON but as you would expect can load YAML as well.  The OPA syntax is a super-set of JSON.)

### 2.1 Lookup and Compare Values
To write even the most trivial policy you need to be able to lookup and compare values in YAML/JSON.  The values in OPA are the JSON types (string, number, boolean, null, array, object) along with a set type.  To lookup/compare values OPA gives you the usual dot-operator and array referencer.

```
# check if request is for a Pod
input.request.kind.kind == "Pod"
```

```
# check if image for the container at index 0 is nginx
input.request.object.spec.containers[0].image == "nginx"
```

```
# check if label app does not equal 'frontend'
input.request.object.metadata.label.app != "nginx"
```

Because often YAML/JSON is schema-less, OPA is forgiving when fields are missing that you dot into.  When you dot into a field (or even a subfield of that field or a subsubfield...), OPA simply treats the result as `undefined`.  This makes common tests simple

```
# check if label 'costcenter' exists
input.request.object.metadata.costcenter
```

Negation lets you invert the result to check if a sub-sub-...field does not exist.  `not` inverts `false` or `undefined` to true.

```
# check if label 'costcenter' is missing
not input.request.object.metadata.costcenter
```

If you have a field name that isn't a valid identifier (e.g. it includes spaces) you can use the `[]` operator, just like JSON-pointer.  In fact, the `.` operator is just syntactic sugar for `[]`.

```
# check if label 'first name` exists
input.request.object.metadata["first name"]
```

So far we've just seen comparison as equality, but OPA has 50+ builtins to do different kinds of comparisons and manipulations.

```
# check if 'costcenter' label value starts with 'hooli.com'
startswith(input.request.object.metadata.labels.costcenter, "hooli.com")
```

OPA's builtins can be found at [openpolicyagent.org/docs/language-reference.html](https://openpolicyagent.org/docs/language-reference.html).


### 2.2 Assign variables
The objects you are interested in are sometimes buried pretty deep in YAML/JSON.  OPA lets you create your own variables to store intermediate results.  Those variables are assigned JSON values, and you can treat them the same as you treat `input`.

```
# create new variable 'container' assigned to container 0
labels := input.request.object.metadata.labels

# check if container 0's image name starts with 'hooli.com'
startswith(labels.costcenter, "hooli.com")
```

Notice that assignment is `:=`.  There is also a `=` operator that combines both comparison `==` and assignment `:=`.  It will assign or compare depending on whether the variables already have values; it can be invaluable in certain cases but we recommend starting with just `:=` and `==`. 

### 2.3 Write Rules

The expressions shown so far do not stand on their own.  You can evaluate them as queries or in the OPA REPL, but if you put them in a file and load them, OPA will complain.  To be a valid OPA policy you need to write *rules* (and include them in a package--shown in the tutorial and described later in this primer).  A rule is an if-then statement; if all of the conditions are true in the body of the rule then the head is also true.  A rule takes the form:

```
HEAD {
    CONDITION1
    ...
    CONDITIONM
}
```

For example, for admission control you end up writing rules that list the conditions for denying requests.

```
# Note: not quite correct for admission control, but valid in OPA.

# deny any resource with an email label from outside the hooli.com org
deny {
    not endswith(input.request.metadata.labels.email, "@hooli.com")
}
# deny any resource without a costcenter label
deny {
    not input.request.metadata.labels.costcenter
}
```

When you ask OPA for the value of `deny` it will tell you either `true` or `false`.  `deny` is not a keyword in OPA--it's just another variable that has a value.  Every variable and expression in OPA is assigned a JSON value, a set, or is undefined.  In this case, `deny` is a variable, and it's assigned the JSON value `true`.  Instead of requiring you to write `deny = true` OPA lets you write simply `deny` (both in the head and in the body).  

```
deny = true {
    not input.request.metadata.labels.costcenter
}
```

The reason the rules above aren't quite correct for admission control is that the Kubernetes API server needs to return an error message to the user describing why the request was rejected.  In fact, there could be many such error messages because the user needs to correct many different parts of their YAML.  So instead of thinking of `deny` as having value `true` or `false`, you think about `deny` as being a set of error messages--a set of strings.

To do that we change the head of the rule to say what value to include in the `deny` set.

```
# Correct policies for admission control.

# deny all pods where container 0's image comes from somewhere other than `hooli.com`
deny[msg] {
    not endswith(input.request.metadata.labels.email, "@hooli.com")
    msg := "email not from the domain 'hooli.com'"
}
# deny any resource without a costcenter label
deny[msg] {
    not input.request.metadata.labels.costcenter
    msg := "resource should include a 'costcenter' label"
}
```

## 3. Iteration

Now imagine you want to ensure all images come from a trusted image registry.  The challenge is that when you write the policy you don't know how many images might be in a pod.  So to write the statement "all images must come from the `initech.com` repository", you need some way of iterating in the policy.

```yaml
request:
  object:
    spec:
      containers:
      - image: nginx
        name: nginx
      - image: busybox
        name: busybox
      - image: initech.com/mysql
        name: mysql
``` 

You already know how to access, say, element 0 of the containers array. If you want to iterate you just put a variable in place of the array index 0.  Below we use the variable `i`, but that's just the name we've chosen for the variable.


```
# element 0 of the containers array
input.request.object.spec.containers[0]

# iterate over elements of the containers array
input.request.object.spec.containers[i]
```

Now to require that all container images come from `initech.com`, you wrap that iteration into a `deny` rule.

```
# require all images in all pods to come from 'initech.com' registry
deny[msg] {
    input.request.kind.kind == "Pod"
    container := input.request.object.spec.containers[i]
    not startswith(container.image, "initech.com")
    msg := sprintf("image %v from an unsafe image registry", [container])
}
```

If you want to think procedurally, this rule tells OPA to iterate over all values of `i` and if all of the conditions in the body are true, adds the value of `msg` to the set `deny`.

If you want to think declaratively, the rule tells OPA the conditions under which `msg` should be included in the `deny` set, so under the hood OPA finds all variable assignments for `i`, `container`, `msg` that make all the conditions in the body true and for each one ensures `deny[msg]` is true.

Often you want to iterate but not be bothered to invent new variable names, so OPA lets has the underscore `_` variable that is anonymous.  Using multiple underscores even in the same rule is equivalent to using different variables in all those locations. 

## 4. Context-aware Policies

For some real-world Kubernetes admission control policies, you can make a decision just by looking at the new/modified resource (i.e. by looking at just the OPA `input`).  But sometimes you need additional information about the resources that already exist in the Kubernetes cluster.  OPA makes that information available for writing policies; it has a sidecar running next to it that replicates the Kubernetes API resources into OPA so that you can write *context-aware* policies.

For example, imagine you want to ensure no one ever creates a new ingress that has the same hostname as an existing ingress.  To do that you need to reference the *existing* ingresses and compare them to the new ingress.  Existing resources are loaded in the global variable `data` at the location `data.kubernetes`. 

```
# deny creation of an ingress when one already exists with the same host
deny[msg] {
    input.request.operation == "CREATE"
    input.request.kind.kind == "Ingress"
    existing := data.kubernetes.ingresses[namespace][name]
    existing.spec.rules[_].host == input.request.object.spec.rules[_].host
    msg := sprintf("ingress host conflicts with existing host: %v/%v", [namespace, name])
}
```


## 5. Additional Topics
### Virtual documents
### List comprehensions
### Any vs All
### Modularity
### Functions
### Virtual documents
### Virtual documents vs functions

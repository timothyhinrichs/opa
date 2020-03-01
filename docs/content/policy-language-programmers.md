---
title: Policy Language
kind: documentation
weight: 2
toc: true
---

# Policy Language

OPA's policy language Rego is a declarative, side-effect-free language purpose-built for expressing policies in a domain-agnostic way.  It combines simple functions and variable assignments from traditional programming languages and queries from databases, all in a unified syntax.

This introduction is designed for programmers--starting from the most familiar topic of variable assignments and functions and progressing to the unique aspects of Rego--the inclusion of database queries.  This introduction is a bit unusual because the practical usage of Rego is the other way around: database queries are more common than functions.  Nevertheless for programmers this order of presentation is sometimes the most helpful.


## Values and Variables

As programmers, assigning a variable a value is one of the most fundamental things we do.  In Rego all variables are assigned one of the following kinds of values.  There are no user-defined data structures or types.
* Strings, e.g. “a string”
* Numbers, e.g. 17.35
* Booleans, e.g. true
* null
* Arrays, e.g. [1,1,2,3]
* Objects, e.g. {“user”: “alice”, “path”: [“pets”, “dogs”]}
* Sets, e.g. {1,2,3}

Rego is a superset of JSON and consequently every JSON document is a valid Rego value; the only addition is that Rego also supports sets--a collection of values that are unique.  As shown below, these values are represented in Rego the same way they are represented in JSON and are assigned to variables using the `:=` operator.

```rego
s := “a string”
n := 17.35
b := true
u := null
a := [1,1,2,3]    # array
d := {“user”: “alice”, “path”: [“pets”, “dogs”]}    # object
e := {1,2,3}     # set
```

Rego does not allow you to assign a variable more than once.  Variables are immutable.

```rego
a := "a string"
a := "another string"   # compile error
```

For composite values (arrays, dictionaries, and sets) you inspect the internal values using dot notation and bracket syntax.

```rego
d := {"user": "alice", "path": ["pets", "dogs"]}
d.user      # "alice"
d["user"]   # "alice"
d.path[0]   # "pets"
```

If you apply dot notation to a field or array index that does not exist, Rego treats the value as `undefined`.  `undefined` propagates so that if you apply dots/brackets repeatedly to a field or array index that does not exist, the value of the entire expression is still undefined.

```rego
d := {"user": "alice", "path": ["pets", "dogs"]}
d.user.foo      # undefined
d.user.foo.bar  # undefined
d.path[77]      # undefined
d.path[77].foo  # undefined
```

We will discuss more about how to use `undefined` to your advantage a little later.


The above expressions are all pretty common across programming languages.   Sets, however, aren't as common.  In Rego, a set is a special dictionary that maps the members of the set to themselves.  If `s` is a set then for every element `e` that is a member of `s` Rego ensures that `s[e] == e` is always true.  Sets differ from dictionaries, however, in that a dictionary's keys must always be strings (so they can be returned in JSON), but a set's members can be any Rego value.

```rego
s := {"alice", "bob"}
s["alice"]     #  "alice"
s["eve"]       #  "undefined
```

You can also use variables to index into composite values.

```rego
# dictionary
d := {"user": "alice", "path": ["pets", "dogs"]}
k := "path"
d[k]     # ["pets", "dogs"]
```

```rego
# array
arr := ["pets", "dogs"]
i := 1
arr[i]  # "dogs"
```

```rego
# set
st := {"pets", "dogs"}
e := "dogs"
st[e]  # "dogs"
```

There's a special, global variable that represents whatever JSON data was provided to OPA to make a policy decision: `input`.  `input` is an arbitrary JSON object (and never contains a set since the value handed to OPA is always JSON).  This global variable is controlled by OPA; it could be a JSON object representing an HTTP API, a kubernetes resource, or any other JSON value handed to OPA by the software system needing a policy decision.

```rego
# OPA handles assigning 'input' automatically, but conceptually you would write it as follows.
input := {
    "method": "GET",
    "path": "/pets/dogs",
    "user": "alice"
}
```

Recall that OPA can also be injected with external data, e.g. the current set of k8s resources, the record of who owns each application resource, or who according to PagerDuty is on-call.  That external data is stored within a second special, global variable called `data`.  While there are several different ways to inject external data into OPA, imagine we have pushed a list of the people who are currently on-call into OPA with the following RESTful API call:

```http
PUT v1/data/pagerduty/oncall  ["alice", "bob", "dave"]
```

In Rego this array you can reference as an offshoot of `data`, and you can treat `data` the same as you treat `input` or any other variable in Rego.

```rego
data.pagerduty.oncall      # ["alice", "bob", "dave"]
data.pagerduty.oncall[1]   # "bob"
```

Notice the symmetry between the API call that injects the data `v1/data/pagerduty/oncall` and the Rego reference to that same data: `data.pagerduty.oncall`.

All of the policy you write in Rego makes decisions based on the information contained within `input` and `data`.


## Functions
Functions are one of the most familiar concepts in programming languages, and while they are not the most common construct in Rego they are a good way to understand the language.  A Rego function takes N values as input and returns a single output.  Just like most languages Rego has a collection of pre-built functions you can call to manipulate Rego's values, e.g. add numbers or concatenate strings.  More interestingly, there are also builtins that manipulate special kinds of strings, e.g. network CIDRs "10.100.19.1/24" or JWT tokens "avbdcwse.dasdfasdf.asdffds".

You can use those pre-built functions to built your own functions.  The one below first trims the whitespace from the string `s`, splits the result on `.` and returns an array of strings.

```rego
trim_and_split(s) := x {
    t := trim(s, " ")
    x := split(t, ".")
}
```

One thing to understand about Rego is that return values are defined by variable assignment.  In the first line of the function `trim_and_split(s) := x` you see the argument `s` and the return value `x`.  Whatever value gets assigned to `x` by the end of the function is what that function returns.  There is no explicit `return` statement in Rego.

```Return values are defined by variable assignments```

One feature of Rego that is similar to Scala, Haskell, ML [look these up] but is unlike Java and C is that you can write several different statements that all collectively define a function.  Each statement (called a "rule" in Rego) handles a different part of the space of all possible arguments to the function.  C++ only lets you do this if the types of the arguments are distinct (operator overloading); Rego simply requires that for every possible input to the function the different statement collectively produce at most 1 distinct value.  In addition to assignment statements, each rule may therefore include conditions that must all be true for the rule to produce a return value.

For example to define the function that performs `trim_and_split` but that can take as argument either a string or an array that has already been split, you would write the following two rules.

```rego
trim_and_split(s) := x {
    is_string(s)
    t := trim(s, " ")
    x := split(t, ".")
}
trim_and_split(arr) := x {
    is_array(arr)
    x := arr
}
```

In Rego, every rule body is a collection of conditions and assignments.  All of the conditions must be true and all of the assignments must succeed for the rule to produce a return value.

```
A rule body contains only two things: variable assignments and conditions
```

For example, `trim_and_split("foo.bar.baz")` results in just the first rule being applicable, and `trim_and_split(["foo", "bar", "baz"])` results in just the second rule being applicable. The result in both cases is `["foo", "bar", "baz"]`.

### 0-ary Functions / Conditional variable assignment
As mentioned earlier, functions like `trim_and_split` are not the most common kind of function.  This is because typically when writing policy you're making a policy decision based on `input`, and so there's little need to have functions that take arguments.  Remember that `input` is the global variable that OPA assigns to whatever the software system calling OPA provides when asking for a policy decision.  Instead most of the functions you write in Rego take 0 arguments.  Because they are so common and because 0-ary functions are equivalent to top-level variables Rego requires you to write a 0-ary function as a variable.   So instead of `f()` you write just `f`.

For example, the following rule defines a 0-ary function / variable that returns the lower-cased username for the incoming `input.user`.

```rego
username := x {
    u := input.user
    x := lower(u)
}
```

A really common example of these 0-ary functions appears when you write policy that decides whether to allow a request or not.  In this case, `allow` is the name of the function, and it is assigned either true or false.

The following example allows every request made by `alice`, who is presumably an administrator, and it allows every GET request for a public API.  (We will see later how to generalize this so you make decisions based on roles, etc.)

```rego
allow := true {
    input.user == "alice"
}
allow := true {
    input.method == "GET"
    input.is_public == true
}
```

Because defining 0-ary functions that return `true` is so common, Rego lets you drop the `:= true`.  By default, a function (0-ary or n-ary) that does not explicitly assign a value is assigned to `true` whenever the function-body succeeds.

```rego
allow {
    input.user == "alice"
}
allow {
    input.method == "GET"
    input.is_public == true
}
```


## Iteration

For some policies you need to write logic that iterates over composite values like arrays and dictionaries.  Because Rego is a declarative, side-effect-free policy language, it supports a  simple kind of iteration--no while loops and no for loops.  Think of iteration as a condition just like `x > 7`; you use iteration to define a condition over a collection of elements, e.g. there is some element in the array equal to 7.

For the sake of pedagogy, first lets look at how you would write a rule without iteration that checks if `input.user` belongs to a list of administrators.

```rego
# Pedagogically useful only--not realistic
admins := ["alice", "bob"]
allow {
    i := 0
    admins[i] == input.user
}
allow {
    i := 1
    admins[i] == input.user
}
```

Obviously you wouldn't do this in practice because you need 1 rule per element of the `admins` array.  What you want to do and what `some` lets you say is that `allow` is true if there is some `i` where `admins[i] == input.user`.

```rego
admins := ["alice", "bob"]
allow {
    some i
    admins[i] == input.user
}
```

If you need to iterate over all pairs of variables you can use multiple variables with `some`.  Suppose in the following example `input.roles` is a list of roles the user has been granted, e.g. `["reader", "writer"]`.

```rego
admin_roles := ["root", "superuser"]
allow {
    some i,j
    admins_roles[i] == input.roles[j]
}
```

One key takeaway is that the syntax for iteration is EXACTLY the same as the syntax for looking up a value but where you apply `some` to any unknown variables.

The scope of `some` is always the entire rule.  It does not matter where you place `some` within the rule; `some` tells Rego that if there is an assignment of values to variables that makes all the conditions in the rule body true, that the rule is applicable.

`some` lets you iterate over arrays, dictionaries, and sets.


```rego
# Iterate over dictionary key/value pairs
apicall := {"user": "alice", "path": ["pets", "dogs"]}
allow {
    # condition that must be true with concrete values for the dictionary key
    # apicall["path"][0] == "pets"
    # same condition but iterating over all keys in 'd'
    some k
    apicall[k][0] == "pets"
}
```

For sets, remember that if `s` is a set then `s[x]` means that `x` belongs to `s`.

```rego
# Iterate over set elements
admin_users := {"alice", "bob"}
allow {
    some element
    admin_users[element]
    element == input.user
}
```

```
Recipe for iteration:
1. Write down the conditions you want to be true, using new variables for indexes/keys/values you do not know
1. Apply `some` to the variables you created
```



## A special kind of function: Virtual Documents

In addition to classic functions that return a value when given an input, Rego supports a special kind of function (called a virtual document) that does not exist in most programming languages.  These functions are more like queries in databases; they can be used as traditional functions by providing arguments when you call them, but they can also be used like queries where you can ask for all possible input/output combinations.

Consider the following definition of `admin` that is a boolean function that returns true when a user is an administrator:

```rego
admin(x) { x == "alice" }
admin(x) { x == "bob" }
admin(x) { x == "eve" }
```

You can use the logic written above to check if a user is an administrator via

```rego
admin("alice")    # true
admin("dave")     # undefined
```

But sometimes you want to return the list of all administrators.  As people we know that the logic shown above should let us enumerate all administrators, but most programming languages don't let you do it.  You would need to write a separate function that takes no arguments and returns the list of administrators, which you could certainly do in Rego.

```rego
all_admins := ["alice", "bob", "eve"]
```

But Rego isn't a programming language--it's a policy language--and so lets you define functions (called "virtual documents") and run them forwards and backwards.  It lets you define a function like `admin` that enables the caller to decide whether to check if a single user is an administrator or to return the list of all administrators.

There are two kinds of virtual documents supported today: the set and the dictionary.  The `admin` example is handled by a set.  Using a virtual set you would define the `admin` virtual doc as follows.

```rego
admin[x] { x := "alice" }
admin[x] { x := "bob" }
admin[x] { x := "eve" }
```

The caller of the virtual doc can then use `admin[]` in different ways.  Sometimes the argument `x` gets assigned a value and sometimes `x` is not assigned a value.

```rego
admin["alice"]     # true since alice is a member of the admin set
admin["eve"]       # undefined since eve is not a member
admin              # {"alice", "bob", "eve"}   the set of all admins
some x; admin[x]   # iterate over all values that belong to `admin` and assign each one in turn to `x`
```

There are a couple of key observations here.

First, instead of `admin(x)` in the head of the rule we write `admin[x]`.  The square brackets denote this as a virtual document as opposed to a classic function.  Notice what happens when the `admin[]` virtual doc is invoked--it looks exactly the same as if `admin` were defined not with rules but with a simple set such as `admin = {"alice", "bob", "eve"}`.  This is why we call these special functions "virtual documents" because the caller cannot tell whether the the `admin` set is defined with rules or not.  This applies to iteration too: `some x; admin[x]` works the same way whether `admin` is defined with rules or not.

Second, the rule body assigns the variable `x` to a value (e.g. `x := "alice"`) instead of testing the value of `x` (e.g. `x == "alice"`).  This is important because the argument `x` may NOT be assigned a value when passed as an argument.  The rule body therefore assumes `x` has no value and assigns it.  Rego automatically turns `:=` into `==` when `x` is assigned a value as the argument. This is important because virtual docs have the property that the caller can ask for all values of a set and so the rule body must enumerate them.  Rego will throw a 'safety' error if it cannot enumerate all of the input/output values of a virtual document.

Third, you can think of the virtual document `admin` as a SQL view definition, but in a syntax that is closer to programming languages and designed for expressing policy.  The 3 `admin` rules above are roughly equivalent to the SQL that follows.

```sql
CREATE VIEW admin AS
    SELECT x WHERE x = "alice"
    UNION
    SELECT x WHERE x = "bob"
    UNION
    SELECT x WHERE x = "eve"
```

(And yes Rego supports joins as well--that's just iteration.  We will see that shortly.)

There is another kind of virtual doc: a map from strings to arbitrary objects.  The same rules apply.  Write the rules assuming that you will NOT be given the inputs.

For example, we could define a function that decides if a given user is the owner of a given resource:

```rego
owner(resource) := user {
    resource == "file123"
    user := "alice"
}
owner(resource) := user {
    resource == "file456"
    user := "bob"
}
owner(resource) := user {
    resource == "file789"
    user := "eve"
}
```

Like you would expect, you always provide the resource when calling the `owner()` function:

```rego
owner("file456")   # "alice"
owner("missing")   # undefined
owner(x)           # compile error if x is not assigned a variable
```

Now define this with a virtual document.

```rego
owner[resource] := user {
    resource := "file123"
    user := "alice"
}
owner[resource] := user {
    resource := "file456"
    user := "bob"
}
owner[resource] := user {
    resource := "file789"
    user := "eve"
}
```

You can invoke the virtual document in several different ways, providing concrete values for whatever it is that you know.

```rego
owner["file456"]       # "alice"
owner["missing"]       # undefined
owner                  # {"file123", "file456", "file789"}    the keys of the owner doc
some k; owner[k]       # iterate over all keys of `owner` and assign each one in turn to `k`
some k; v := owner[k]  # iterate over all key/value pairs of owner and assign key to k and value to v
```

Again notice that this is exactly how you would interact with an `owner` dictionary that was defined with a concrete value.


## Virtual Documents with Iteration

The examples of virtual documents in the last section never used iteration, though in reality most virtual documents in practice use iteration.

For example, suppose we have some data that maps each user to each of the roles she has been assigned.

```rego
roles := {
    "alice": ["admin"],
    "bob": ["reader", "author"],
    "eve": ["reader", "owner"],
    "dave": ["admin"]
}
```

Who is an administrator is something that you will undoubtedly use in different contexts. Sometimes you will want to iterate over the administrators, and sometimes you will want to simply check if a known person is an administrator.  Virtual documents let you define `admin` once and use it however you need.  You should think of this `admin` document as JSON data that happens to be computed but could also have been provided explicitly.

In this example we need to iterate over all of the users and for each one that has the role admin include them in the virtual set.  When you first get started, we recommend you break this into 2 steps: first define a 0-ary function that returns true when there is at least one user who is an admin.  And then convert that rule into a virtual set.  Here is the definition of `has_admin`:

```rego
has_admin {
    some user
    list := roles[user]
    some i
    list[i] == "admin"
}
```

The virtual set is the same as `admin` except want to include in the set all of the assignments to the variable `user` that make the body of the rule true.  So include the `user` variable in the head of the rule.  That is the only change needed to produce the following rule.

```rego
admin[user] {
    some user
    list := roles[user]
    some i
    list[i] == "admin"
}
```

The head of the rule is effectively the SELECT part of a SQL query.  You can pick any of the variables out of the body that you want and put them in the SELECT/head.  We cannot quite show the analogous SQL query because SQL does not support JSON data (in particular it cannot iterate over key/value pairs or over array indexes).

Remember that sets can contain any kind of object, so if you wanted you could create a set where each element records which users are admins and which array index they are an `"admin"`: `[user, arrayindex]`.  Again, the only thing in this rule that is different from `has_admin` is the head.

```rego
admin[[user, i]] {
    some user
    list := roles[user]
    some i
    list[i] == "admin"
}
```

While less common, you can also create a virtual dictionary in exactly the same way.  If you want a dictionary that maps each user to the lone array index proving she is an administrator, you can do that as follows.  Again, the only difference from the `has_admin` rule is the head.

```rego
admin[user] := i {
    some user
    list := roles[user]
    some i
    list[i] == "admin"
}
```

In short, virtual documents are a way of encoding database queries over JSON data using the same language that you use to define functions.  Because virtual docs must always enumerate all their input/output pairs they must be derived from existing JSON data: the `input` document and the `data` documents.  When you write a virtual doc you are writing something akin to a SQL query where instead of using base tables to write your query you are using the `input` and `data` JSON documents.  Start by writing the body of the rule and then write the head to produce the set or dictionary that you want.

When in doubt whether to use a classic function or a virtual doc, here is the order of preference for style, performance, and ease-of-analysis when using Rego:

1. 0-ary function
1. virtual document
1. N-ary function


## Modules and Packages

A classic question in the space of policy is how to organize all of your policy statements.  Rego provides a package system designed to let you group together whatever statements you like into a logical unit.  You can import one package from another just like you would in traditional programming languages.  These packages should feel pretty familiar.  You declare a package with the `package` directive; there is at most one package directive per file.

```rego
package microservices
allow { ... }
```

Package names can be hierarchical:

```rego
package util.roles
admin[user] { ... }
```

To use a function or virtual doc from a different package, reference the full path of the function or virtual doc starting with the root `data`.

```rego
package microservices
allow {
    ...
    data.util.roles.admin[x]
}
```

Remember that `data` is a global variable.  In addition to the rules organized into packages, `data` also includes all of the external data that is injected into OPA.  That is, external data and packages share the same namespace.  The reason for this is the prevalence of virtual docs.  As you can see above, the caller of a virtual docs treats that doc just like they treat data: they iterate over it and check for membership as if the rules were just raw data.

Consequently, you must ensure that your package paths and external data paths do not conflict.  OPA does allow you to use a package that is a prefix of another package (e.g. `package foo` and `package foo.bar`), but the rules/data inserted must not conflict.   The recommendation is to avoid using packages and external data that are prefixes of each other unless you have explicit need for doing so.

Packages let you organize statements into separate policies.  Often a package maps directly to a single file on disk.  But OPA lets you have multiple files all with the same package.  Each of these files OPA calls a `module`.  While OPA knows the difference between modules, there is no way to distinguish modules within Rego.

Once we have organized our rules into multiple modules and packages the question then becomes: how do these policies get combined to return a decision to the caller?  Unlike many policy systems, OPA does NOT combine policies/packages automatically.  The caller who is asking for a policy decision picks a single package that will make the decision.  From the HTTP API, the caller might for example make the following request:

```http
POST v1/data/microservices/allow  {"input": ...}
```

This HTTP request maps directly onto the package path of `data.microservices.allow`.

While OPA does not automatically combine different policies, you can choose to combine them if you want.  After all, how to combine policies to derive a final decision is a policy problem, and Rego therefore gives you the ability to encode that policy decision yourself.  Moreover, Rego lets you choose how to resolve conflicts as well.

For example, suppose you have a policy written by security and one by developers.

```rego
package security
allow { ... }
```

```rego
package developer
allow { ... }
```

You could then combine them by creating a new package `main` that makes the final decision by requiring both the security and developer policies to allow the decision.

```rego
package main
allow {
    data.security.allow
    data.developer.allow
}
```

Or you could go a step further and say that if the security policy denies or allows the request, then it must be allowed, but otherwise the developer policy makes the decision.  Moreover, for each policy `deny` overrides `allow`.

```rego
package main
# security allowed but did not deny (deny overrides allow)
allow {
    data.security.allow
    not data.security.deny
}
# security neither allowed nor denied AND
# developer allowed but did not deny
allow {
    not data.security.allow
    not data.security.deny
    data.developer.allow
    not data.developer.deny
}
```

This ability to compose policies/packages and resolve conflicts puts the full spectrum of policy decisions into the hands of the policy author.












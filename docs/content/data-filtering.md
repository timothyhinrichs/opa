---
title: Data Filtering
kind: documentation
weight: 61
restrictedtoc: true
---

Many of OPA's use cases can be construed as deciding whether an API call is authorized or not (e.g. Kubernetes, microservices, SSH, Terraform).  But sometimes API-level granularity is not enough.  A user might be authorized to run an API call that returns a list of resources but should not be allowed to read every item in that list, or perhaps there are some fields or attributes on some items that  the user should not be allowed to read.  We call this the *data filtering* problem because the OPA policies of interest dictate which portions of data the user is authorized to read (or update), and there must be some way of removing the portions of data the user is not authorized for.

One form of data filtering, called *field-level data filtering* dictates which fields within an object the user may see. For example, the user might be authorized to ask for the details of a dog in a petstore (weight, color, breed, age), but the user might not be authorized to see the field for its previous owner.

Another form of data filtering, called “resource-level data filtering” dictates which resources as a whole the user may see.  For example, the user is authorized to ask for the list of dogs, but is only authorized to see those whose owner marked them available for adoption.

There are different approaches to data filtering, which are depicted below.
* **OPA-native**: OPA is handed the results of an API call and filters them according to policy.
* **Database-native**: OPA helps the calling service rewrite a database query so that the results of the query satisfy policy constraints.

{{< figure src="data-input.png" width="60" caption="Input flow" >}}

Each choice has its own tradeoffs.  The content of the data does not matter, but the size and available enforcement points do impact which choice is best.  These choices are not mutually exclusive.  A service could use database-native data filtering to fetch only the proper data from a database, then it could perform some complex calculation, and apply OPA-native data filtering before returning the response to the user.


## Option 1a: OPA-native data filtering

Because OPA itself can return arbitrary JSON documents as policy decisions, one option is to hand the data that needs filtering to OPA and write policies that remove unwanted fields and rows from that data.  (Or more accurately for JSON: remove unwanted array elements and key-value pairs.)

When the backend service decides it needs data from an external service, it retrieves that data however it normally does.
The backend service then sends that data to OPA.  OPA applies field-level and resource-level filtering as defined by policy and returns the response.  The backend service uses the resulting data to construct a response for its caller.

For example, imagine that the following data is provided as `input`.

```yaml
token: 1234.5678.9012
object:
- name: fido
  age: 2
  breed: Spaniel
  previousOwner: Alice
  adoption: true
- name: cujo
  age: 4
  breed: "Saint Bernard"
  previousOwner: "Donna Trenton"
  adoption: false
```

The policy we want to enforce is:
* Employees can see everything
* Customers can see the list of dogs available for adoption


```rego
package filter.dogs

# entry point.  Backend calls v1/data/filter/dogs/result
result[dog] {
    dog := input.object[_]
    allow(dog)
}
# show everyone dogs available for adoption
allow(dog) {
    dog.adoption == true
}
# show employees all dogs
allow(dog) {
    is_employee
}

# user is an employee if their JWT token includes `is_employee` claim
is_employee {
    # Decode JWT token
    [_, payload, _] := io.jwt.decode(input.token)
    # check if token includes the `is_employee` claim
    payload.is_employee
}
```

The `result` rule above returns a set of dogs that the user is allowed to see, meaning that it does not preserve the order of items in the list.  If order is important, you can use an array comprehension instead of a set.

```rego
# If order is important, replace the result rule above with an array comprehension
result := [dog |
    dog := input.object[_]
    allow(dog)
]

```

Or imagine we want to do field-level data filtering
* Employees can see all fields
* Customers can see insensitive fields


This time suppose the data provided is a single object.

```yaml
token: 1234.5678.9012
object:
  name: fido
  age: 2
  breed: Spaniel
  previousOwner: alice
```

The Rego that does this filtering breaks the fields into two groups (`core` and `sensitive`), grants employees but not customers access to sensitive fields, and constructs a new object with all of the fields the user is granted.

```rego
package pets.backend.filter.dog

# entry point: Backend service asks for v1/data/pets/backend/filter/dog/result
# Create a copy of input.object[x] for all field names that are allowed
result[field] := value  {
    value := input.object[field]
    allow(field)
}

# set of sensitive field names
sensitive = {"previousOwner"}

# keep the core fields for everyone
allow(field) { not sensitive[field] }

# add in the sensitive fields for employees
allow(field) {
	is_employee
	sensitive[field]
}

# user is an employee if their JWT token includes `is_employee` claim
is_employee {
    # Decode JWT token
    [_, payload, _] := io.jwt.decode(input.token)
    # check if token includes the `is_employee` claim
    payload.is_employee
}

```

Of course, often data-filtering requires both resource-level and field-level filtering at the same time.  Consider again the first example we saw.

```yaml
token: 1234.5678.9012
object:
- name: fido
  age: 2
  breed: Spaniel
  previousOwner: Alice
  adoption: true
- name: cujo
  age: 4
  breed: "Saint Bernard"
  previousOwner: "Donna Trenton"
  adoption: false
```

The policy we want to implement is now the combination of policies above:
* Employees can see all fields and all items
* Customers can only see insensitive fields
* Customers can only see pets available for adoption


```rego
package pets.backend.filter.dogs

# entry point.  Backend calls v1/data/pets/backend/filter/dogs/result
result = [filtered_dog |
    dog := input.object[_]
    allow_dog(dog)
    filtered_dog := filter_dog(dog)
}

# everyone can see dogs up for adoption; employees can see everything
allow_dog(dog) { dog.adoption == true }
allow_dog(dog) { is_employee }

# create a new object with all the fields that the user is allowed to see.
filter_dog(dog) = {field: value |
    value := dog[field]
    allow_dog_field(field)
}

# set of sensitive field names that should be hidden
sensitive = {"previousOwner"}

# allow only employees to see sensitive fields; allow everyone to see everything else
allow_dog_field(field) { is_employee; sensitive[field] }
allow_dog_field(field) { not sensitive[field] }

# user is an employee if their JWT token includes `is_employee` claim
is_employee {
    # Decode JWT token
    [_, payload, _] := io.jwt.decode(input.token)
    # check if token includes the `is_employee` claim
    payload.is_employee
}

```

This illustrates a pattern for writing Rego to do field-level and resource-level data-filtering.  For each level of the JSON hierarchy write one function that decides whether to allow the resource and another function that removes unwanted fields from that resource.

## Option 1b: OPA-native data filtering with json.filter()

For some use cases, the OPA-native data filtering of the last section is the right solution, but for other use cases it has some limitations.  The Rego illustrated in the last section ends up being roughly the same size as the schema of the data that you are filtering.  If there are N-levels of nesting in the data, you will end up writing N-pairs of filtering functions (one for the resource-filtering and one for the field-filtering).  Moreover, there is more boilerplate in the logic you write than you would ideally want.  The logic that repeats is: walk over elements of an array, eliminate those elements that are not allowed, and rewrite each of the allowed elements to remove unwanted fields.

An alternative is to use a builtin that was designed to eliminate rows and fields from a JSON object: `json.filter()` and `json.remove()`.  Both `json.filter` and `json.remove` take two arguments
* `object`: a JSON object
* `paths`: a collection of JSON paths through the object, e.g. `["a/b", "a/d/b"]`
`json.filter` removes all paths from `object` except those in `paths`.  `json.remove` removes all paths from `paths` from `object`.  Using these builtins allows you to write policies that describe which slices of JSON data should be kept or removed, respectively.

Consider the following data that needs to be filtered.

```yaml
token: 1234.5678.9012
object:
- name: fido
  age: 2
  breed: Spaniel
  previousOwner: Alice
  adoption: true
- name: cujo
  age: 4
  breed: "Saint Bernard"
  previousOwner: "Donna Trenton"
  adoption: false
```

Consider the following policy.
* Employees can see all fields and all items
* Customers can only see insensitive fields
* Customers can only see pets available for adoption

```rego
# entry point.  Returns a filtered version of input.object
result := json.remove(input.object, deny)

# remove a row for customers if it is not up for adoption
deny[path] {
    not is_employee
    input.object[i].adoption == false
    path := sprintf("%d", [i])
}

# set of sensitive field names
sensitive = {"previousOwner"}

# remove sensitive fields for customers
deny[path] {
    not is_employee
    input.object[i][key]
    sensitive[key]
    path := sprintf("%d/%s", [i, key])
}

# user is an employee if their JWT token includes `is_employee` claim
is_employee {
    # Decode JWT token
    [_, payload, _] := io.jwt.decode(input.token)
    # check if token includes the `is_employee` claim
    payload.is_employee
}
```

The logic shown above has the nice property that you only write as many Rego statements as there are slices of the JSON data you might remove.  This logic is also composable in the sense that one team could write the logic that identifies which rows to delete and another team could write the logic that decides which fields to delete, and you construct the overall policy by simply combining the two teams' rules.  In contrast, the approach shown in Option 1a requires those teams to collaborate more closely to make their policies composable.

Of course the example shown above lists the slices of JSON that need to be removed.  In some cases, you want the policy to be written the other way around--to list the slices of JSON that should be kept.  The latter form ensures that if someone extends the data provided to OPA to include new information without the policy being updated that the new information is not accidentally leaked.  This is where the `json.filter()` command is helpful.


### Recommended usage: Small amounts of data OR data not coming from database
The OPA-native approach is valuable when the alternative is to filter data in application (e.g. Java, Ruby) code.  Usually this means the data easily fits into memory and that it is produced by an external API call (not a database query) or is computed internally by the application.  OPA will not paginate the results, and often the need to paginate implies that you should consider the database-native option.




## Option 2: Simple database-native data filtering (Experimental)

The database-native option has the database itself do the filtering.  Database query languages, like SQL or Elastic, were designed to return only those rows or fields that are desired.  An authorization policy that describes which rows and fields a user is authorized to see can be folded into the query so that at the same time the database retrieves the required data it filters out rows and fields.  Why retrieve data that the user should never see?

In this section we assume you are using a SQL database for filtering and are targeting a single table of information; below we explain how to generalize for other styles of databases.  This means that the SQL SELECT clause and the WHERE clause will be modified to implement field-level and row-level filtering, respectively.

Consider the following data.

```yaml
pets:
- name: fido
  age: 2
  breed: Spaniel
  previousOwner: Alice
  adoption: true
- name: cujo
  age: 4
  breed: "Saint Bernard"
  previousOwner: "Donna Trenton"
  adoption: false
```

Consider the following policy.
* Employees can see all fields and all items
* Customers can only see insensitive fields
* Customers can only see pets available for adoption

Suppose that the application wants to run a SQL query

```SELECT * FROM pets```

on behalf of a non-employee.  To implement the policy above and do data filtering within the database, the SQL query would need to be transformed into the following.

```SELECT name, age, breed, adoption FROM pets WHERE adoption == true```

To do this while decoupling the data authorization policy using OPA requires two steps.
1. Write a Rego policy that describes which rows and columns each user is authorized for
1. When a SQL query needs to be run, ask OPA for the user's permitted columns and rows.
1. Transform the SQL query to satisfy the permitted columns and rows, and then execute it.
1. The resulting data will obey the OPA data-filtering policy, so return it to the user.

### Field-level data filtering and SELECT
When writing the Rego policy, remember that OPA is NOT provided the data shown above.  To address-field level filtering we need to write a policy that constrains which fields the SQL SELECT clause is authorized to ask for.  Field-level data filtering in this case means writing the Rego that returns the names of fields that the user is authorized to see.

```rego
# input
token: ....
```


```rego
fields := {"name", "age", "breed", "previousOwner", "adoption"}
sensitive_fields := {"previousOwner"}
# employees can see sensitve fields
allow_field[c] {
    sensitive_fields[c]
    is_employee
}
# everyone can see non-sensitive fields
allow_field[c] {
    fields[c]
    not sensitive_fields[c]
}
# user is an employee if their JWT token includes `is_employee` claim
is_employee {
    # Decode JWT token
    [_, payload, _] := io.jwt.decode(input.token)
    # check if token includes the `is_employee` claim
    payload.is_employee
}
```

Simply evaluating `allow_fields` while providing an input with the user's JWT will enumerate all the fields that user is authorized to see.

```bash
$ opa eval -b . 'data.authz.rowscols.deny_column' --format=pretty -i input.json
[ "name", "age", "breed", "adoption"]
```

The client using OPA to implement data-filtering would need to restrict the SELECT clause so that it only includes the fields the user is authorized to see.

### Row-level data filtering and WHERE

Row-level data-filtering is more complicated because the policy depends on the contents of the row, which is not included in the input.  The output of OPA's policy in this case is a set of conditions on rows that must be true for the user to see that row.  The SQL WHERE clause must be transformed to include all of these conditions.

As a policy-author you write the logic that either allows or denies the request, assuming that the row is provided as input.  For example, imagine your input is as follows:

```rego
# input
token: ....
row:
  name: fido
  age: 2
  breed: Spaniel
  previousOwner: Alice
  adoption: true
```

Now you write a boolean `allow_row` rule that decides whether that input is authorized or not.

```rego
# Customers can only see pets available for adoption
allow_row {
    not is_employee
    input.row.adoption == true
}
```

When authoring policy, we assumed that the row was provided as `input.row`, but in reality we will use the policy to derive the conditions that should be added to the SQL WHERE clause.  To do that, we use OPA's partial evaluation feature and tell it that `input.row` is unknown.  This will cause OPA to return a set of conditions that would make the `input.row` allowed, instead of a concrete answer like we saw earlier.

```bash
$ opa eval -b . data.authz.rowscols.allow_row -p --format=pretty -u input.object -i input.json

+---------+-------------------------------+
| Query 1 | input.object.adoption = true |
+---------+-------------------------------+
```

This query can be returned as JSON, which can then be injected into the SQL WHERE clause as shown below.

```
WHERE adoption = TRUE
```

For this example, generating the WHERE clause is straightforward:
* Drop the `input.object` off of `input.object.adoption`
* Translate Rego's `=` to  SQL's corresponding operator (which happens to also be `=`, though in other cases the difference might be more substantial)
* Translate Rego's `true` to SQL's `TRUE` (though again SQL's case insensitivity turns this into a noop)

More generally when there are multiple conditions or multiple queries, the translator would translate each of the conditions to SQL and combine them as follows:
* Use AND to combine multiple conditions within a single Query when generating SQL.
* Use OR to combine multiple queries when generating SQL.


### Extensions

The approach outlined above can be extended in the following dimensions:
* Multiple tables
* Adding tables to the query
* Non-SQL


#### Multiple tables

Suppose the original query required multiple SQL tables to assemble the information that should be returned, such as:

```SELECT * FROM pets as P, owners as O WHERE P.previousOwner = O.name ```

This query would return all the columns from both the `pets` and the `owners` tables, and would return all rows from the join.  This scenario would require extracting the authorized columns for both `pets` and `owners`.

The inputs that come in might include the list of tables that are being queried and their aliases.

```yaml
# input
token: ....
tables:
  pets: P
  owners: O
```


```rego
# allowed fields for all tables in request, properly aliased
allow_fields[f] {
    some table
    alias := input.tables[table]
    raw_field := alloweded_fields[table][_]
    f := sprintf("%v.%v", [alias, raw_field])
}

# allowed fields for all tables
allow_fields_all["pets"] = allow_field_pets
allow_fields_all["owners"] = allow_field_owners

# Fields for pets
allow_field_pets[c] { ...}

# Fields for owners
allow_field_owners[c] { ... }
```

It would also require having separated out the conditions for rows on each table as well.  Whereas in the single-table case, we wrote policy assuming the row was provided at `input.row`, here we use `input.pet` and `input.owner`.

```rego
# allow if row satisfies all of the tables in the input
allow {
    not some_table_not_allowed
}
some_table_not_allowed {
    input.tables[table]
    not allow_row[table]
}
allow_row["pet"] {
    not is_employee
    input.pet.adoption == true
}
allow_row["owner"] {
    not is_employee
    input.owner.public == true
}
```

If you want rules that cover the combination of pet and owner, we recommend you construct a SQL view and write dedicated row-filtering rules.

THIS GENERATES SUPPORT, UNDOUBTEDLY.  Could just run 2*N queries.




### Using additional tables for a query

Up til now we've shown cases for how to extend the SELECT clause or the WHERE clause within a query.  Sometimes, however, the data you need to check if a row is authorized exists in a table that is not in the original query, which to enforce requires adding to the FROM clause as well.  In this case, the policy needs access to an entirely different table.

For example, suppose that only the owner of a pet can see the pet's row.  Where the username provided in the JWT needs to be looked up in the owner's table.  The original query would ask for all pets

```SELECT * FROM pets```

This would be translated into a query that joins Pets against the Owner table and checks if the user is the owner.

```
SELECT * FROM pets as P, owners as O WHERE P.ownerid == O.owner.id AND O.owner.username == "alice"
```

When writing this policy in Rego, we might start with the following:

```yaml
# input
token: ....
tables:
  pets: P
```

```
allow_row["pet"] {
    input.owner.id == input.pet.ownerid
    input.owner.username == input.username
}
```

The result of partial evaluation here would evaluate `input.username` and replace it with `"alice"` but would otherwise return the body of that rule.  The caller would need to recognize the use of a new table and inject it into the query.


### Non-SQL databases

While the discussion above has been grounded in SQL, a similar approach can be taken for other databases as well.  For example, a prototype for doing this same thing with ElasticSearch is available [here](https://github.com/open-policy-agent/contrib/tree/master/data_filter_elasticsearch).




## Summary

| Approach | Perf/Avail | Limitations | Recommended Data |
| -------- | ---------- | ----- | -----|
| JWT | High | Updates only when user logs back in | User attributes |
| Input | High | Coupling between service and OPA | Local, dynamic |
| Bundle | High | Updates to policy/data at the same time.  Size an issue. | Static, medium |
| Push | High | Control data refresh rate.  Size an issue. | Dynamic, medium |
| Evaluation Pull (experimental) | Dependent on network | Perfectly up to date.  No size limit. | Dynamic or large |




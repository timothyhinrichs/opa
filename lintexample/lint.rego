package system.lint

permitted_heads := {"allow", "deny"}
required_heads := {"authz"}

deny[msg] {
    non_permitted_heads[msg]
}
deny[msg] {
    missing_heads[msg]
}
deny[msg] {
    incorrect_k8s_enforce_type_format[msg]
}

incorrect_k8s_enforce_type_format[msg] {
    some package_name, i
        rule := user_packages[package_name].rules[i]
        rule.head.name == "enforce"
        not has_k8s_enforce_return_type(rule)
        msg := sprintf("Rule number %v is an 'enforce' that fails to return an object with keys 'allowed' and 'message'",
            [i])
}

has_k8s_enforce_return_type(rule) {
    rule.head.key.type == "var"
    target_var := rule.head.key.value
    some i
        rule.body[i].terms[0].type == "ref"
        rule.body[i].terms[0].value == [{"type": "var", "value": "eq"}]
        rule.body[i].terms[1].type == "var"
        rule.body[i].terms[1].value == target_var
        rule.body[i].terms[2].type == "object"
        rule.body[i].terms[2].value[k][0] == {"type": "string", "value": "allowed"}
        rule.body[i].terms[2].value[m][0] == {"type": "string", "value": "message"}
}

missing_heads[msg] {
    some package_name
        module := user_packages[package_name]
        some head
            required_heads[head]
            not rule_head_exists_in_package(package_name, head)
            msg := sprintf("Required variable %v does not exist in package %v", [head, package_name])
}

rule_head_exists_in_package(package_name, head) {
    some i
        user_packages[package_name].rules[i].head.name == head
}

# permitted heads
non_permitted_heads[msg] {
    some package_name
        module := user_packages[package_name]
        some i
            rule := module.rules[i]
            not permitted_heads[rule.head.name]
            msg := sprintf("Rule head not permitted: %v", [rule.head.name])
}

# filter out packages
user_packages[pack] = module {
    some filename
        module := input[filename]
        elems := [p | p := module["package"].path[j].value]
        pack := concat(".", elems)
    not startswith(pack, "data.system")
}

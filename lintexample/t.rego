package foo

allow { true }

allow { false }

deny { bar }

bar { true }

enforce[msg] { msg := "a string" }

enforce[decision] { decision := {"allowed": true, "message": "foobar"} }


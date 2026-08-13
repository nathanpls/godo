# Request Validation

Package `github.com/nathanpls/godo/validate` provides explicit, reflection-free
field validation. `godo/http.DecodeJSON` strictly decodes bounded JSON requests.

```sh
godo add validate
```

Decode a request before validating domain rules:

```go
var input struct {
    Name  string `json:"name"`
    Email string `json:"email"`
    Role  string `json:"role"`
}

if err := godohttp.DecodeJSON(r, &input, 1<<20); err != nil {
    var decode *godohttp.DecodeError
    if errors.As(err, &decode) {
        _ = godohttp.WriteProblem(w, decode.Problem())
        return
    }
    _ = godohttp.WriteProblem(w, godohttp.Problem{Status: http.StatusInternalServerError})
    return
}
```

`DecodeJSON` requires `application/json` or a `+json` media type. It rejects
unknown object fields, trailing JSON values, malformed bodies, and requests over
the supplied byte limit.

Validate fields explicitly:

```go
validator := validate.New()
validator.Required("name", input.Name != "")
validator.StringLength("name", input.Name, 2, 100)
validator.OneOf("role", input.Role, "admin", "member")
validator.Check("email", "format", validEmail(input.Email), "must be an email")

if err := validator.Err(); err != nil {
    var validation *validate.Error
    errors.As(err, &validation)
    _ = godohttp.WriteProblem(w, godohttp.Problem{
        Status: http.StatusUnprocessableEntity,
        Extensions: map[string]any{"errors": validation.Violations},
    })
    return
}
```

Built-in rules cover required presence, UTF-8 string length, integer ranges,
and allowed string values. `Check` keeps application-specific rules explicit
without tags, reflection, or a validation expression language.

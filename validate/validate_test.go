package validate

import (
	"errors"
	"testing"
)

func TestValidatorAccumulatesViolations(t *testing.T) {
	validator := New()
	validator.Required("name", false)
	validator.StringLength("name", "x", 2, 10)
	validator.IntRange("age", 200, 0, 150)
	validator.OneOf("role", "root", "admin", "member")
	validator.Check("email", "format", false, "must be an email")
	err := validator.Err()
	var validation *Error
	if !errors.As(err, &validation) || len(validation.Violations) != 5 {
		t.Fatalf("error = %#v", err)
	}
	if validation.Violations[0].Field != "name" || validation.Violations[0].Code != "required" {
		t.Fatalf("violations = %+v", validation.Violations)
	}
	validation.Violations[0].Field = "changed"
	var again *Error
	if !errors.As(validator.Err(), &again) || again.Violations[0].Field != "name" {
		t.Fatal("Err did not return a snapshot")
	}
}

func TestValidatorAcceptsValidOptionalValues(t *testing.T) {
	validator := New()
	validator.Required("name", true)
	validator.StringLength("name", "Gopher", 2, 10)
	validator.StringLength("bio", "", 2, 100)
	validator.IntRange("age", 42, 0, 150)
	validator.OneOf("role", "admin", "admin", "member")
	if err := validator.Err(); err != nil {
		t.Fatal(err)
	}
}

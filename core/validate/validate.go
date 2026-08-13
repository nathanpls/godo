package validate

import (
	"fmt"
	"slices"
	"unicode/utf8"
)

// Violation describes one invalid input field.
type Violation struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error contains all validation violations.
type Error struct {
	Violations []Violation `json:"errors"`
}

// Error implements error.
func (validation *Error) Error() string {
	if validation == nil || len(validation.Violations) == 0 {
		return "validation failed"
	}
	return fmt.Sprintf("validation failed: %s %s", validation.Violations[0].Field, validation.Violations[0].Message)
}

// Validator accumulates explicit validation failures.
type Validator struct{ violations []Violation }

// New creates an empty Validator.
func New() *Validator { return &Validator{} }

// Check adds a violation when valid is false. It is the escape hatch for
// application-specific rules.
func (validator *Validator) Check(field, code string, valid bool, message string) {
	if validator == nil {
		panic("validate: validator must not be nil")
	}
	if valid {
		return
	}
	validator.add(field, code, message)
}

// Required requires present to be true.
func (validator *Validator) Required(field string, present bool) {
	validator.ensure()
	validator.Check(field, "required", present, "is required")
}

// StringLength checks the UTF-8 character count of a non-empty value. Empty
// optional values are ignored; combine it with Required when needed.
func (validator *Validator) StringLength(field, value string, minimum, maximum int) {
	validator.ensure()
	if minimum < 0 || maximum < minimum {
		panic("validate: invalid string length bounds")
	}
	if value == "" {
		return
	}
	if !utf8.ValidString(value) {
		validator.add(field, "invalid_utf8", "must be valid UTF-8")
		return
	}
	length := utf8.RuneCountInString(value)
	if length < minimum || length > maximum {
		validator.add(field, "length", fmt.Sprintf("must contain between %d and %d characters", minimum, maximum))
	}
}

// IntRange checks an integer against inclusive bounds.
func (validator *Validator) IntRange(field string, value, minimum, maximum int64) {
	validator.ensure()
	if maximum < minimum {
		panic("validate: invalid integer bounds")
	}
	if value < minimum || value > maximum {
		validator.add(field, "range", fmt.Sprintf("must be between %d and %d", minimum, maximum))
	}
}

// OneOf requires value to equal one allowed value.
func (validator *Validator) OneOf(field, value string, allowed ...string) {
	validator.ensure()
	if len(allowed) == 0 {
		panic("validate: OneOf requires at least one allowed value")
	}
	if !slices.Contains(allowed, value) {
		validator.add(field, "one_of", "must be one of the allowed values")
	}
}

// Err returns nil when validation succeeded or an Error containing a snapshot
// of all violations.
func (validator *Validator) Err() error {
	if validator == nil || len(validator.violations) == 0 {
		return nil
	}
	return &Error{Violations: slices.Clone(validator.violations)}
}

func (validator *Validator) add(field, code, message string) {
	validator.ensure()
	if field == "" || code == "" || message == "" {
		panic("validate: field, code, and message must not be empty")
	}
	validator.violations = append(validator.violations, Violation{Field: field, Code: code, Message: message})
}

func (validator *Validator) ensure() {
	if validator == nil {
		panic("validate: validator must not be nil")
	}
}

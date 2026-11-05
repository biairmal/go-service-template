// Package apierr builds the API's error responses: an errorz error whose code
// selects the HTTP status and whose meta.reason tells clients what happened.
// Never invent an errorz code: an unknown one maps to 500.
package apierr

import (
	"errors"

	"github.com/biairmal/go-sdk/lib/errorz"
)

// Meta keys written on API errors.
const (
	MetaReason = "reason"
	MetaFields = "fields"
)

// New returns the error for reason, with the errorz code reasonCodes assigns
// to it and meta.reason set. An unknown reason yields a 500, so a typo is
// loud.
func New(reason Reason) error {
	return build(reason)
}

// WithFields is New plus meta.fields, e.g. {"title": "is required"}.
func WithFields(reason Reason, fields map[string]string) error {
	return build(reason).WithMeta(MetaFields, fields)
}

// build creates the coded error. Starting from the errorz constructor keeps
// errors.Is(err, errorz.ErrForbidden) and friends working.
func build(reason Reason) *errorz.Error {
	var e *errorz.Error
	switch reasonCodes[reason] {
	case errorz.CodeForbidden:
		e = errorz.Forbidden()
	case errorz.CodeBadRequest:
		e = errorz.BadRequest()
	case errorz.CodeNotFound:
		e = errorz.NotFound()
	case errorz.CodeConflict:
		e = errorz.Conflict()
	default:
		e = errorz.Internal().WithMessage("apierr: unknown reason " + string(reason))
	}
	return e.WithMeta(MetaReason, string(reason))
}

// ReasonOf returns err's meta.reason, or "" when it has none.
func ReasonOf(err error) Reason {
	var e *errorz.Error
	if !errors.As(err, &e) {
		return ""
	}
	r, ok := e.Meta[MetaReason].(string)
	if !ok {
		return ""
	}
	return Reason(r)
}

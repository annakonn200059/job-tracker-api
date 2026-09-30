package errors_models

import "errors"

var (
	ErrNotFound   = errors.New("not found")
	ErrConflict   = errors.New("conflict")
	ErrValidation = errors.New("validation failed")
	ErrForbidden  = errors.New("forbidden")
)

// ErrUnauthorized means the request carries no valid session. It is
// distinct from ErrForbidden (authenticated, but not allowed).
var ErrUnauthorized = errors.New("unauthorized")

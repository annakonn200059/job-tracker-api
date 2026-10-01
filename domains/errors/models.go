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

// ErrUnsupportedMediaType means the request body is not in a format the
// endpoint accepts (e.g. JSON endpoints called without
// Content-Type: application/json).
var ErrUnsupportedMediaType = errors.New("unsupported media type")

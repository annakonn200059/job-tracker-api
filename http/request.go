package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	errors_models "github.com/annakonn200059/job-tracker-api/domains/errors"
)

const maxBodyBytes = 1 << 20 // 1MB

// DecodeJSON decodes a JSON request body into dst. It rejects unknown
// fields and bodies over 1MB, and wraps any failure in ErrValidation so it
// reaches the caller as 400 rather than 500 once passed to WriteError.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("%w: %v", errors_models.ErrValidation, err)
	}
	return nil
}

// WriteJSON writes v as a JSON response with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// PathID parses the {key} path value (set via ServeMux's {key} pattern) as
// a positive int64.
func PathID(r *http.Request, key string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(key), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: invalid %s", errors_models.ErrValidation, key)
	}
	return id, nil
}

// QueryInt64 parses an optional int64 query parameter.
func QueryInt64(q url.Values, key string) (*int64, error) {
	raw := q.Get(key)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid %s", errors_models.ErrValidation, key)
	}
	return &v, nil
}

// QueryInt32 parses an optional int32 query parameter.
func QueryInt32(q url.Values, key string) (*int32, error) {
	raw := q.Get(key)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid %s", errors_models.ErrValidation, key)
	}
	v32 := int32(v)
	return &v32, nil
}

// QueryInt16 parses an optional int16 query parameter.
func QueryInt16(q url.Values, key string) (*int16, error) {
	raw := q.Get(key)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseInt(raw, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid %s", errors_models.ErrValidation, key)
	}
	v16 := int16(v)
	return &v16, nil
}

// QueryBool parses an optional bool query parameter.
func QueryBool(q url.Values, key string) (*bool, error) {
	raw := q.Get(key)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid %s", errors_models.ErrValidation, key)
	}
	return &v, nil
}

// QueryString returns the query parameter, or nil if absent.
func QueryString(q url.Values, key string) *string {
	raw := q.Get(key)
	if raw == "" {
		return nil
	}
	return &raw
}

// QueryCSV splits a comma-separated query parameter, or returns nil if
// absent.
func QueryCSV(q url.Values, key string) []string {
	raw := q.Get(key)
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

// QueryCSVInt64 splits a comma-separated query parameter into int64s.
func QueryCSVInt64(q url.Values, key string) ([]int64, error) {
	raw := QueryCSV(q, key)
	if raw == nil {
		return nil, nil
	}
	out := make([]int64, len(raw))
	for i, s := range raw {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid %s", errors_models.ErrValidation, key)
		}
		out[i] = v
	}
	return out, nil
}

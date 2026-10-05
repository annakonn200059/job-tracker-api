package http

import (
	"encoding/json"
	"errors"
	"reflect"
)

// Optional is a request field for PATCH bodies (JSON merge-patch semantics),
// where a missing key and an explicit null mean different things:
//
//	key absent      -> Set == false: leave the stored value alone
//	"key": null     -> Set == true, Value is nil: clear it
//	"key": <value>  -> Set == true, Value holds it
//
// Use a pointer T (Optional[*string]) for fields that can be cleared. For a
// non-pointer T, null is rejected, since there is nothing to clear it to.
type Optional[T any] struct {
	Set   bool
	Value T
}

// UnmarshalJSON is only called when the key is present (null included), so
// a field that stays at its zero value was absent.
func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	if string(data) == "null" && reflect.TypeFor[T]().Kind() != reflect.Pointer {
		return errors.New("field may not be null")
	}
	o.Set = true
	return json.Unmarshal(data, &o.Value)
}

// Apply writes the value to dst if the field was present.
func (o Optional[T]) Apply(dst *T) {
	if o.Set {
		*dst = o.Value
	}
}

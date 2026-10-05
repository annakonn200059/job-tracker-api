// Package contracttest checks HTTP handlers against the hand-written spec in
// api/openapi.yaml. A test sends a request through Do, which fails the test
// if the request or the handler's response doesn't match the spec. That is
// what turns a renamed Go JSON field into a failing test rather than a
// silently stale spec.
package contracttest

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"

	errors_models "github.com/annakonn200059/job-tracker-api/domains/errors"
	apihttp "github.com/annakonn200059/job-tracker-api/http"
)

// Spec loading is shared by every test in a package; a broken spec fails
// each of them with the same error.
var (
	loadOnce sync.Once
	router   routers.Router
	loadErr  error
)

func specPath() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "api", "openapi.yaml")
}

func load() (routers.Router, error) {
	loadOnce.Do(func() {
		doc, err := openapi3.NewLoader().LoadFromFile(specPath())
		if err != nil {
			loadErr = err
			return
		}
		if err := doc.Validate(context.Background()); err != nil {
			loadErr = err
			return
		}
		// The router matches servers[].url too; drop them so routes match on
		// path alone, whatever host httptest puts on the request.
		doc.Servers = nil
		router, loadErr = legacy.NewRouter(doc)
	})
	return router, loadErr
}

var options = &openapi3filter.Options{
	// Security is enforced by the real middleware under test, not by the
	// validator; it only needs to know the schemes exist.
	AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
	// A status code the spec doesn't list is drift too.
	IncludeResponseStatus: true,
	MultiError:            true,
}

// Do serves req through h and returns the recorded response. It fails the
// test if req doesn't match its operation in the spec (a mistake in the
// test) or if the response doesn't (a mismatch between handler and spec).
// Callers still assert the status they expect: a response can match the
// spec and still be the wrong one, e.g. a documented 400.
func Do(t *testing.T, h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()

	rt, err := load()
	if err != nil {
		t.Fatalf("load api/openapi.yaml: %v", err)
	}

	// The validator consumes the body, so keep a copy for the handler.
	var body []byte
	if req.Body != nil {
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(req.Body); err != nil {
			t.Fatalf("read request body: %v", err)
		}
		body = buf.Bytes()
	}
	resetBody := func() {
		req.Body = http.NoBody
		if body != nil {
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
	}

	route, pathParams, err := rt.FindRoute(req)
	if err != nil {
		t.Fatalf("%s %s is not in the spec: %v", req.Method, req.URL.Path, err)
	}
	reqInput := &openapi3filter.RequestValidationInput{
		Request:    req,
		PathParams: pathParams,
		Route:      route,
		Options:    options,
	}

	resetBody()
	if err := openapi3filter.ValidateRequest(req.Context(), reqInput); err != nil {
		t.Fatalf("request does not match the spec (fix the test): %v", err)
	}

	resetBody()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	respInput := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: reqInput,
		Status:                 rec.Code,
		Header:                 rec.Header(),
		Options:                options,
	}
	respInput.SetBodyBytes(rec.Body.Bytes())
	if err := openapi3filter.ValidateResponse(req.Context(), respInput); err != nil {
		t.Errorf("%s %s: response does not match the spec: %v\nbody: %s",
			req.Method, req.URL.Path, err, rec.Body.String())
	}
	return rec
}

// Token is a session token Authenticated accepts, for UserID.
const (
	Token  = "contract-test-token"
	UserID = int64(42)
)

// Authenticated wraps h in the same session middleware main.go uses, with a
// resolver that accepts only Token. Send it with Bearer to act as UserID;
// omit it to exercise the 401 path.
func Authenticated(h http.Handler, publicPaths ...string) http.Handler {
	return apihttp.Authenticate(staticResolver{}, apihttp.RequireAuth(publicPaths, h))
}

type staticResolver struct{}

func (staticResolver) ResolveSession(_ context.Context, token string) (int64, error) {
	if token != Token {
		return 0, errors_models.ErrUnauthorized
	}
	return UserID, nil
}

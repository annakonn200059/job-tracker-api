package http

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"
)

// Readiness reports whether this instance should receive traffic.
type Readiness struct {
	// atomic.Bool because the shutdown goroutine writes this while request
	// goroutines read it. A plain bool is a data race — go test -race says so.
	ready atomic.Bool
}

func NewReadiness() *Readiness {
	r := &Readiness{}
	r.ready.Store(true)
	return r
}

// Unready flips the flag so /readyz starts failing. Called on SIGTERM, before
// draining begins, to give the endpoint controller time to remove this pod
// from Service endpoints.
func (r *Readiness) Unready() { r.ready.Store(false) }

func (r *Readiness) IsReady() bool { return r.ready.Load() }

// Pinger is anything that can check a dependency. *pgxpool.Pool satisfies it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// If the process can answer, it is alive.
func LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}
}

// ReadinessHandler reports 503 when shutting down or when the database is
// unreachable.
func ReadinessHandler(rd *Readiness, db Pinger, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Checked first: during shutdown there is no point touching the
		// database, and the answer is 503 either way.
		if !rd.IsReady() {
			http.Error(w, "shutting down", http.StatusServiceUnavailable)
			return
		}

		// r.Context() so a disconnecting client releases the pooled
		// connection instead of holding it for the full timeout.
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}

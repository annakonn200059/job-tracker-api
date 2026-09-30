package http

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	errors_models "github.com/annakonn200059/job-tracker-api/domains/errors"
)

// SessionCookieName is the cookie carrying the session token for browser
// clients. Non-browser clients send the same token as
// "Authorization: Bearer <token>".
const SessionCookieName = "session"

// SessionResolver maps a session token to its user ID, returning
// errors_models.ErrUnauthorized for unknown or expired tokens. Declared here
// rather than importing the auth service, so this package stays below the
// service layer.
type SessionResolver interface {
	ResolveSession(ctx context.Context, token string) (int64, error)
}

type userIDKey struct{}

// Authenticate resolves the request's session token, if any, and stores the
// user ID in the request context for UserID to read. It never rejects a
// request by itself; RequireAuth, placed inside it, does that.
func Authenticate(resolver SessionResolver, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := SessionToken(r)
		if token == "" {
			next.ServeHTTP(w, r)
			return
		}
		userID, err := resolver.ResolveSession(r.Context(), token)
		switch {
		case err == nil:
			r = r.WithContext(context.WithValue(r.Context(), userIDKey{}, userID))
		case errors.Is(err, errors_models.ErrUnauthorized):
			// Stale token: carry on anonymously.
		default:
			WriteError(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAuth rejects with 401 every request without a valid session,
// except requests to publicPaths (exact path match) and CORS preflights.
// Deny-by-default: a newly added route is protected unless it is
// deliberately listed as public. It must run inside Authenticate.
func RequireAuth(publicPaths []string, next http.Handler) http.Handler {
	public := make(map[string]bool, len(publicPaths))
	for _, p := range publicPaths {
		public[p] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if public[r.URL.Path] || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if _, err := UserID(r); err != nil {
			WriteError(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// UserID returns the authenticated caller's user ID, set by Authenticate,
// or ErrUnauthorized (401) if the request has no valid session.
func UserID(r *http.Request) (int64, error) {
	id, ok := r.Context().Value(userIDKey{}).(int64)
	if !ok {
		return 0, errors_models.ErrUnauthorized
	}
	return id, nil
}

// SessionToken returns the session token from the Authorization header
// (preferred) or the session cookie, or "" if neither is present.
func SessionToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if scheme, token, ok := strings.Cut(h, " "); ok && strings.EqualFold(scheme, "Bearer") {
			return strings.TrimSpace(token)
		}
		return ""
	}
	if c, err := r.Cookie(SessionCookieName); err == nil {
		return c.Value
	}
	return ""
}

// CookieConfig controls the session cookie's attributes.
type CookieConfig struct {
	// Secure must be true anywhere served over HTTPS; false only for
	// plain-http local development.
	Secure bool
	// Domain is optional; set it (e.g. "example.com") when the frontend and
	// API live on different subdomains and both need the cookie.
	Domain string
}

// SetSessionCookie writes the session cookie. HttpOnly keeps it away from
// page JavaScript (XSS can't steal it); SameSite=Lax stops it being sent on
// cross-site POSTs, which is the CSRF defence for the JSON API.
func SetSessionCookie(w http.ResponseWriter, cfg CookieConfig, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Domain:   cfg.Domain,
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   cfg.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie expires the session cookie in the browser.
func ClearSessionCookie(w http.ResponseWriter, cfg CookieConfig) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		Domain:   cfg.Domain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   cfg.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

package auth_service

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"

	auth_models "github.com/annakonn200059/job-tracker-api/domains/auth"
)

const (
	googleIssuer  = "https://accounts.google.com"
	googleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"
)

// GoogleClaims is the subset of a verified Google ID token the service uses.
type GoogleClaims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

// GoogleVerifier checks a Google ID token (the "credential" returned by
// Google Identity Services on the frontend) and returns its claims.
type GoogleVerifier interface {
	Verify(ctx context.Context, rawIDToken string) (*GoogleClaims, error)
}

type oidcGoogleVerifier struct {
	verifier *oidc.IDTokenVerifier
}

// NewGoogleVerifier verifies signature, issuer, expiry, and that the token
// was issued for clientID (the "aud" claim) — without the audience check,
// a token minted for any other site using Google sign-in would be accepted.
// Google's signing keys are fetched lazily and cached, so startup does not
// depend on reaching Google.
func NewGoogleVerifier(clientID string) GoogleVerifier {
	// The key set outlives any single request, so it gets its own context.
	keys := oidc.NewRemoteKeySet(context.Background(), googleJWKSURL)
	return &oidcGoogleVerifier{
		verifier: oidc.NewVerifier(googleIssuer, keys, &oidc.Config{ClientID: clientID}),
	}
}

func (v *oidcGoogleVerifier) Verify(ctx context.Context, rawIDToken string) (*GoogleClaims, error) {
	tok, err := v.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", auth_models.ErrInvalidGoogleToken, err)
	}
	var c struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := tok.Claims(&c); err != nil {
		return nil, fmt.Errorf("%w: %v", auth_models.ErrInvalidGoogleToken, err)
	}
	return &GoogleClaims{
		Subject:       tok.Subject,
		Email:         c.Email,
		EmailVerified: c.EmailVerified,
		Name:          c.Name,
	}, nil
}

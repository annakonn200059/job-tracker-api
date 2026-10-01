package auth_service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	auth_models "github.com/annakonn200059/job-tracker-api/domains/auth"
	apperr "github.com/annakonn200059/job-tracker-api/domains/errors"
	users_models "github.com/annakonn200059/job-tracker-api/domains/users"
	"github.com/annakonn200059/job-tracker-api/internal/infrastructure/database"
	sessions_repo "github.com/annakonn200059/job-tracker-api/repos/sessions"
	users_repo "github.com/annakonn200059/job-tracker-api/repos/users"
)

const bcryptCost = 12

// dummyHash is compared against when the email doesn't exist or has no
// password, so a failed login takes the same time either way and response
// timing doesn't reveal which emails are registered.
var dummyHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("dummy-password"), bcryptCost)
	return h
})

type Service struct {
	pool       *pgxpool.Pool
	users      *users_repo.Repo
	sessions   *sessions_repo.Repo
	google     GoogleVerifier // nil when Google sign-in is not configured
	sessionTTL time.Duration
}

func NewService(
	pool *pgxpool.Pool,
	users *users_repo.Repo,
	sessions *sessions_repo.Repo,
	google GoogleVerifier,
	sessionTTL time.Duration,
) *Service {
	return &Service{pool: pool, users: users, sessions: sessions, google: google, sessionTTL: sessionTTL}
}

func (s *Service) GoogleEnabled() bool { return s.google != nil }

// ClientMeta is recorded on the session for the user's own reference
// (e.g. a future "active sessions" list); it plays no part in validation.
type ClientMeta struct {
	UserAgent string
	IP        string
}

// Result is a signed-in user plus the raw session token. The raw token is
// only ever held in memory here and in the response; the database stores
// its hash.
type Result struct {
	User      *users_models.User
	Token     string
	ExpiresAt time.Time
}

type RegisterParams struct {
	Email       string
	Password    string
	DisplayName *string
}

// Register creates a password account and signs it in. An email that
// already belongs to any account — including a Google-only one — is
// rejected rather than having a password attached to it, since that
// would let whoever registers first take over the Google user's account.
func (s *Service) Register(ctx context.Context, p RegisterParams, meta ClientMeta) (*Result, error) {
	email, err := users_models.NormalizeEmail(p.Email)
	if err != nil {
		return nil, err
	}
	if err := auth_models.ValidatePassword(p.Password); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(p.Password), bcryptCost)
	if err != nil {
		return nil, err
	}
	hashStr := string(hash)

	var res *Result
	err = database.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		u, err := s.users.WithTx(tx).Create(ctx, users_models.User{
			Email:        email,
			PasswordHash: &hashStr,
			DisplayName:  trimmedOrNil(p.DisplayName),
		})
		if err != nil {
			return err
		}
		res, err = s.startSession(ctx, s.sessions.WithTx(tx), u, meta)
		return err
	})
	return res, err
}

func (s *Service) Login(ctx context.Context, email, password string, meta ClientMeta) (*Result, error) {
	email, err := users_models.NormalizeEmail(email)
	if err != nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte(password))
		return nil, auth_models.ErrInvalidCredentials
	}

	u, err := s.users.GetByEmail(ctx, email)
	if err != nil && !errors.Is(err, apperr.ErrNotFound) {
		return nil, err
	}
	if u == nil || u.PasswordHash == nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte(password))
		return nil, auth_models.ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(*u.PasswordHash), []byte(password)) != nil {
		return nil, auth_models.ErrInvalidCredentials
	}

	_ = s.sessions.DeleteExpiredByUser(ctx, u.ID) // best-effort housekeeping
	return s.startSession(ctx, s.sessions, u, meta)
}

// LoginWithGoogle verifies a Google ID token and signs the user in,
// creating or linking an account as needed:
//
//  1. The Google account is already linked → sign in as that user.
//  2. An account with the same email exists → link it. If that account's
//     email was never verified, it was registered with a password by
//     someone who may not own the address, so the password is dropped and
//     its sessions are revoked before linking (pre-account-takeover
//     defence). The real owner can set a password again later.
//  3. Otherwise → create a Google-only account (no password).
func (s *Service) LoginWithGoogle(ctx context.Context, idToken string, meta ClientMeta) (*Result, error) {
	if s.google == nil {
		return nil, apperr.ErrNotFound
	}
	claims, err := s.google.Verify(ctx, idToken)
	if err != nil {
		return nil, err
	}
	if !claims.EmailVerified {
		return nil, auth_models.ErrGoogleEmailUnverified
	}
	email, err := users_models.NormalizeEmail(claims.Email)
	if err != nil {
		return nil, auth_models.ErrInvalidGoogleToken
	}

	var res *Result
	err = database.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		users := s.users.WithTx(tx)
		sessions := s.sessions.WithTx(tx)

		u, err := users.GetByIdentity(ctx, users_models.ProviderGoogle, claims.Subject)
		if err != nil && !errors.Is(err, apperr.ErrNotFound) {
			return err
		}

		if u == nil {
			u, err = users.GetByEmailForUpdate(ctx, email)
			switch {
			case err == nil:
				if u.EmailVerifiedAt == nil {
					if err := sessions.DeleteByUser(ctx, u.ID); err != nil {
						return err
					}
				}
				u, err = users.MarkEmailVerified(ctx, u.ID, u.EmailVerifiedAt == nil)
				if err != nil {
					return err
				}
			case errors.Is(err, apperr.ErrNotFound):
				now := time.Now()
				u, err = users.Create(ctx, users_models.User{
					Email:           email,
					DisplayName:     trimmedOrNil(&claims.Name),
					EmailVerifiedAt: &now,
				})
				if err != nil {
					return err
				}
			default:
				return err
			}

			if err := users.CreateIdentity(ctx, users_models.Identity{
				UserID:   u.ID,
				Provider: users_models.ProviderGoogle,
				Subject:  claims.Subject,
				Email:    &email,
			}); err != nil {
				return err
			}
		}

		res, err = s.startSession(ctx, sessions, u, meta)
		return err
	})
	return res, err
}

// Logout revokes the session. Unknown or already-expired tokens are not an
// error: the caller ends up logged out either way.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.sessions.DeleteByTokenHash(ctx, hashToken(token))
}

// ResolveSession returns the user ID owning an active session token, or
// ErrUnauthorized.
func (s *Service) ResolveSession(ctx context.Context, token string) (int64, error) {
	if token == "" {
		return 0, apperr.ErrUnauthorized
	}
	id, err := s.sessions.UserIDByTokenHash(ctx, hashToken(token))
	if errors.Is(err, apperr.ErrNotFound) {
		return 0, apperr.ErrUnauthorized
	}
	return id, err
}

func (s *Service) Me(ctx context.Context, userID int64) (*users_models.User, error) {
	return s.users.GetByID(ctx, userID)
}

func (s *Service) startSession(ctx context.Context, sessions *sessions_repo.Repo, u *users_models.User, meta ClientMeta) (*Result, error) {
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	sess, err := sessions.Create(ctx, auth_models.Session{
		UserID:    u.ID,
		TokenHash: hashToken(token),
		UserAgent: trimmedOrNil(&meta.UserAgent),
		IP:        trimmedOrNil(&meta.IP),
		ExpiresAt: time.Now().Add(s.sessionTTL),
	})
	if err != nil {
		return nil, err
	}
	return &Result{User: u, Token: token, ExpiresAt: sess.ExpiresAt}, nil
}

// newToken returns 256 bits of randomness, URL-safe so it fits in a cookie
// or Authorization header unescaped.
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken is a plain SHA-256: the token is already high-entropy random,
// so a slow password hash would add latency to every request for nothing.
func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func trimmedOrNil(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return &t
}

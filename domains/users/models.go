package users_models

import (
	"errors"
	"net/mail"
	"strings"
	"time"
)

var (
	ErrEmailTaken   = errors.New("email is already registered")
	ErrInvalidEmail = errors.New("invalid email")
)

type User struct {
	ID              int64
	Email           string
	PasswordHash    *string // nil when the account is OAuth-only
	DisplayName     *string
	Locale          string
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Provider names an external identity provider. Values match the CHECK
// constraint on user_identities.provider.
type Provider string

const ProviderGoogle Provider = "google"

// Identity links a user to an account at an external provider. Subject is
// the provider's stable user ID, never the email.
type Identity struct {
	ID        int64
	UserID    int64
	Provider  Provider
	Subject   string
	Email     *string
	CreatedAt time.Time
}

// NormalizeEmail trims the address and checks it is a bare address
// ("a@b.c", not "Name <a@b.c>"). Case is left alone: the column is CITEXT.
func NormalizeEmail(raw string) (string, error) {
	email := strings.TrimSpace(raw)
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || len(email) > 254 {
		return "", ErrInvalidEmail
	}
	return email, nil
}

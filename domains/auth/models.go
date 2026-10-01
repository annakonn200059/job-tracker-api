package auth_models

import (
	"errors"
	"time"
)

const (
	MinPasswordLen = 8
	// MaxPasswordLen is bcrypt's input limit in bytes; longer input would
	// be silently truncated by most implementations, so it is rejected.
	MaxPasswordLen = 72
)

var (
	// ErrInvalidCredentials is deliberately vague: it does not say whether
	// the email exists, so login can't be used to enumerate accounts.
	ErrInvalidCredentials    = errors.New("invalid email or password")
	ErrPasswordTooShort      = errors.New("password must be at least 8 characters")
	ErrPasswordTooLong       = errors.New("password must be at most 72 bytes")
	ErrInvalidGoogleToken    = errors.New("invalid google id token")
	ErrGoogleEmailUnverified = errors.New("google account email is not verified")
)

type Session struct {
	ID        int64
	UserID    int64
	TokenHash []byte
	UserAgent *string
	IP        *string
	CreatedAt time.Time
	ExpiresAt time.Time
}

func ValidatePassword(p string) error {
	if len([]rune(p)) < MinPasswordLen {
		return ErrPasswordTooShort
	}
	if len(p) > MaxPasswordLen {
		return ErrPasswordTooLong
	}
	return nil
}

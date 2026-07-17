package auth

import (
	"errors"
	"time"
)

// ErrTokenExpired is returned when a presented token is past its expiry.
var ErrTokenExpired = errors.New("token has exipred; request a new one")

// Token is a bearer API token with an absolute expiry.
type Token struct {
	Value     string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// Validate checks the token against the clock.
func (t Token) Validate(now time.Time) error {
	if now.After(t.ExpiresAt) {
		return ErrTokenExpired
	}
	return nil
}

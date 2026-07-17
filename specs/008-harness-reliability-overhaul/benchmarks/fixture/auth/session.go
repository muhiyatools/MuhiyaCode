// Package auth manages operator sessions and API tokens for OrderDesk.
package auth

import (
	"errors"
	"time"
)

// SessionRenewalWindow is how long Renew extends a session.
const SessionRenewalWindow = 30 * time.Minute

// ErrSessionNotFound is returned when a session ID is unknown.
var ErrSessionNotFound = errors.New("session not found; sign in again")

// Session is one authenticated operator session.
type Session struct {
	ID        string
	User      string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Active reports whether the session is still valid at the given time.
func (s Session) Active(now time.Time) bool {
	return now.Before(s.ExpiresAt)
}

// Renew extends a session by the standard renewal window.
func (s *Session) Renew(now time.Time) {
	s.ExpiresAt = now.Add(SessionRenewalWindow)
}

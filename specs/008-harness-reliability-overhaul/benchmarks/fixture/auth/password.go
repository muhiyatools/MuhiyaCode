package auth

import "errors"

// MinPasswordLength is the shortest password OrderDesk accepts.
const MinPasswordLength = 12

// ErrPasswordTooShort is returned when a candidate password is too short.
var ErrPasswordTooShort = errors.New("password must be at least 12 characters")

// CheckPassword validates a candidate password against the local policy.
func CheckPassword(candidate string) error {
	if len(candidate) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	return nil
}

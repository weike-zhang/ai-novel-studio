package auth

import (
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MinAccountNameLength = 4
	MaxAccountNameLength = 20
	MinPasswordLength    = 6
	MaxPasswordLength    = 72
)

// NormalizeAccountName implements AUTH-03. Account names are deliberately
// ASCII-only, so lower-casing is stable across Go, MySQL, and clients.
func NormalizeAccountName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) < MinAccountNameLength || len(value) > MaxAccountNameLength {
		return "", ErrInvalidAccountName
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return "", ErrInvalidAccountName
		}
	}
	return strings.ToLower(value), nil
}

func ValidatePassword(password string) error {
	if !utf8.ValidString(password) {
		return ErrInvalidPassword
	}
	length := utf8.RuneCountInString(password)
	if length < MinPasswordLength || length > MaxPasswordLength {
		return ErrInvalidPassword
	}
	var hasLetter, hasDigit bool
	for _, r := range password {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			hasLetter = true
		}
		if r >= '0' && r <= '9' {
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return ErrInvalidPassword
	}
	return nil
}

type Account struct {
	ID                    string
	AccountName           string
	AccountNameNormalized string
	Nickname              string
	PasswordHash          string
	RecoveryCodeDigest    []byte
	Status                string
	EmailNormalized       string
	EmailCiphertext       []byte
	EmailVerifiedAt       *time.Time
	AdultConfirmedAt      *time.Time
	TermsVersion          string
	CreatedAt             time.Time
}

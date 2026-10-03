package auth

import (
	"net/mail"
	"strings"
)

// NormalizeEmail applies only the provider-independent FR-01 rules. In
// particular, it never removes +tags or dots because their meaning varies by
// provider.
func NormalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 254 || strings.Count(value, "@") != 1 {
		return "", ErrInvalidEmail
	}
	for _, char := range value {
		if char > 127 {
			return "", ErrInvalidEmail
		}
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value {
		return "", ErrInvalidEmail
	}
	return value, nil
}

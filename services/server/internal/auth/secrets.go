package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

const (
	defaultSessionTokenBytes = 32
	defaultRecoveryCodeBytes = 24
	defaultCodeBytes         = 24
	minimumSecretKeyBytes    = 32
)

type SecretManager struct {
	sessionKey []byte
	pepper     []byte
	random     io.Reader
}

func NewSecretManager(sessionDigestKey, recoveryPepper []byte) (*SecretManager, error) {
	return newSecretManager(sessionDigestKey, recoveryPepper, rand.Reader)
}

func newSecretManager(sessionDigestKey, recoveryPepper []byte, random io.Reader) (*SecretManager, error) {
	if len(sessionDigestKey) < minimumSecretKeyBytes || len(recoveryPepper) < minimumSecretKeyBytes || random == nil {
		return nil, ErrInvalidConfiguration
	}
	return &SecretManager{
		sessionKey: append([]byte(nil), sessionDigestKey...),
		pepper:     append([]byte(nil), recoveryPepper...),
		random:     random,
	}, nil
}

func (m *SecretManager) NewSessionToken() (plaintext string, digest []byte, err error) {
	return m.generate(defaultSessionTokenBytes, m.sessionKey, "session-token-v1")
}

func (m *SecretManager) SessionDigest(plaintext string) []byte {
	return keyedDigest(m.sessionKey, "session-token-v1", plaintext)
}

func (m *SecretManager) NewRecoveryCode() (plaintext string, digest []byte, err error) {
	return m.generate(defaultRecoveryCodeBytes, m.pepper, "recovery-code-v1")
}

func (m *SecretManager) RecoveryCodeDigest(plaintext string) []byte {
	return keyedDigest(m.pepper, "recovery-code-v1", plaintext)
}

// NewVerificationCode returns a six-digit numeric registration code. Only the
// digest is stored; the plaintext goes to the user's email and the message
// provider.
func (m *SecretManager) NewVerificationCode() (plaintext string, digest []byte, err error) {
	const digits = "0123456789"
	code := make([]byte, 6)
	buf := make([]byte, 6)
	if _, err := io.ReadFull(m.random, buf); err != nil {
		return "", nil, fmt.Errorf("generate verification-code-v1: %w", err)
	}
	for i := range code {
		code[i] = digits[int(buf[i])%len(digits)]
	}
	plaintext = string(code)
	return plaintext, keyedDigest(m.sessionKey, "verification-code-v1", plaintext), nil
}

func (m *SecretManager) VerificationCodeDigest(plaintext string) []byte {
	return keyedDigest(m.sessionKey, "verification-code-v1", plaintext)
}

// NewRegistrationNonce returns a random token bound to the verification-code
// send, stored as a digest and presented at registration.
func (m *SecretManager) NewRegistrationNonce() (plaintext string, digest []byte, err error) {
	return m.generate(defaultSessionTokenBytes, m.sessionKey, "registration-nonce-v1")
}

func (m *SecretManager) RegistrationNonceDigest(plaintext string) []byte {
	return keyedDigest(m.sessionKey, "registration-nonce-v1", plaintext)
}

// EmailHash is the short-lived HMAC used to key rate limits and risk lookups;
// it never reveals the normalized email.
func (m *SecretManager) EmailHash(normalized string) []byte {
	return keyedDigest(m.sessionKey, "registration-email-v1", normalized)
}

// RiskCookieDigest is the HMAC persisted for a first-party risk cookie; the
// raw random value only ever lives in the browser.
func (m *SecretManager) RiskCookieDigest(value string) []byte {
	return keyedDigest(m.sessionKey, "risk-cookie-v1", value)
}

// IPPrefixHash hides the full client IP while still grouping shared networks.
func (m *SecretManager) IPPrefixHash(prefix string) []byte {
	return keyedDigest(m.sessionKey, "ip-prefix-v1", prefix)
}

func (m *SecretManager) generate(size int, key []byte, purpose string) (string, []byte, error) {
	raw := make([]byte, size)
	if _, err := io.ReadFull(m.random, raw); err != nil {
		return "", nil, fmt.Errorf("generate %s: %w", purpose, err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(raw)
	return plaintext, keyedDigest(key, purpose, plaintext), nil
}

func keyedDigest(key []byte, purpose, plaintext string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(purpose))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(plaintext))
	return mac.Sum(nil)
}

func equalDigest(left, right []byte) bool { return hmac.Equal(left, right) }

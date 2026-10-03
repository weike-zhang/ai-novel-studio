package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
)

var ErrInvalidEmailCiphertext = errors.New("invalid email ciphertext")

const (
	emailCipherKeyPurpose = "email-cipher-key-v1"
	emailCipherNonceSize  = 12
)

// EmailCipher encrypts the original display value of an email with AEAD. The
// normalized email stays plaintext for uniqueness and lookups; only the
// display value is sealed so support and exports never see it.
type EmailCipher struct {
	aead cipher.AEAD
}

func NewEmailCipher(sessionDigestKey []byte) (*EmailCipher, error) {
	key := deriveEmailCipherKey(sessionDigestKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("email cipher key: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("email cipher: %w", err)
	}
	return &EmailCipher{aead: aead}, nil
}

func deriveEmailCipherKey(sessionDigestKey []byte) []byte {
	mac := hmac.New(sha256.New, sessionDigestKey)
	_, _ = mac.Write([]byte(emailCipherKeyPurpose))
	return mac.Sum(nil)
}

func (c *EmailCipher) Seal(nonce []byte, plaintext string) ([]byte, error) {
	if len(nonce) != emailCipherNonceSize {
		return nil, errors.New("email cipher nonce must be 12 bytes")
	}
	return c.aead.Seal(nil, nonce, []byte(plaintext), nil), nil
}

func (c *EmailCipher) Open(nonce, ciphertext []byte) (string, error) {
	if len(nonce) != emailCipherNonceSize {
		return "", errors.New("email cipher nonce must be 12 bytes")
	}
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrInvalidEmailCiphertext
	}
	return string(plaintext), nil
}

// SealEmail returns a self-contained AEAD blob: nonce || ciphertext.
func (c *EmailCipher) SealEmail(nonce []byte, plaintext string) ([]byte, error) {
	sealed, err := c.Seal(nonce, plaintext)
	if err != nil {
		return nil, err
	}
	blob := make([]byte, 0, emailCipherNonceSize+len(sealed))
	blob = append(blob, nonce...)
	blob = append(blob, sealed...)
	return blob, nil
}

// OpenEmail parses a nonce || ciphertext blob produced by SealEmail.
func (c *EmailCipher) OpenEmail(blob []byte) (string, error) {
	if len(blob) < emailCipherNonceSize {
		return "", ErrInvalidEmailCiphertext
	}
	return c.Open(blob[:emailCipherNonceSize], blob[emailCipherNonceSize:])
}

func equalBytes(left, right []byte) bool {
	return subtle.ConstantTimeCompare(left, right) == 1
}

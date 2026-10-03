package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

type Argon2idParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltBytes   uint32
	KeyBytes    uint32
}

func DefaultArgon2idParams() Argon2idParams {
	return Argon2idParams{MemoryKiB: 64 * 1024, Iterations: 3, Parallelism: 2, SaltBytes: 16, KeyBytes: 32}
}

func (p Argon2idParams) validate() error {
	if p.MemoryKiB < 8*1024 || p.MemoryKiB > 1024*1024 ||
		p.Iterations == 0 || p.Iterations > 20 ||
		p.Parallelism == 0 || p.Parallelism > 32 ||
		p.SaltBytes < 16 || p.SaltBytes > 64 ||
		p.KeyBytes < 16 || p.KeyBytes > 64 {
		return ErrInvalidConfiguration
	}
	return nil
}

type PasswordHasher struct {
	params Argon2idParams
	random io.Reader
}

func NewPasswordHasher(params Argon2idParams) (*PasswordHasher, error) {
	return newPasswordHasher(params, rand.Reader)
}

func newPasswordHasher(params Argon2idParams, random io.Reader) (*PasswordHasher, error) {
	if err := params.validate(); err != nil || random == nil {
		return nil, ErrInvalidConfiguration
	}
	return &PasswordHasher{params: params, random: random}, nil
}

func (h *PasswordHasher) Hash(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, h.params.SaltBytes)
	if _, err := io.ReadFull(h.random, salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, h.params.Iterations, h.params.MemoryKiB, h.params.Parallelism, h.params.KeyBytes)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, h.params.MemoryKiB, h.params.Iterations, h.params.Parallelism, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify returns whether the password matches and whether the encoded hash
// should be replaced with one using the current configured parameters.
func (h *PasswordHasher) Verify(password, encoded string) (matches, needsUpgrade bool, err error) {
	params, salt, expected, err := parseArgon2id(encoded)
	if err != nil {
		return false, false, err
	}
	actual := argon2.IDKey([]byte(password), salt, params.Iterations, params.MemoryKiB, params.Parallelism, params.KeyBytes)
	matches = subtle.ConstantTimeCompare(actual, expected) == 1
	needsUpgrade = matches && params != h.params
	return matches, needsUpgrade, nil
}

func parseArgon2id(encoded string) (Argon2idParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return Argon2idParams{}, nil, nil, ErrInvalidCredentials
	}
	parameterParts := strings.Split(parts[3], ",")
	if len(parameterParts) != 3 {
		return Argon2idParams{}, nil, nil, ErrInvalidCredentials
	}
	memory, err := parsePHCParameter(parameterParts[0], "m", 32)
	if err != nil {
		return Argon2idParams{}, nil, nil, ErrInvalidCredentials
	}
	iterations, err := parsePHCParameter(parameterParts[1], "t", 32)
	if err != nil {
		return Argon2idParams{}, nil, nil, ErrInvalidCredentials
	}
	parallelism, err := parsePHCParameter(parameterParts[2], "p", 8)
	if err != nil {
		return Argon2idParams{}, nil, nil, ErrInvalidCredentials
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.Strict().DecodeString(parts[4])
	if err != nil {
		return Argon2idParams{}, nil, nil, ErrInvalidCredentials
	}
	key, err := b64.Strict().DecodeString(parts[5])
	if err != nil {
		return Argon2idParams{}, nil, nil, ErrInvalidCredentials
	}
	params := Argon2idParams{MemoryKiB: uint32(memory), Iterations: uint32(iterations), Parallelism: uint8(parallelism), SaltBytes: uint32(len(salt)), KeyBytes: uint32(len(key))}
	if err := params.validate(); err != nil {
		return Argon2idParams{}, nil, nil, ErrInvalidCredentials
	}
	return params, salt, key, nil
}

func parsePHCParameter(value, name string, bitSize int) (uint64, error) {
	prefix := name + "="
	if !strings.HasPrefix(value, prefix) || len(value) == len(prefix) {
		return 0, ErrInvalidCredentials
	}
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, prefix), 10, bitSize)
	if err != nil {
		return 0, ErrInvalidCredentials
	}
	return parsed, nil
}

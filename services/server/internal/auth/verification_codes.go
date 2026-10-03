package auth

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// VerificationCodeStore persists registration verification codes, their
// nonces, attempt counters, per-email cooldowns and rolling send rate limits.
// Codes are one-time and never stored in plaintext.
type VerificationCodeStore interface {
	// Send stores a fresh code digest + nonce digest for emailHash, enforcing
	// the per-email cooldown and the rolling 24h send cap. On cooldown it
	// returns ErrVerificationCooldown with the remaining seconds.
	Send(ctx context.Context, emailHash []byte, codeDigest, nonceDigest []byte) (time.Duration, error)
	// VerifyAndConsume atomically checks the code and nonce digests and
	// consumes the code on success. Wrong codes increment the attempt counter;
	// the code is destroyed on success, expiry, or when attempts run out.
	VerifyAndConsume(ctx context.Context, emailHash []byte, codeDigest, nonceDigest []byte) error
	// CooldownRemaining returns seconds until the next send is allowed for
	// emailHash (zero when sending is allowed).
	CooldownRemaining(ctx context.Context, emailHash []byte) (time.Duration, error)
}

type VerificationCodeStoreConfig struct {
	CodeTTL          time.Duration
	CodeCooldown     time.Duration
	MaxCodeAttempts  int
	MaxEmailsPer24h  int
	RollingWindow24h time.Duration
}

func DefaultVerificationCodeStoreConfig() VerificationCodeStoreConfig {
	return VerificationCodeStoreConfig{
		CodeTTL:          10 * time.Minute,
		CodeCooldown:     60 * time.Second,
		MaxCodeAttempts:  5,
		MaxEmailsPer24h:  5,
		RollingWindow24h: 24 * time.Hour,
	}
}

type RedisVerificationCodeStore struct {
	client *redis.Client
	config VerificationCodeStoreConfig
}

var _ VerificationCodeStore = (*RedisVerificationCodeStore)(nil)

func NewRedisVerificationCodeStore(client *redis.Client, config VerificationCodeStoreConfig) (*RedisVerificationCodeStore, error) {
	if client == nil || config.CodeTTL <= 0 || config.CodeCooldown <= 0 || config.MaxCodeAttempts <= 0 || config.MaxEmailsPer24h <= 0 || config.RollingWindow24h <= 0 {
		return nil, ErrInvalidConfiguration
	}
	return &RedisVerificationCodeStore{client: client, config: config}, nil
}

const sendScript = `
local count = redis.call('INCR', KEYS[2])
if count == 1 then
  redis.call('EXPIRE', KEYS[2], ARGV[4])
end
if count > tonumber(ARGV[5]) then
  redis.call('DECR', KEYS[2])
  return -2
end
local cooldown = redis.call('SET', KEYS[3], '1', 'EX', ARGV[3], 'NX')
if not cooldown then
  return -1
end
redis.call('HSET', KEYS[1], 'digest', ARGV[1], 'nonce', ARGV[2], 'attempts', '0')
redis.call('PEXPIRE', KEYS[1], ARGV[6])
return 0
`

func (s *RedisVerificationCodeStore) Send(ctx context.Context, emailHash []byte, codeDigest, nonceDigest []byte) (time.Duration, error) {
	codeKey := verificationCodeKey(emailHash)
	rateKey := verificationRateKey(emailHash)
	cooldownKey := verificationCooldownKey(emailHash)
	result, err := s.client.Eval(ctx, sendScript, []string{codeKey, rateKey, cooldownKey},
		hex.EncodeToString(codeDigest),
		hex.EncodeToString(nonceDigest),
		int64(s.config.CodeCooldown/time.Second),
		int64(s.config.RollingWindow24h/time.Second),
		s.config.MaxEmailsPer24h,
		int64(s.config.CodeTTL/time.Millisecond),
	).Int64()
	if err != nil {
		return 0, fmt.Errorf("send verification code: %w", err)
	}
	switch result {
	case -1:
		remaining, cooldownErr := s.CooldownRemaining(ctx, emailHash)
		return remaining, errors.Join(ErrVerificationCooldown, cooldownErr)
	case -2:
		return 0, ErrVerificationRateLimited
	}
	return 0, nil
}

const verifyScript = `
local stored = redis.call('HMGET', KEYS[1], 'digest', 'nonce', 'attempts')
if not stored[1] then
  return -1
end
if stored[1] == ARGV[1] and stored[2] == ARGV[2] then
  redis.call('DEL', KEYS[1])
  return 1
end
local attempts = tonumber(stored[3]) + 1
if attempts >= tonumber(ARGV[3]) then
  redis.call('DEL', KEYS[1])
  return -3
end
redis.call('HSET', KEYS[1], 'attempts', tostring(attempts))
return -2
`

func (s *RedisVerificationCodeStore) VerifyAndConsume(ctx context.Context, emailHash []byte, codeDigest, nonceDigest []byte) error {
	result, err := s.client.Eval(ctx, verifyScript, []string{verificationCodeKey(emailHash)},
		hex.EncodeToString(codeDigest),
		hex.EncodeToString(nonceDigest),
		s.config.MaxCodeAttempts,
	).Int64()
	if err != nil {
		return fmt.Errorf("verify verification code: %w", err)
	}
	switch result {
	case 1:
		return nil
	case -1:
		return ErrVerificationCodeExpired
	case -3:
		return ErrVerificationCodeUsed
	default:
		return ErrVerificationCode
	}
}

func (s *RedisVerificationCodeStore) CooldownRemaining(ctx context.Context, emailHash []byte) (time.Duration, error) {
	ttl, err := s.client.TTL(ctx, verificationCooldownKey(emailHash)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return 0, fmt.Errorf("verification cooldown: %w", err)
	}
	if ttl < 0 {
		return 0, nil
	}
	return ttl, nil
}

func verificationCodeKey(emailHash []byte) string {
	return "novelstudio:auth:regcode:" + hex.EncodeToString(emailHash)
}

func verificationRateKey(emailHash []byte) string {
	return "novelstudio:auth:regrate:" + hex.EncodeToString(emailHash)
}

func verificationCooldownKey(emailHash []byte) string {
	return "novelstudio:auth:regcooldown:" + hex.EncodeToString(emailHash)
}

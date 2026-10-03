package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func openVerificationCodeTestStore(t *testing.T) (*RedisVerificationCodeStore, []byte) {
	t.Helper()
	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	client := redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("REDIS_TEST_PASSWORD")})
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis not reachable at %s: %v", addr, err)
	}
	store, err := NewRedisVerificationCodeStore(client, VerificationCodeStoreConfig{
		CodeTTL:          10 * time.Minute,
		CodeCooldown:     60 * time.Second,
		MaxCodeAttempts:  5,
		MaxEmailsPer24h:  5,
		RollingWindow24h: 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	emailHash := make([]byte, 32)
	if _, err := rand.Read(emailHash); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		client.Del(context.Background(), verificationCodeKey(emailHash), verificationRateKey(emailHash), verificationCooldownKey(emailHash))
	})
	return store, emailHash
}

func randomDigestBytes(t *testing.T, seed byte) []byte {
	t.Helper()
	digest := make([]byte, 32)
	for index := range digest {
		digest[index] = seed
	}
	return digest
}

func TestRedisVerificationCodeSendVerifyAndConsume(t *testing.T) {
	store, emailHash := openVerificationCodeTestStore(t)
	ctx := context.Background()
	codeDigest := randomDigestBytes(t, 1)
	nonceDigest := randomDigestBytes(t, 2)

	if _, err := store.Send(ctx, emailHash, codeDigest, nonceDigest); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if err := store.VerifyAndConsume(ctx, emailHash, codeDigest, nonceDigest); err != nil {
		t.Fatalf("VerifyAndConsume() error = %v", err)
	}
	if err := store.VerifyAndConsume(ctx, emailHash, codeDigest, nonceDigest); !errors.Is(err, ErrVerificationCodeExpired) {
		t.Fatalf("reuse error = %v, want expired", err)
	}
}

func TestRedisVerificationCodeCooldownAndRateLimit(t *testing.T) {
	store, emailHash := openVerificationCodeTestStore(t)
	ctx := context.Background()

	if _, err := store.Send(ctx, emailHash, randomDigestBytes(t, 1), randomDigestBytes(t, 2)); err != nil {
		t.Fatalf("first Send() error = %v", err)
	}
	remaining, err := store.Send(ctx, emailHash, randomDigestBytes(t, 3), randomDigestBytes(t, 4))
	if !errors.Is(err, ErrVerificationCooldown) || remaining <= 0 {
		t.Fatalf("second Send() = %v %v, want cooldown", remaining, err)
	}
	cooldown, err := store.CooldownRemaining(ctx, emailHash)
	if err != nil || cooldown <= 0 {
		t.Fatalf("CooldownRemaining() = %v %v", cooldown, err)
	}
}

func TestRedisVerificationCodeExhaustsAfterMaxAttempts(t *testing.T) {
	store, emailHash := openVerificationCodeTestStore(t)
	ctx := context.Background()
	codeDigest := randomDigestBytes(t, 11)
	nonceDigest := randomDigestBytes(t, 12)

	if _, err := store.Send(ctx, emailHash, codeDigest, nonceDigest); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 4; attempt++ {
		if err := store.VerifyAndConsume(ctx, emailHash, randomDigestBytes(t, 99), nonceDigest); !errors.Is(err, ErrVerificationCode) {
			t.Fatalf("wrong code attempt %d error = %v", attempt, err)
		}
	}
	if err := store.VerifyAndConsume(ctx, emailHash, randomDigestBytes(t, 100), nonceDigest); !errors.Is(err, ErrVerificationCodeUsed) {
		t.Fatalf("exhausted error = %v, want used", err)
	}
	if err := store.VerifyAndConsume(ctx, emailHash, codeDigest, nonceDigest); !errors.Is(err, ErrVerificationCodeExpired) {
		t.Fatalf("after exhaustion error = %v, want expired", err)
	}
}

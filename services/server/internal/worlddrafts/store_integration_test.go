package worlddrafts

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
)

const (
	wdTestUserID  = "018f1000-0000-7000-8000-000000000001"
	wdTestUserID2 = "018f1000-0000-7000-8000-000000000099"
)

func wdIntegrationStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN is not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping test mysql: %v", err)
	}
	wdResetFixture(t, db)
	return NewStore(db), db
}

func wdResetFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	for _, table := range []string{"outbox_events", "world_draft_attempts", "world_draft_jobs", "world_reports", "moderation_decisions", "wallet_entries", "story_events", "turns", "ai_attempts", "ai_jobs", "story_threads", "character_states", "simulations", "worlds", "wallets", "auth_sessions", "users"} {
		if _, err := db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			t.Fatalf("clear %s: %v", table, err)
		}
	}
	statements := []string{
		`INSERT INTO users (id,account_name,nickname) VALUES (UUID_TO_BIN(?,1),'synthetic_worldsmith','测试造境者')`,
		`INSERT INTO users (id,account_name,nickname) VALUES (UUID_TO_BIN(?,1),'synthetic_other','另一位测试者')`,
	}
	args := [][]any{{wdTestUserID}, {wdTestUserID2}}
	for i, statement := range statements {
		if _, err := db.ExecContext(ctx, statement, args[i]...); err != nil {
			t.Fatalf("seed fixture statement %d: %v", i, err)
		}
	}
}

func TestMySQLWorldDraftLifecycle(t *testing.T) {
	store, db := wdIntegrationStore(t)
	ctx := context.Background()

	input := GenerateInput{Topic: "雾港来信", Type: "推理", Boundaries: "不包含现实伤害指导"}
	created, fresh, err := store.Create(ctx, CreateInput{UserID: wdTestUserID, IdempotencyKey: "world-draft-00000001", GenerateInput: input})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !fresh || created.Status != StatusQueued || created.Kind != "world_draft" {
		t.Fatalf("created = %#v, fresh = %v", created, fresh)
	}
	replayed, fresh, err := store.Create(ctx, CreateInput{UserID: wdTestUserID, IdempotencyKey: "world-draft-00000001", GenerateInput: input})
	if err != nil || fresh || replayed.ID != created.ID {
		t.Fatalf("idempotent replay = %#v, fresh=%v, err=%v", replayed, fresh, err)
	}

	processor := NewProcessor(store, FakeProvider{})
	world, err := processor.Process(ctx, created.ID, "integration-worker")
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if world.ID == "" || world.OwnerID != wdTestUserID || world.DraftSource != "ai" || world.Visibility != "private" || world.Status != "draft" {
		t.Fatalf("generated world = %#v", world)
	}

	completed, err := store.Get(ctx, wdTestUserID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != StatusCompleted || completed.Result == nil || completed.Result.ID != world.ID {
		t.Fatalf("completed job = %#v", completed)
	}
	if _, err := store.Get(ctx, wdTestUserID2, created.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("other owner get error=%v", err)
	}

	var outboxCount, attemptCount, worldCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM outbox_events`).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM world_draft_attempts`).Scan(&attemptCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM worlds`).Scan(&worldCount); err != nil {
		t.Fatal(err)
	}
	if outboxCount != 2 || attemptCount != 1 || worldCount != 1 {
		t.Fatalf("outbox=%d attempts=%d worlds=%d", outboxCount, attemptCount, worldCount)
	}

	queued, fresh, err := store.Create(ctx, CreateInput{UserID: wdTestUserID, IdempotencyKey: "world-draft-00000002", GenerateInput: GenerateInput{Topic: "雾港来信·续", Type: "悬疑", Boundaries: "合成边界"}})
	if err != nil || !fresh {
		t.Fatalf("second Create() = %#v fresh=%v err=%v", queued, fresh, err)
	}
	cancelled, err := store.Cancel(ctx, wdTestUserID, queued.ID)
	if err != nil || cancelled.Status != StatusCancelled {
		t.Fatalf("cancelled=%#v err=%v", cancelled, err)
	}
	if _, err := store.Retry(ctx, wdTestUserID, queued.ID); !errors.Is(err, ErrConcurrentState) {
		t.Fatalf("retry cancelled error=%v", err)
	}

	failing, fresh, err := store.Create(ctx, CreateInput{UserID: wdTestUserID, IdempotencyKey: "world-draft-00000003", GenerateInput: GenerateInput{Topic: "必定失败的世界", Type: "冒险", Boundaries: "边界"}})
	if err != nil || !fresh {
		t.Fatalf("third Create() err=%v fresh=%v", err, fresh)
	}
	if _, err := store.BeginAttempt(ctx, failing.ID, "fake", "deterministic-world-v1", "lost-worker"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE world_draft_jobs SET lease_expires_at=DATE_SUB(CURRENT_TIMESTAMP(6),INTERVAL 10 MINUTE) WHERE id=UUID_TO_BIN(?,1)`, failing.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.RecoverExpired(ctx, failing.ID)
	if err != nil || !recovered {
		t.Fatalf("RecoverExpired() recovered=%v err=%v", recovered, err)
	}
	recoveredJob, err := store.GetForProcessing(ctx, failing.ID)
	if err != nil || recoveredJob.Status != StatusQueued {
		t.Fatalf("recovered job=%#v err=%v", recoveredJob, err)
	}
	if _, err := store.BeginAttempt(ctx, recoveredJob.ID, "fake", "deterministic-world-v1", "integration-worker"); err != nil {
		t.Fatal(err)
	}
	if err := store.Fail(ctx, recoveredJob.ID, "", "integration-worker", "generation_failed"); err != nil {
		t.Fatal(err)
	}
	failed, err := store.Get(ctx, wdTestUserID, recoveredJob.ID)
	if err != nil || failed.Status != StatusFailed || failed.ErrorCode != "generation_failed" {
		t.Fatalf("failed=%#v err=%v", failed, err)
	}
	retried, err := store.Retry(ctx, wdTestUserID, failed.ID)
	if err != nil || retried.Status != StatusQueued {
		t.Fatalf("retried=%#v err=%v", retried, err)
	}
}

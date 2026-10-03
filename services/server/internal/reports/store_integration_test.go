package reports

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
)

const (
	rpTestReporterID = "0198b8a0-5001-7000-8000-000000000001"
	rpTestReporter2  = "0198b8a0-5001-7000-8000-000000000002"
	rpTestWorldOwner = "0198b8a0-5001-7000-8000-000000000003"
	rpTestWorldID    = "0198b8a0-5001-7000-8000-000000000101"
)

func rpIntegrationStore(t *testing.T) (*Store, *sql.DB) {
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
	rpResetFixture(t, db)
	return NewStore(db), db
}

func rpResetFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	for _, table := range []string{"outbox_events", "world_reports", "moderation_decisions", "world_draft_attempts", "world_draft_jobs", "wallet_entries", "story_events", "turns", "ai_attempts", "ai_jobs", "story_threads", "character_states", "simulations", "worlds", "wallets", "auth_sessions", "users"} {
		if _, err := db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			t.Fatalf("clear %s: %v", table, err)
		}
	}
	for _, fixture := range []struct {
		id, account, nickname, status string
	}{
		{rpTestReporterID, "report_reporter", "举报测试者", "active"},
		{rpTestReporter2, "report_reporter2", "第二位举报者", "active"},
		{rpTestWorldOwner, "report_owner", "被举报作者", "active"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO users (id,account_name,nickname,status) VALUES (UUID_TO_BIN(?,1),?,?,?)`, fixture.id, fixture.account, fixture.nickname, fixture.status); err != nil {
			t.Fatalf("seed report user: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO worlds (id,owner_id,title,summary,setting_json,visibility,status,version) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),'雾港','一处被举报的世界','{}','public','published',1)`, rpTestWorldID, rpTestWorldOwner); err != nil {
		t.Fatalf("seed report world: %v", err)
	}
}

func TestMySQLReportLifecycle(t *testing.T) {
	store, _ := rpIntegrationStore(t)
	ctx := context.Background()

	report, err := store.Create(ctx, CreateInput{WorldID: rpTestWorldID, ReporterID: rpTestReporterID, Reason: "色情低俗", Note: "章节内含违规描述"})
	if err != nil {
		t.Fatal(err)
	}
	if report.WorldID != rpTestWorldID || report.ReporterID != rpTestReporterID || report.Reason != "色情低俗" || report.Note != "章节内含违规描述" || report.Status != "pending" {
		t.Fatalf("created report = %#v", report)
	}

	if _, err := store.Create(ctx, CreateInput{WorldID: rpTestWorldID, ReporterID: rpTestReporterID, Reason: "其他", Note: "重复举报"}); !errors.Is(err, ErrDuplicateReport) {
		t.Fatalf("duplicate report error = %v", err)
	}

	second, err := store.Create(ctx, CreateInput{WorldID: rpTestWorldID, ReporterID: rpTestReporter2, Reason: "侵权冒用"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != "pending" || second.Note != "" {
		t.Fatalf("second report = %#v", second)
	}
}

func TestMySQLReportValidatesInput(t *testing.T) {
	store, _ := rpIntegrationStore(t)
	ctx := context.Background()

	if _, err := store.Create(ctx, CreateInput{WorldID: rpTestWorldID, ReporterID: rpTestReporterID, Reason: "不存在的原因"}); !errors.Is(err, ErrInvalidReason) {
		t.Fatalf("invalid reason error = %v", err)
	}
	longNote := ""
	for i := 0; i < 301; i++ { longNote += "字" }
	if _, err := store.Create(ctx, CreateInput{WorldID: rpTestWorldID, ReporterID: rpTestReporterID, Reason: "其他", Note: longNote}); !errors.Is(err, ErrInvalidNote) {
		t.Fatalf("overlong note error = %v", err)
	}
	if _, err := store.Create(ctx, CreateInput{WorldID: "0198b8a0-5001-7000-8000-000000000099", ReporterID: rpTestReporterID, Reason: "其他"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing world error = %v", err)
	}
}

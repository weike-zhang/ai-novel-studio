package messaging

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

func TestMySQLOutboxClaimAndConfirm(t *testing.T) {
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN is not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `DELETE FROM outbox_events`); err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO outbox_events(id,aggregate_type,aggregate_id,event_type,payload_json) VALUES(UUID_TO_BIN('018f2000-0000-7000-8000-000000000001',1),'turn_job',UUID_TO_BIN('018f2000-0000-7000-8000-000000000002',1),'turn_job.queued',JSON_OBJECT('jobId','018f2000-0000-7000-8000-000000000002'))`)
	if err != nil {
		t.Fatal(err)
	}
	store := NewSQLForwarder(db)
	events, err := store.Claim(ctx, "integration-publisher", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventType != "turn_job.queued" {
		t.Fatalf("events=%#v", events)
	}
	if err := store.MarkPublished(ctx, "integration-publisher", events[0].ID); err != nil {
		t.Fatal(err)
	}
	var published, attempts int
	if err := db.QueryRowContext(ctx, `SELECT published_at IS NOT NULL,publish_attempts FROM outbox_events WHERE id=UUID_TO_BIN(?,1)`, events[0].ID).Scan(&published, &attempts); err != nil {
		t.Fatal(err)
	}
	if published != 1 || attempts != 1 {
		t.Fatalf("published=%d attempts=%d", published, attempts)
	}
}

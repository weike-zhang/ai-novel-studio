package worlds

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

const (
	integrationOwnerID   = "0198b8a0-1001-7000-8000-000000000001"
	integrationReaderID  = "0198b8a0-1001-7000-8000-000000000002"
	integrationPublicID  = "0198b8a0-1001-7000-8000-000000000003"
	integrationPrivateID = "0198b8a0-1001-7000-8000-000000000004"
	integrationDownID    = "0198b8a0-1001-7000-8000-000000000005"
)

func TestMySQLDiscoveryPrivacyAndFrozenJourney(t *testing.T) {
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN is not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping test mysql: %v", err)
	}
	cleanupWorldFixtures(t, db)
	t.Cleanup(func() { cleanupWorldFixtures(t, db) })

	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users (id,account_name,nickname) VALUES (UUID_TO_BIN(?,1),'world_store_owner','造境者')`, []any{integrationOwnerID}},
		{`INSERT INTO users (id,account_name,nickname) VALUES (UUID_TO_BIN(?,1),'world_store_reader','旅人')`, []any{integrationReaderID}},
		{`INSERT INTO worlds (id,owner_id,title,summary,setting_json,visibility,status,version) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),'公开雾港','可被发现',JSON_OBJECT('genre',JSON_ARRAY('奇幻','探索'),'rules',JSON_ARRAY('事实冻结')),'public','published',3)`, []any{integrationPublicID, integrationOwnerID}},
		{`INSERT INTO worlds (id,owner_id,title,summary,setting_json,visibility,status,version) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),'私密草稿','不可枚举',JSON_OBJECT('type','悬疑','tags',JSON_ARRAY('私密')),'private','draft',7)`, []any{integrationPrivateID, integrationOwnerID}},
		{`INSERT INTO worlds (id,owner_id,title,summary,setting_json,visibility,status,version) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),'下架世界','不可枚举',JSON_OBJECT(),'public','suspended',2)`, []any{integrationDownID, integrationOwnerID}},
	} {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed fixture: %v", err)
		}
	}

	store := NewStore(db)
	page, err := store.ListPublic(ctx, PublicWorldQuery{Keyword: "造境", Type: "奇幻", Tags: []string{"探索"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Worlds) != 1 || page.Worlds[0].ID != integrationPublicID {
		t.Fatalf("public page=%#v", page)
	}
	for _, id := range []string{integrationPrivateID, integrationDownID} {
		if _, err := store.Get(ctx, id, integrationReaderID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get(%s) reader error=%v", id, err)
		}
		if _, err := store.Get(ctx, id, integrationOwnerID); err != nil {
			t.Fatalf("Get(%s) owner error=%v", id, err)
		}
	}
	if _, err := store.CreateJourney(ctx, CreateJourneyInput{UserID: integrationReaderID, WorldID: integrationPrivateID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reader private journey error=%v", err)
	}

	journey, err := store.CreateJourney(ctx, CreateJourneyInput{UserID: integrationOwnerID, WorldID: integrationPrivateID})
	if err != nil {
		t.Fatal(err)
	}
	if journey.WorldVersion != 7 {
		t.Fatalf("journey version=%d", journey.WorldVersion)
	}
	reused, err := store.CreateJourney(ctx, CreateJourneyInput{UserID: integrationOwnerID, WorldID: integrationPrivateID})
	if err != nil {
		t.Fatal(err)
	}
	if reused.ID != journey.ID {
		t.Fatalf("repeated journey created a duplicate: first=%s second=%s", journey.ID, reused.ID)
	}

	// Two simultaneous starts for a new world must converge on one journey.
	type journeyResult struct {
		journey Journey
		err     error
	}
	raceContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	results := make(chan journeyResult, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			created, createErr := store.CreateJourney(raceContext, CreateJourneyInput{UserID: integrationOwnerID, WorldID: integrationPublicID})
			results <- journeyResult{journey: created, err: createErr}
		}()
	}
	wait.Wait()
	close(results)
	var concurrentID string
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if concurrentID == "" {
			concurrentID = result.journey.ID
		} else if result.journey.ID != concurrentID {
			t.Fatalf("concurrent journeys diverged: first=%s second=%s", concurrentID, result.journey.ID)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE worlds SET version=8,setting_json=JSON_OBJECT('changed',TRUE) WHERE id=UUID_TO_BIN(?,1)`, integrationPrivateID); err != nil {
		t.Fatal(err)
	}
	var storedVersion int64
	var storedSnapshot []byte
	if err := db.QueryRowContext(ctx, `SELECT world_version,world_snapshot_json FROM simulations WHERE id=UUID_TO_BIN(?,1)`, journey.ID).Scan(&storedVersion, &storedSnapshot); err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		World struct {
			Version int64 `json:"version"`
		} `json:"world"`
		Setting map[string]json.RawMessage `json:"setting"`
	}
	if err := json.Unmarshal(storedSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if storedVersion != 7 || snapshot.World.Version != 7 || snapshot.Setting["type"] == nil || snapshot.Setting["tags"] == nil || snapshot.Setting["changed"] != nil {
		t.Fatalf("snapshot was not frozen: version=%d snapshot=%s", storedVersion, storedSnapshot)
	}
}

func cleanupWorldFixtures(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	worldIDs := []string{integrationPublicID, integrationPrivateID, integrationDownID}
	for _, id := range worldIDs {
		if _, err := db.ExecContext(ctx, `DELETE FROM world_reports WHERE world_id=UUID_TO_BIN(?,1)`, id); err != nil {
			t.Fatalf("cleanup world reports: %v", err)
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM simulations WHERE world_id=UUID_TO_BIN(?,1)`, id); err != nil {
			t.Fatalf("cleanup simulations: %v", err)
		}
	}
	for _, id := range worldIDs {
		if _, err := db.ExecContext(ctx, `DELETE FROM worlds WHERE id=UUID_TO_BIN(?,1)`, id); err != nil {
			t.Fatalf("cleanup worlds: %v", err)
		}
	}
	for _, id := range []string{integrationOwnerID, integrationReaderID} {
		if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id=UUID_TO_BIN(?,1)`, id); err != nil {
			t.Fatalf("cleanup users: %v", err)
		}
	}
}

func TestMySQLListMyWorlds(t *testing.T) {
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN is not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping test mysql: %v", err)
	}
	cleanupWorldFixtures(t, db)
	t.Cleanup(func() { cleanupWorldFixtures(t, db) })

	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users (id,account_name,nickname) VALUES (UUID_TO_BIN(?,1),'world_store_owner','造境者')`, []any{integrationOwnerID}},
		{`INSERT INTO worlds (id,owner_id,title,summary,setting_json,visibility,status,version) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),'公开雾港','可被发现',JSON_OBJECT('genre',JSON_ARRAY('奇幻')),'public','published',3)`, []any{integrationPublicID, integrationOwnerID}},
		{`INSERT INTO worlds (id,owner_id,title,summary,setting_json,visibility,status,version) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),'私密草稿','不可枚举',JSON_OBJECT('type','悬疑'),'private','draft',7)`, []any{integrationPrivateID, integrationOwnerID}},
	} {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed fixture: %v", err)
		}
	}

	store := NewStore(db)
	worlds, err := store.ListMyWorlds(ctx, integrationOwnerID)
	if err != nil {
		t.Fatal(err)
	}
	if len(worlds) != 2 {
		t.Fatalf("ListMyWorlds() = %d worlds, want 2", len(worlds))
	}
	seen := map[string]MyWorldSummary{}
	for _, world := range worlds {
		seen[world.ID] = world
	}
	if seen[integrationPublicID].Status != "published" || seen[integrationPrivateID].Status != "draft" {
		t.Fatalf("ListMyWorlds() = %#v", worlds)
	}
}

func TestMySQLModeratorSuspendAndRestore(t *testing.T) {
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN is not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping test mysql: %v", err)
	}
	cleanupWorldFixtures(t, db)
	t.Cleanup(func() { cleanupWorldFixtures(t, db) })

	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users (id,account_name,nickname) VALUES (UUID_TO_BIN(?,1),'world_store_owner','造境者')`, []any{integrationOwnerID}},
		{`INSERT INTO worlds (id,owner_id,title,summary,setting_json,visibility,status,version) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),'公开雾港','可被发现',JSON_OBJECT('type','奇幻'),'public','published',3)`, []any{integrationPublicID, integrationOwnerID}},
		{`INSERT INTO worlds (id,owner_id,title,summary,setting_json,visibility,status,version) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),'下架世界','不可枚举',JSON_OBJECT(),'public','suspended',2)`, []any{integrationDownID, integrationOwnerID}},
	} {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed fixture: %v", err)
		}
	}

	store := NewStore(db)

	suspended, err := store.SuspendByModerator(ctx, integrationPublicID)
	if err != nil {
		t.Fatalf("SuspendByModerator: %v", err)
	}
	if suspended.Status != "suspended" || suspended.Visibility != "private" {
		t.Fatalf("after suspend = %#v", suspended)
	}
	page, err := store.ListPublic(ctx, PublicWorldQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, world := range page.Worlds {
		if world.ID == integrationPublicID {
			t.Fatalf("suspended world still public: %#v", world)
		}
	}
	if _, err := store.Get(ctx, integrationPublicID, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get suspended world err = %v, want ErrNotFound", err)
	}

	restored, err := store.RestoreByModerator(ctx, integrationPublicID)
	if err != nil {
		t.Fatalf("RestoreByModerator: %v", err)
	}
	if restored.Status != "published" || restored.Visibility != "public" {
		t.Fatalf("after restore = %#v", restored)
	}

	if _, err := store.SuspendByModerator(ctx, integrationDownID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("suspend non-published err = %v, want ErrNotFound", err)
	}
}

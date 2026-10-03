package worlds

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

type fixedPublicationModerator struct {
	decision ModerationDecision
}

func (f fixedPublicationModerator) ReviewPublication(context.Context, PublicationContent) (ModerationDecision, error) {
	return f.decision, nil
}

const (
	authoringOwnerID     = "0198b8a0-2001-7000-8000-000000000001"
	authoringOtherID     = "0198b8a0-2001-7000-8000-000000000002"
	authoringSuspendedID = "0198b8a0-2001-7000-8000-000000000003"
)

func TestMySQLWorldAuthoringLifecycle(t *testing.T) {
	db := openWorldAuthoringTestDB(t)
	ctx := context.Background()
	cleanupAuthoringFixtures(t, db)
	t.Cleanup(func() { cleanupAuthoringFixtures(t, db) })
	seedAuthoringUsers(t, db)
	store := NewStoreWithPublicationModerator(db, SyntheticAllowPublicationModerator{})

	manual, err := store.CreateDraft(ctx, CreateDraftInput{OwnerID: authoringOwnerID, Title: "  手工世界  ", Source: DraftSourceManual})
	if err != nil {
		t.Fatal(err)
	}
	if manual.Title != "手工世界" || manual.Summary != "" || manual.Visibility != "private" || manual.Status != "draft" || manual.Version != 1 || manual.DraftSource != DraftSourceManual {
		t.Fatalf("manual draft = %#v", manual)
	}
	if _, err := store.Publish(ctx, ChangePublicationInput{OwnerID: authoringOwnerID, WorldID: manual.ID, ExpectedVersion: 1}); !isValidationError(err) {
		t.Fatalf("incomplete publish error = %v", err)
	}

	fullSetting := publishableSetting("夜航员")
	manual, err = store.UpdateDraft(ctx, UpdateDraftInput{OwnerID: authoringOwnerID, WorldID: manual.ID, ExpectedVersion: 1, Title: "手工世界", Summary: "一段完整简介", Setting: fullSetting})
	if err != nil {
		t.Fatal(err)
	}
	if manual.Version != 2 {
		t.Fatalf("updated version = %d", manual.Version)
	}
	if _, err := store.UpdateDraft(ctx, UpdateDraftInput{OwnerID: authoringOwnerID, WorldID: manual.ID, ExpectedVersion: 1, Title: "旧客户端覆盖", Setting: fullSetting}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale update error = %v", err)
	}
	if _, err := store.UpdateDraft(ctx, UpdateDraftInput{OwnerID: authoringOtherID, WorldID: manual.ID, ExpectedVersion: 2, Title: "越权覆盖", Setting: fullSetting}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner update error = %v", err)
	}

	manual, err = store.Publish(ctx, ChangePublicationInput{OwnerID: authoringOwnerID, WorldID: manual.ID, ExpectedVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	if manual.Version != 3 || manual.Visibility != "public" || manual.Status != "published" || manual.PublishedAt == nil {
		t.Fatalf("published world = %#v", manual)
	}
	if _, err := store.Publish(ctx, ChangePublicationInput{OwnerID: authoringOwnerID, WorldID: manual.ID, ExpectedVersion: 3}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("double publish error = %v", err)
	}

	manual, err = store.Unpublish(ctx, ChangePublicationInput{OwnerID: authoringOwnerID, WorldID: manual.ID, ExpectedVersion: 3})
	if err != nil {
		t.Fatal(err)
	}
	if manual.Version != 4 || manual.Visibility != "private" || manual.Status != "draft" || manual.UnpublishedAt == nil {
		t.Fatalf("unpublished world = %#v", manual)
	}
}

func TestMySQLAIDraftNeedsCurrentExplicitHumanConfirmation(t *testing.T) {
	db := openWorldAuthoringTestDB(t)
	ctx := context.Background()
	cleanupAuthoringFixtures(t, db)
	t.Cleanup(func() { cleanupAuthoringFixtures(t, db) })
	seedAuthoringUsers(t, db)
	store := NewStoreWithPublicationModerator(db, SyntheticAllowPublicationModerator{})

	aiDraft, err := store.CreateDraft(ctx, CreateDraftInput{OwnerID: authoringOwnerID, Title: "AI 世界", Summary: "完整简介", Setting: publishableSetting("巡界者"), Source: DraftSourceAI})
	if err != nil {
		t.Fatal(err)
	}
	if aiDraft.Visibility != "private" || aiDraft.Status != "draft" || aiDraft.DraftSource != DraftSourceAI {
		t.Fatalf("AI draft escaped private draft state: %#v", aiDraft)
	}
	if _, err := store.Publish(ctx, ChangePublicationInput{OwnerID: authoringOwnerID, WorldID: aiDraft.ID, ExpectedVersion: 1}); !errors.Is(err, ErrHumanConfirmationRequired) {
		t.Fatalf("unconfirmed AI publish error = %v", err)
	}
	if _, err := store.ConfirmAIFields(ctx, ConfirmAIFieldsInput{OwnerID: authoringOwnerID, WorldID: aiDraft.ID, ExpectedVersion: 1, Fields: []string{"title"}}); !isValidationError(err) {
		t.Fatalf("partial confirmation error = %v", err)
	}

	aiDraft, err = store.ConfirmAIFields(ctx, ConfirmAIFieldsInput{OwnerID: authoringOwnerID, WorldID: aiDraft.ID, ExpectedVersion: 1, Fields: RequiredPublishFieldNames()})
	if err != nil {
		t.Fatal(err)
	}
	if !hasCurrentHumanConfirmation(aiDraft) {
		t.Fatal("complete current confirmation was not persisted")
	}
	aiDraft, err = store.UpdateDraft(ctx, UpdateDraftInput{OwnerID: authoringOwnerID, WorldID: aiDraft.ID, ExpectedVersion: 1, Title: aiDraft.Title, Summary: "确认后又修改", Setting: aiDraft.Setting})
	if err != nil {
		t.Fatal(err)
	}
	if aiDraft.Version != 2 || aiDraft.HumanConfirmedVersion != nil || len(aiDraft.HumanConfirmedFields) != 0 || aiDraft.HumanConfirmedAt != nil {
		t.Fatalf("edit did not invalidate confirmation: %#v", aiDraft)
	}
	if _, err := store.Publish(ctx, ChangePublicationInput{OwnerID: authoringOwnerID, WorldID: aiDraft.ID, ExpectedVersion: 2}); !errors.Is(err, ErrHumanConfirmationRequired) {
		t.Fatalf("stale confirmation publish error = %v", err)
	}
	aiDraft, err = store.ConfirmAIFields(ctx, ConfirmAIFieldsInput{OwnerID: authoringOwnerID, WorldID: aiDraft.ID, ExpectedVersion: 2, Fields: RequiredPublishFieldNames()})
	if err != nil {
		t.Fatal(err)
	}
	aiDraft, err = store.Publish(ctx, ChangePublicationInput{OwnerID: authoringOwnerID, WorldID: aiDraft.ID, ExpectedVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	if aiDraft.Status != "published" || aiDraft.Visibility != "public" || aiDraft.Version != 3 {
		t.Fatalf("confirmed AI publish = %#v", aiDraft)
	}
}

func TestMySQLSuspendedOwnerCannotPublish(t *testing.T) {
	db := openWorldAuthoringTestDB(t)
	ctx := context.Background()
	cleanupAuthoringFixtures(t, db)
	t.Cleanup(func() { cleanupAuthoringFixtures(t, db) })
	seedAuthoringUsers(t, db)
	store := NewStoreWithPublicationModerator(db, SyntheticAllowPublicationModerator{})

	draft, err := store.CreateDraft(ctx, CreateDraftInput{OwnerID: authoringSuspendedID, Title: "暂停作者草稿", Summary: "完整简介", Setting: publishableSetting("守门人")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(ctx, ChangePublicationInput{OwnerID: authoringSuspendedID, WorldID: draft.ID, ExpectedVersion: draft.Version}); !errors.Is(err, ErrOwnerSuspended) {
		t.Fatalf("suspended publish error = %v", err)
	}
	stored, err := store.loadAuthoringWorld(ctx, nil, authoringSuspendedID, draft.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "draft" || stored.Visibility != "private" || stored.Version != 1 {
		t.Fatalf("suspended publication changed draft: %#v", stored)
	}
}

func TestMySQLPublicationModerationIsAuditedAndFailsClosed(t *testing.T) {
	db := openWorldAuthoringTestDB(t)
	ctx := context.Background()
	cleanupAuthoringFixtures(t, db)
	t.Cleanup(func() { cleanupAuthoringFixtures(t, db) })
	seedAuthoringUsers(t, db)

	decision := ModerationDecision{Action: ModerationManualReview, Category: "high_risk", ReasonCode: "needs_human_review", RuleVersion: "test-v1", Source: "fake"}
	store := NewStoreWithPublicationModerator(db, fixedPublicationModerator{decision: decision})
	draft, err := store.CreateDraft(ctx, CreateDraftInput{OwnerID: authoringOwnerID, Title: "待审世界", Summary: "完整简介", Setting: publishableSetting("巡界者"), Source: DraftSourceManual})
	if err != nil {
		t.Fatal(err)
	}
	draft, err = store.Publish(ctx, ChangePublicationInput{OwnerID: authoringOwnerID, WorldID: draft.ID, ExpectedVersion: draft.Version})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Status != "pending_review" || draft.Visibility != "private" || draft.Version != 2 {
		t.Fatalf("pending world = %#v", draft)
	}
	var decisionCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM moderation_decisions WHERE content_id=UUID_TO_BIN(?,1) AND content_version=1 AND action='manual_review'`, draft.ID).Scan(&decisionCount); err != nil || decisionCount != 1 {
		t.Fatalf("moderation decisions=%d err=%v", decisionCount, err)
	}
	page, err := store.ListPublic(ctx, PublicWorldQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, world := range page.Worlds {
		if world.ID == draft.ID {
			t.Fatal("pending-review world leaked into public discovery")
		}
	}
	draft, err = store.Unpublish(ctx, ChangePublicationInput{OwnerID: authoringOwnerID, WorldID: draft.ID, ExpectedVersion: draft.Version})
	if err != nil || draft.Status != "draft" || draft.Version != 3 {
		t.Fatalf("withdraw pending review = %#v err=%v", draft, err)
	}
}

func openWorldAuthoringTestDB(t *testing.T) *sql.DB {
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
	return db
}

func seedAuthoringUsers(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, fixture := range []struct {
		id, account, nickname, status string
	}{
		{authoringOwnerID, "authoring_owner", "合成作者", "active"},
		{authoringOtherID, "authoring_other", "合成他人", "active"},
		{authoringSuspendedID, "authoring_suspended", "合成暂停作者", "suspended"},
	} {
		if _, err := db.Exec(`INSERT INTO users (id,account_name,nickname,status) VALUES (UUID_TO_BIN(?,1),?,?,?)`, fixture.id, fixture.account, fixture.nickname, fixture.status); err != nil {
			t.Fatalf("seed authoring user: %v", err)
		}
	}
}

func cleanupAuthoringFixtures(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, id := range []string{authoringOwnerID, authoringOtherID, authoringSuspendedID} {
		if _, err := db.Exec(`DELETE wda FROM world_draft_attempts wda JOIN world_draft_jobs wdj ON wdj.id=wda.job_id WHERE wdj.user_id=UUID_TO_BIN(?,1)`, id); err != nil {
			t.Fatalf("cleanup world draft attempts: %v", err)
		}
		if _, err := db.Exec(`DELETE FROM world_draft_jobs WHERE user_id=UUID_TO_BIN(?,1)`, id); err != nil {
			t.Fatalf("cleanup world draft jobs: %v", err)
		}
		if _, err := db.Exec(`DELETE md FROM moderation_decisions md JOIN worlds w ON w.id=md.content_id WHERE w.owner_id=UUID_TO_BIN(?,1)`, id); err != nil {
			t.Fatalf("cleanup moderation decisions: %v", err)
		}
		if _, err := db.Exec(`DELETE FROM world_reports WHERE reporter_id=UUID_TO_BIN(?,1) OR world_id IN (SELECT id FROM worlds WHERE owner_id=UUID_TO_BIN(?,1))`, id, id); err != nil {
			t.Fatalf("cleanup world reports: %v", err)
		}
		if _, err := db.Exec(`DELETE FROM worlds WHERE owner_id=UUID_TO_BIN(?,1)`, id); err != nil {
			t.Fatalf("cleanup authored worlds: %v", err)
		}
		if _, err := db.Exec(`DELETE FROM users WHERE id=UUID_TO_BIN(?,1)`, id); err != nil {
			t.Fatalf("cleanup authoring user: %v", err)
		}
	}
}

func publishableSetting(player string) json.RawMessage {
	payload, err := json.Marshal(map[string]any{
		"worldBackground": "两座界门之间的城",
		"playerIdentity":  map[string]any{"background": player},
		"characters":      []any{map[string]any{"name": "岚", "role": "向导"}},
		"opening":         map[string]any{"text": "界门在夜里开启"},
		"contentRating":   "16+",
	})
	if err != nil {
		panic(err)
	}
	return payload
}

func isValidationError(err error) bool {
	var validation *ValidationError
	return errors.As(err, &validation)
}

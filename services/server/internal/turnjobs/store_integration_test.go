package turnjobs

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/weike-zhang/ai-novel-studio/services/server/internal/achievements"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/storyengine"
)

const (
	testUserID       = "018f1000-0000-7000-8000-000000000001"
	testWorldID      = "018f1000-0000-7000-8000-000000000002"
	testSimulationID = "018f1000-0000-7000-8000-000000000003"
)

func integrationStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_TEST_DSN is not set")
	}
	db, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping test mysql: %v", err)
	}
	resetFixture(t, db)
	return NewStore(db), db
}

func resetFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	for _, table := range []string{"benefit_grants", "user_benefits", "checkins", "achievement_claims", "outbox_events", "world_draft_attempts", "world_draft_jobs", "world_reports", "moderation_decisions", "wallet_entries", "story_events", "turns", "ai_attempts", "ai_jobs", "story_threads", "character_states", "simulations", "worlds", "wallets", "auth_sessions", "users"} {
		if _, err := db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			t.Fatalf("clear %s: %v", table, err)
		}
	}
	statements := []string{
		`INSERT INTO users (id,account_name,nickname) VALUES (UUID_TO_BIN(?,1),'synthetic_reader','测试旅人')`,
		`INSERT INTO wallets (user_id,available_balance,reserved_balance) VALUES (UUID_TO_BIN(?,1),30,0)`,
		`INSERT INTO worlds (id,owner_id,title,summary,setting_json,visibility,status,version) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),'雾港来信','合成测试世界',JSON_OBJECT('player',JSON_OBJECT('role','归乡人')),'public','published',1)`,
		`INSERT INTO simulations (id,user_id,world_id,world_version,world_snapshot_json,state_json,summary,status,version,last_turn_seq) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),1,JSON_OBJECT('title','雾港来信','rules',JSON_ARRAY('硬事实不可重写')),JSON_OBJECT('location','旧港'),'','active',1,0)`,
		`INSERT INTO character_states (simulation_id,character_id,name,identity_json,current_state_json,version) VALUES (UUID_TO_BIN(?,1),'lin-lan','林岚',JSON_OBJECT('name','林岚','role','守信人'),JSON_OBJECT('location','旧港','trust',0),1)`,
	}
	args := [][]any{{testUserID}, {testUserID}, {testWorldID, testUserID}, {testSimulationID, testUserID, testWorldID}, {testSimulationID}}
	for i, statement := range statements {
		if _, err := db.ExecContext(ctx, statement, args[i]...); err != nil {
			t.Fatalf("seed fixture statement %d: %v", i, err)
		}
	}
}

func TestMySQLListSimulations(t *testing.T) {
	store, _ := integrationStore(t)
	ctx := context.Background()
	summaries, err := store.ListSimulations(ctx, testUserID)
	if err != nil {
		t.Fatalf("ListSimulations() error = %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != testSimulationID || summaries[0].WorldID != testWorldID || summaries[0].LastTurnSeq != 0 || summaries[0].LastSceneNo != 1 {
		t.Fatalf("ListSimulations() = %#v", summaries)
	}
}

func TestMySQLTurnJobLifecycle(t *testing.T) {
	store, db := integrationStore(t)
	ctx := context.Background()
	created, fresh, err := store.Create(ctx, CreateInput{UserID: testUserID, SimulationID: testSimulationID, IdempotencyKey: "turn-request-00000001", Action: "沿着潮湿的石阶向下走", Credits: MaxTurnCredits})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !fresh || created.Status != StatusQueued {
		t.Fatalf("created = %#v, fresh = %v", created, fresh)
	}
	replayed, fresh, err := store.Create(ctx, CreateInput{UserID: testUserID, SimulationID: testSimulationID, IdempotencyKey: "turn-request-00000001", Action: "沿着潮湿的石阶向下走", Credits: MaxTurnCredits})
	if err != nil || fresh || replayed.ID != created.ID {
		t.Fatalf("idempotent replay = %#v, fresh=%v, err=%v", replayed, fresh, err)
	}
	assertWallet(t, db, 18, 12)

	processor := NewProcessor(store, FakeProvider{}, achievements.NewStore(db))
	cost := TurnCost(len([]rune(fakeTurnNarrative)))
	turn, err := processor.Process(ctx, created.ID, "integration-worker")
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if turn.Seq != 1 || turn.Proposal.Narrative == "" {
		t.Fatalf("committed turn = %#v", turn)
	}
	if turn.SceneNo != 1 || turn.SceneTitle != "" || turn.SceneClosure != "" || turn.Interaction != InteractionAct {
		t.Fatalf("continuation turn scene/interaction = %#v", turn)
	}
	completed, err := store.Get(ctx, testUserID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != StatusCompleted || completed.CommittedTurn == nil {
		t.Fatalf("completed job = %#v", completed)
	}
	assertWallet(t, db, int64(33-cost), 0)
	var eventCount, turnCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM story_events`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM turns`).Scan(&turnCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 || turnCount != 1 {
		t.Fatalf("events=%d turns=%d", eventCount, turnCount)
	}
	snapshot, err := store.GetSimulationSnapshot(ctx, testUserID, testSimulationID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.LastTurnSeq != 1 || len(snapshot.RecentTurns) != 1 || snapshot.RecentTurns[0].Proposal.Narrative == "" {
		t.Fatalf("simulation snapshot=%#v", snapshot)
	}
	if snapshot.LastSceneNo != 1 || snapshot.RecentTurns[0].SceneNo != 1 {
		t.Fatalf("snapshot scene=%#v", snapshot)
	}
	if _, err := store.GetSimulationSnapshot(ctx, "018f1000-0000-7000-8000-000000000099", testSimulationID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other owner snapshot error=%v", err)
	}

	cancelJob, _, err := store.Create(ctx, CreateInput{UserID: testUserID, SimulationID: testSimulationID, IdempotencyKey: "turn-request-00000002", Action: "先留在原地观察", Credits: MaxTurnCredits})
	if err != nil {
		t.Fatal(err)
	}
	assertWallet(t, db, int64(21-cost), 12)
	if err := store.Cancel(ctx, testUserID, cancelJob.ID); err != nil {
		t.Fatal(err)
	}
	assertWallet(t, db, int64(33-cost), 0)
	cancelled, err := store.Get(ctx, testUserID, cancelJob.ID)
	if err != nil || cancelled.Status != StatusCancelled {
		t.Fatalf("cancelled job=%#v err=%v", cancelled, err)
	}
	if _, err := store.Retry(ctx, testUserID, cancelJob.ID); !errors.Is(err, ErrCannotRetry) {
		t.Fatalf("retry cancelled error=%v", err)
	}

	interrupted, _, err := store.Create(ctx, CreateInput{UserID: testUserID, SimulationID: testSimulationID, IdempotencyKey: "turn-request-00000003", Action: "跟随灯影继续前进", Credits: MaxTurnCredits})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := store.BeginAttempt(ctx, interrupted, "fake", "deterministic-story-v1", "lost-worker")
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Number != 1 {
		t.Fatalf("attempt number=%d", attempt.Number)
	}
	assertWallet(t, db, int64(21-cost), 12)
	recovered, err := store.RecoverForRedelivery(ctx, interrupted.ID)
	if err != nil || !recovered {
		t.Fatalf("recovered=%v err=%v", recovered, err)
	}
	recoveredJob, err := store.Get(ctx, testUserID, interrupted.ID)
	if err != nil || recoveredJob.Status != StatusQueued {
		t.Fatalf("recovered job=%#v err=%v", recoveredJob, err)
	}
	turn, err = processor.Process(ctx, interrupted.ID, "replacement-worker")
	if err != nil || turn.Seq != 2 {
		t.Fatalf("recovered Process() turn=%#v err=%v", turn, err)
	}
	assertWallet(t, db, int64(33-2*cost), 0)
}

func assertWallet(t *testing.T, db *sql.DB, available, reserved int64) {
	t.Helper()
	var gotAvailable, gotReserved int64
	if err := db.QueryRow(`SELECT available_balance,reserved_balance FROM wallets WHERE user_id=UUID_TO_BIN(?,1)`, testUserID).Scan(&gotAvailable, &gotReserved); err != nil {
		t.Fatal(err)
	}
	if gotAvailable != available || gotReserved != reserved {
		t.Fatalf("wallet=(%d,%d), want=(%d,%d)", gotAvailable, gotReserved, available, reserved)
	}
}

// sceneBreakingProvider opens a new scene on every plan, so the integration test
// can verify the scene number, title and closure are committed atomically.
type sceneBreakingProvider struct{}

const sceneBreakNarrative = "天亮前你离开旧港。一夜之后，雨落在窄巷的瓦檐上，你抵达了新的场景。"
const achievementNarrative = "第二声钟响起，你没有回答。雾把码头围成一座岛。"

func (sceneBreakingProvider) Name() string  { return "fake" }
func (sceneBreakingProvider) Model() string { return "deterministic-story-v1" }

func (sceneBreakingProvider) GenerateTurnPlan(_ context.Context, continuity storyengine.ContinuityContext, action, _ string) (storyengine.TurnProposal, map[string]any, error) {
	sim := continuity.Simulation
	return storyengine.TurnProposal{
		InputVersion: sim.Version, ExpectedNextSeq: sim.LastSeq + 1,
		Events: []storyengine.StoryEvent{{Type: "scene_changed", SubjectRef: "player", Payload: map[string]any{"action": action}}},
		Scene:  storyengine.SceneProposal{StartsNewScene: true, Title: "雨巷", Closure: "你离开旧港，一夜之后抵达落雨的窄巷。"},
	}, map[string]any{}, nil
}

func (sceneBreakingProvider) GenerateNarrativeDraft(_ context.Context, _ storyengine.ContinuityContext, _, _ string, _ storyengine.TurnProposal) (NarrativeDraft, map[string]any, error) {
	return NarrativeDraft{Narrative: sceneBreakNarrative, Choices: []string{"敲门", "继续前进", "回头查看"}}, map[string]any{}, nil
}

func TestMySQLSceneBreakPersists(t *testing.T) {
	store, db := integrationStore(t)
	ctx := context.Background()
	created, _, err := store.Create(ctx, CreateInput{UserID: testUserID, SimulationID: testSimulationID, IdempotencyKey: "scene-break-request-0001", Action: "天亮后前往雨巷", Credits: MaxTurnCredits})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	turn, err := NewProcessor(store, sceneBreakingProvider{}, achievements.NewStore(db)).Process(ctx, created.ID, "integration-worker")
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if turn.SceneNo != 2 || turn.SceneTitle != "雨巷" || turn.SceneClosure == "" {
		t.Fatalf("scene break turn = %#v", turn)
	}
	snapshot, err := store.GetSimulationSnapshot(ctx, testUserID, testSimulationID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.LastSceneNo != 2 || snapshot.RecentTurns[0].SceneNo != 2 || snapshot.RecentTurns[0].SceneTitle != "雨巷" {
		t.Fatalf("snapshot after scene break = %#v", snapshot)
	}
	summaries, err := store.ListSimulations(ctx, testUserID)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].LastSceneNo != 2 {
		t.Fatalf("summary after scene break = %#v", summaries)
	}
}

// achievingProvider returns a catalogued world achievement plus an emergent one,
// with empty choices (the world cannot be intervened this turn).
type achievingProvider struct{}

func (achievingProvider) Name() string  { return "fake" }
func (achievingProvider) Model() string { return "deterministic-story-v1" }

func (achievingProvider) GenerateTurnPlan(_ context.Context, continuity storyengine.ContinuityContext, _, _ string) (storyengine.TurnProposal, map[string]any, error) {
	sim := continuity.Simulation
	return storyengine.TurnProposal{
		InputVersion: sim.Version, ExpectedNextSeq: sim.LastSeq + 1,
		Events: []storyengine.StoryEvent{{Type: "action_taken", SubjectRef: "player", Payload: map[string]any{"action": "继续"}}},
		Achievements: []storyengine.AchievementProposal{
			{Key: "answer-bell"},
			{Key: "emergent-1", Title: "雾里的名字", Description: "你听见雾里有人叫出你的名字。"},
		},
	}, map[string]any{}, nil
}

func (achievingProvider) GenerateNarrativeDraft(_ context.Context, _ storyengine.ContinuityContext, _, _ string, _ storyengine.TurnProposal) (NarrativeDraft, map[string]any, error) {
	return NarrativeDraft{Narrative: achievementNarrative, Choices: []string{}}, map[string]any{}, nil
}

func TestMySQLContinueAndAchievements(t *testing.T) {
	store, db := integrationStore(t)
	ctx := context.Background()
	result, err := db.ExecContext(ctx,
		`UPDATE simulations SET world_snapshot_json=JSON_OBJECT('world',JSON_OBJECT('id',?),'setting',JSON_OBJECT('achievements',JSON_ARRAY(JSON_OBJECT('key','answer-bell','title','只有钟声记得','hint','没有回答第二声钟','reward',6))),'title','雾港来信') WHERE id=UUID_TO_BIN(?,1)`,
		testWorldID, testSimulationID,
	)
	if err != nil {
		t.Fatalf("seed achievement catalogue: %v", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		t.Fatalf("seed achievement catalogue affected %d rows", affected)
	}
	created, _, err := store.Create(ctx, CreateInput{UserID: testUserID, SimulationID: testSimulationID, IdempotencyKey: "continue-request-0001", Interaction: InteractionContinue, Credits: MaxTurnCredits})
	if err != nil {
		t.Fatalf("Create(continue) error = %v", err)
	}
	turn, err := NewProcessor(store, achievingProvider{}, achievements.NewStore(db)).Process(ctx, created.ID, "integration-worker")
	if err != nil {
		t.Fatalf("Process(continue) error = %v", err)
	}
	if turn.Interaction != InteractionContinue || turn.UserAction != "" || len(turn.Proposal.Choices) != 0 {
		t.Fatalf("continue turn = %#v", turn)
	}
	if len(turn.Achievements) != 3 {
		t.Fatalf("achievements = %#v, want 3 awards", turn.Achievements)
	}
	// 初见世界(3) + 只有钟声记得(6) + emergent(0)；花费按正文长度计
	assertWallet(t, db, int64(39-TurnCost(len([]rune(achievementNarrative)))), 0)

	// Re-evaluating the same proposal must not double-pay.
	sim, err := store.LoadSimulation(ctx, Job{UserID: testUserID, SimulationID: testSimulationID})
	if err != nil {
		t.Fatal(err)
	}
	again, err := achievements.NewStore(db).Evaluate(ctx, testUserID, testSimulationID, sim.WorldSnapshot, turn.Proposal, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("re-evaluation returned %#v, want empty", again)
	}
	assertWallet(t, db, int64(39-TurnCost(len([]rune(achievementNarrative)))), 0)

	claims, err := achievements.NewStore(db).ListClaims(ctx, testUserID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 3 {
		t.Fatalf("claims = %#v", claims)
	}
}

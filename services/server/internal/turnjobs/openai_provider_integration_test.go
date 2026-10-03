package turnjobs

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/weike-zhang/ai-novel-studio/services/server/internal/achievements"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/storyengine"
)

// TestOpenAICompatibleProviderProcessesSyntheticTurn runs the full worker
// pipeline against the development 5.6luna endpoint. It only ever uses the
// synthetic fixture account and world; without TEST_MODEL_* it is skipped.
func TestOpenAICompatibleProviderProcessesSyntheticTurn(t *testing.T) {
	baseURL := os.Getenv("TEST_MODEL_BASE_URL")
	apiKey := os.Getenv("TEST_MODEL_API_KEY")
	if baseURL == "" || apiKey == "" {
		t.Skip("TEST_MODEL_BASE_URL or TEST_MODEL_API_KEY is not set")
	}
	model := os.Getenv("TEST_MODEL_NAME")
	if model == "" {
		model = "gpt-5-6-instant"
	}
	provider, err := NewOpenAICompatibleProvider("test-openai-compatible", baseURL, apiKey, model, 120*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	store, db := integrationStore(t)
	ctx := context.Background()
	created, fresh, err := store.Create(ctx, CreateInput{UserID: testUserID, SimulationID: testSimulationID, IdempotencyKey: "turn-real-model-0001", Action: "沿着潮湿的石阶走向码头，寻找那封没有署名的信", Credits: 3})
	if err != nil || !fresh {
		t.Fatalf("Create() = %#v fresh=%v err=%v", created, fresh, err)
	}
	processor := NewProcessor(store, provider, achievements.NewStore(db))
	turn, err := processor.Process(ctx, created.ID, "synthetic-worker")
	if err != nil {
		var validation storyengine.ValidationError
		if errors.As(err, &validation) {
			t.Fatalf("Process() validation issues = %#v", validation.Issues)
		}
		t.Fatalf("Process() with real model error = %v", err)
	}
	if turn.Seq != 1 || turn.Proposal.Narrative == "" || len(turn.Proposal.Choices) < 2 {
		t.Fatalf("committed turn = %#v", turn)
	}
	completed, err := store.Get(ctx, testUserID, created.ID)
	if err != nil || completed.Status != StatusCompleted || completed.CommittedTurn == nil {
		t.Fatalf("completed job = %#v err = %v", completed, err)
	}
	assertWallet(t, db, 7, 0)
}

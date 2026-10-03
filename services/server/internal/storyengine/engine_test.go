package storyengine

import (
	"context"
	"testing"
)

func TestEngineCommitsValidatedProposal(t *testing.T) {
	sim := baseSimulation()
	repo := NewMemoryRepository(sim)
	engine := New(repo, FakeProvider{})
	turn, err := engine.GenerateTurn(context.Background(), TurnInput{SimulationID: sim.ID, Action: "沿着潮湿的石阶向下走"})
	if err != nil {
		t.Fatalf("GenerateTurn() error = %v", err)
	}
	if turn.Seq != 3 {
		t.Fatalf("turn.Seq = %d, want 3", turn.Seq)
	}
	stored, err := repo.GetSimulation(context.Background(), sim.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LastSeq != 3 || stored.Version != 5 {
		t.Fatalf("stored state = seq %d version %d", stored.LastSeq, stored.Version)
	}
}

func TestEngineRejectsBlankActionWithoutCallingProvider(t *testing.T) {
	repo := NewMemoryRepository(baseSimulation())
	engine := New(repo, FakeProvider{})
	if _, err := engine.GenerateTurn(context.Background(), TurnInput{SimulationID: "sim-demo", Action: "  "}); err == nil {
		t.Fatal("expected blank action error")
	}
}

func TestEngineTracksSceneBreaks(t *testing.T) {
	sim := baseSimulation()
	repo := NewMemoryRepository(sim)

	opening := TurnProposal{InputVersion: sim.Version, ExpectedNextSeq: sim.LastSeq + 1, Narrative: "天亮前你离开旧港。", Choices: []string{"继续", "离开"}, Events: []StoryEvent{{Type: "scene_changed"}}, Scene: SceneProposal{StartsNewScene: true, Title: "雨巷", Closure: "你离开旧港，一夜之后抵达落雨的窄巷。"}}
	turn, err := repo.CommitTurn(context.Background(), sim, "天亮后前往雨巷", opening)
	if err != nil {
		t.Fatalf("CommitTurn(scene break) error = %v", err)
	}
	if turn.SceneNo != 2 || turn.SceneTitle != "雨巷" || turn.SceneClosure == "" {
		t.Fatalf("scene break turn = %#v", turn)
	}
	stored, err := repo.GetSimulation(context.Background(), sim.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LastSceneNo != 2 {
		t.Fatalf("LastSceneNo = %d, want 2", stored.LastSceneNo)
	}

	continuation := TurnProposal{InputVersion: stored.Version, ExpectedNextSeq: stored.LastSeq + 1, Narrative: "你在雨巷里继续走。", Choices: []string{"敲门", "继续走"}, Events: []StoryEvent{{Type: "state_changed"}}}
	next, err := repo.CommitTurn(context.Background(), stored, "沿着雨巷继续走", continuation)
	if err != nil {
		t.Fatalf("CommitTurn(continuation) error = %v", err)
	}
	if next.SceneNo != 2 || next.SceneTitle != "" || next.SceneClosure != "" {
		t.Fatalf("continuation turn = %#v", next)
	}
	final, err := repo.GetSimulation(context.Background(), sim.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.LastSceneNo != 2 {
		t.Fatalf("LastSceneNo after continuation = %d, want 2", final.LastSceneNo)
	}
}

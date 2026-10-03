package storyengine

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestBuildContinuityContextUsesOnlyFrozenCommittedSources(t *testing.T) {
	sim := baseSimulation()
	turns := []Turn{
		{ID: "turn-1", SimulationID: sim.ID, Seq: 1, Status: "committed", UserAction: "进入旧港", Proposal: TurnProposal{Narrative: "潮水退去。"}},
		{ID: "turn-2", SimulationID: sim.ID, Seq: 2, Status: "committed", UserAction: "寻找林岚", Proposal: TurnProposal{Narrative: "钟声响起。"}},
	}
	context, err := BuildContinuityContext(ContinuitySources{
		WorldSnapshot:       sim.WorldSnapshot,
		CommittedSimulation: sim,
		CommittedSummary:    sim.Summary,
		CommittedTurns:      turns,
		RecentTurnLimit:     1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if context.Summary != sim.Summary || context.Simulation.State["weather"] != "fog" {
		t.Fatalf("context omitted committed state: %#v", context)
	}
	if len(context.RecentTurns) != 1 || context.RecentTurns[0].Seq != 2 {
		t.Fatalf("recent turns = %#v, want committed tail", context.RecentTurns)
	}

	// The context owns copies: mutating loader buffers after construction cannot
	// rewrite the prompt boundary seen by the model.
	sim.WorldSnapshot[0] = '['
	sim.State["weather"] = "storm"
	turns[1].Proposal.Narrative = "被篡改"
	if !bytes.Equal(context.WorldSnapshot, json.RawMessage(`{"version":7,"rules":["潮汐每天两次"]}`)) {
		t.Fatalf("snapshot alias leaked: %s", context.WorldSnapshot)
	}
	if context.Simulation.State["weather"] != "fog" || context.RecentTurns[0].Proposal.Narrative != "钟声响起。" {
		t.Fatal("committed context changed through source aliases")
	}
}

func TestBuildContinuityContextRejectsUncommittedTurn(t *testing.T) {
	sim := baseSimulation()
	_, err := BuildContinuityContext(ContinuitySources{
		WorldSnapshot:       sim.WorldSnapshot,
		CommittedSimulation: sim,
		CommittedSummary:    sim.Summary,
		CommittedTurns:      []Turn{{ID: "draft", SimulationID: sim.ID, Seq: 2, Status: "draft"}},
	})
	if err == nil {
		t.Fatal("expected uncommitted turn to be rejected")
	}
}

func TestBuildContinuityContextRejectsMutableWorldMismatch(t *testing.T) {
	sim := baseSimulation()
	_, err := BuildContinuityContext(ContinuitySources{
		WorldSnapshot:       json.RawMessage(`{"version":8}`),
		CommittedSimulation: sim,
		CommittedSummary:    sim.Summary,
	})
	if err == nil {
		t.Fatal("expected mismatched world snapshot to be rejected")
	}
}

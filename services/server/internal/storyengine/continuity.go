package storyengine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

const DefaultRecentTurnLimit = 8

// ContinuitySources is the complete allowlist for generation context. Callers
// must load these values from the frozen simulation row and committed turn
// store; transient model memory, mutable world rows, and external memory
// services are deliberately absent.
type ContinuitySources struct {
	WorldSnapshot       json.RawMessage
	CommittedSimulation Simulation
	CommittedSummary    string
	CommittedTurns      []Turn
	RecentTurnLimit     int
}

// ContinuityContext is an immutable-by-copy prompt input. It contains only the
// frozen world snapshot, committed simulation state/summary, and a bounded tail
// of committed turns. BuildContinuityContext validates provenance invariants
// before any model sees the data.
type ContinuityContext struct {
	WorldSnapshot json.RawMessage `json:"worldSnapshot"`
	Simulation    Simulation      `json:"simulation"`
	Summary       string          `json:"summary"`
	RecentTurns   []Turn          `json:"recentTurns"`
}

func BuildContinuityContext(sources ContinuitySources) (ContinuityContext, error) {
	snapshot := bytes.TrimSpace(sources.WorldSnapshot)
	if len(snapshot) == 0 || !json.Valid(snapshot) {
		return ContinuityContext{}, fmt.Errorf("world snapshot must be frozen valid JSON")
	}
	sim := cloneSimulation(sources.CommittedSimulation)
	if sim.ID == "" || sim.Version < 1 || sim.LastSeq < 0 {
		return ContinuityContext{}, fmt.Errorf("committed simulation boundary is invalid")
	}
	if len(sim.WorldSnapshot) > 0 && !bytes.Equal(bytes.TrimSpace(sim.WorldSnapshot), snapshot) {
		return ContinuityContext{}, fmt.Errorf("simulation world snapshot differs from frozen source")
	}
	sim.WorldSnapshot = append(json.RawMessage(nil), snapshot...)
	sim.Summary = sources.CommittedSummary

	turns := make([]Turn, len(sources.CommittedTurns))
	for i, turn := range sources.CommittedTurns {
		if turn.Status != "committed" {
			return ContinuityContext{}, fmt.Errorf("turn %q is not committed", turn.ID)
		}
		if turn.SimulationID != sim.ID || turn.Seq < 1 || turn.Seq > sim.LastSeq {
			return ContinuityContext{}, fmt.Errorf("turn %q is outside committed simulation boundary", turn.ID)
		}
		turns[i] = cloneTurn(turn)
	}
	sort.Slice(turns, func(i, j int) bool { return turns[i].Seq < turns[j].Seq })
	for i := 1; i < len(turns); i++ {
		if turns[i-1].Seq == turns[i].Seq {
			return ContinuityContext{}, fmt.Errorf("duplicate committed turn sequence %d", turns[i].Seq)
		}
	}
	limit := sources.RecentTurnLimit
	if limit <= 0 {
		limit = DefaultRecentTurnLimit
	}
	if len(turns) > limit {
		turns = turns[len(turns)-limit:]
	}
	return ContinuityContext{
		WorldSnapshot: append(json.RawMessage(nil), snapshot...),
		Simulation:    sim,
		Summary:       sources.CommittedSummary,
		RecentTurns:   turns,
	}, nil
}

func cloneTurn(turn Turn) Turn {
	copy := turn
	copy.Proposal = cloneProposal(turn.Proposal)
	return copy
}

func cloneProposal(proposal TurnProposal) TurnProposal {
	copy := proposal
	copy.Choices = append([]string(nil), proposal.Choices...)
	copy.Events = append([]StoryEvent(nil), proposal.Events...)
	for i := range copy.Events {
		copy.Events[i].Payload = cloneMap(proposal.Events[i].Payload)
	}
	copy.CharacterPatches = append([]CharacterPatch(nil), proposal.CharacterPatches...)
	for i := range copy.CharacterPatches {
		copy.CharacterPatches[i].ExpectedCurrent = cloneMap(proposal.CharacterPatches[i].ExpectedCurrent)
		copy.CharacterPatches[i].Current = cloneMap(proposal.CharacterPatches[i].Current)
		copy.CharacterPatches[i].Identity = cloneMap(proposal.CharacterPatches[i].Identity)
	}
	copy.ThreadPatches = append([]ThreadPatch(nil), proposal.ThreadPatches...)
	return copy
}

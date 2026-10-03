// Demo exercises the real story engine with deterministic proposals and memory storage.
// It does not call a model or load environment variables.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/weike-zhang/ai-novel-studio/services/server/internal/storyengine"
)

type identityMutationProvider struct{}

func (identityMutationProvider) GenerateTurnProposal(ctx context.Context, continuity storyengine.ContinuityContext, action string) (storyengine.TurnProposal, error) {
	proposal, err := (storyengine.FakeProvider{}).GenerateTurnProposal(ctx, continuity, action)
	if err != nil {
		return proposal, err
	}
	proposal.CharacterPatches = []storyengine.CharacterPatch{{
		CharacterID: "keeper", ExpectedVersion: 1,
		Current: map[string]any{"name": "Someone else"},
	}}
	return proposal, nil
}

func run() error {
	ctx := context.Background()
	sim := storyengine.Simulation{
		ID: "demo", WorldID: "harbor", Version: 1, LastSceneNo: 1,
		WorldSnapshot: json.RawMessage(`{"rules":["The harbor clock rings eleven times"]}`),
		Characters: map[string]storyengine.CharacterState{
			"keeper": {ID: "keeper", Name: "Keeper", Version: 1,
				Identity: map[string]any{"name": "Keeper"}, Current: map[string]any{"location": "harbor"}},
		},
		Threads: map[string]storyengine.StoryThread{},
	}
	repo := storyengine.NewMemoryRepository(sim)
	turn, err := storyengine.New(repo, storyengine.FakeProvider{}).GenerateTurn(ctx, storyengine.TurnInput{
		SimulationID: sim.ID, Action: "沿着石阶走向港口",
	})
	if err != nil {
		return err
	}
	fmt.Printf("COMMITTED turn=%d status=%s\n%s\n", turn.Seq, turn.Status, turn.Proposal.Narrative)
	_, err = storyengine.New(repo, identityMutationProvider{}).GenerateTurn(ctx, storyengine.TurnInput{
		SimulationID: sim.ID, Action: "观察守钟人",
	})
	var validation storyengine.ValidationError
	if !errors.As(err, &validation) {
		return fmt.Errorf("expected validation rejection, got %v", err)
	}
	identityRejected := false
	for _, issue := range validation.Issues {
		fmt.Printf("REJECTED code=%s field=%s\n", issue.Code, issue.Field)
		if issue.Code == "identity_mutation" {
			identityRejected = true
		}
	}
	stored, err := repo.GetSimulation(ctx, sim.ID)
	if err != nil {
		return err
	}
	if !identityRejected || stored.LastSeq != 1 || stored.Version != 2 || stored.Characters["keeper"].Name != "Keeper" {
		return fmt.Errorf("rejected proposal changed committed state")
	}
	fmt.Printf("PRESERVED turn=%d version=%d name=%s\n", stored.LastSeq, stored.Version, stored.Characters["keeper"].Name)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

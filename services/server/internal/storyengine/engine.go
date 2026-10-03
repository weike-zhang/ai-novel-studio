package storyengine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var ErrSimulationNotFound = errors.New("simulation not found")

type ModelProvider interface {
	GenerateTurnProposal(ctx context.Context, continuity ContinuityContext, action string) (TurnProposal, error)
}

type Repository interface {
	GetSimulation(ctx context.Context, id string) (Simulation, error)
	CommitTurn(ctx context.Context, sim Simulation, action string, proposal TurnProposal) (Turn, error)
	ListTurns(ctx context.Context, simulationID string) ([]Turn, error)
}

type Engine struct {
	repository Repository
	provider   ModelProvider
}

func New(repository Repository, provider ModelProvider) *Engine {
	return &Engine{repository: repository, provider: provider}
}

func (e *Engine) GenerateTurn(ctx context.Context, input TurnInput) (Turn, error) {
	if err := ValidateAction(input.Action); err != nil {
		return Turn{}, err
	}
	sim, err := e.repository.GetSimulation(ctx, input.SimulationID)
	if err != nil {
		return Turn{}, err
	}
	turns, err := e.repository.ListTurns(ctx, input.SimulationID)
	if err != nil {
		return Turn{}, fmt.Errorf("load committed turns: %w", err)
	}
	continuity, err := BuildContinuityContext(ContinuitySources{
		WorldSnapshot:       sim.WorldSnapshot,
		CommittedSimulation: sim,
		CommittedSummary:    sim.Summary,
		CommittedTurns:      turns,
	})
	if err != nil {
		return Turn{}, fmt.Errorf("build continuity context: %w", err)
	}
	proposal, err := e.provider.GenerateTurnProposal(ctx, continuity, strings.TrimSpace(input.Action))
	if err != nil {
		return Turn{}, fmt.Errorf("generate turn proposal: %w", err)
	}
	if err := ValidateProposalAgainstContext(continuity, proposal); err != nil {
		return Turn{}, err
	}
	return e.repository.CommitTurn(ctx, sim, strings.TrimSpace(input.Action), proposal)
}

type MemoryRepository struct {
	mu          sync.Mutex
	simulations map[string]Simulation
	turns       map[string][]Turn
}

func NewMemoryRepository(simulations ...Simulation) *MemoryRepository {
	repo := &MemoryRepository{simulations: make(map[string]Simulation), turns: make(map[string][]Turn)}
	for _, sim := range simulations {
		repo.simulations[sim.ID] = cloneSimulation(sim)
	}
	return repo
}

func (r *MemoryRepository) GetSimulation(_ context.Context, id string) (Simulation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sim, ok := r.simulations[id]
	if !ok {
		return Simulation{}, ErrSimulationNotFound
	}
	return cloneSimulation(sim), nil
}

func (r *MemoryRepository) CommitTurn(_ context.Context, expected Simulation, action string, proposal TurnProposal) (Turn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.simulations[expected.ID]
	if !ok {
		return Turn{}, ErrSimulationNotFound
	}
	if current.Version != expected.Version || current.LastSeq != expected.LastSeq {
		return Turn{}, ValidationError{Issues: []ValidationIssue{{Code: "concurrent_turn", Field: "simulation", Message: "已有另一幕完成，请从最新界标继续"}}}
	}
	sceneNo := current.LastSceneNo
	sceneTitle := ""
	sceneClosure := ""
	if proposal.Scene.StartsNewScene {
		sceneNo = current.LastSceneNo + 1
		sceneTitle = strings.TrimSpace(proposal.Scene.Title)
		sceneClosure = strings.TrimSpace(proposal.Scene.Closure)
	}
	for _, patch := range proposal.CharacterPatches {
		character := current.Characters[patch.CharacterID]
		for key, value := range patch.Current {
			character.Current[key] = value
		}
		character.Version++
		current.Characters[patch.CharacterID] = character
	}
	for _, patch := range proposal.ThreadPatches {
		if patch.Action == "open" {
			current.Threads[patch.Key] = StoryThread{Key: patch.Key, Title: patch.Title, Status: "open", Priority: patch.Priority}
		} else {
			thread := current.Threads[patch.Key]
			thread.Status = "resolved"
			current.Threads[patch.Key] = thread
		}
	}
	current.Version++
	current.LastSeq++
	current.LastSceneNo = sceneNo
	r.simulations[current.ID] = cloneSimulation(current)
	turn := Turn{ID: fmt.Sprintf("turn-%d", current.LastSeq), SimulationID: current.ID, Seq: current.LastSeq, SceneNo: sceneNo, SceneTitle: sceneTitle, SceneClosure: sceneClosure, UserAction: action, Proposal: proposal, Status: "committed", CreatedAt: time.Now().UTC()}
	r.turns[current.ID] = append(r.turns[current.ID], turn)
	return turn, nil
}

func (r *MemoryRepository) ListTurns(_ context.Context, simulationID string) ([]Turn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.simulations[simulationID]; !ok {
		return nil, ErrSimulationNotFound
	}
	return append([]Turn(nil), r.turns[simulationID]...), nil
}

func cloneSimulation(sim Simulation) Simulation {
	copy := sim
	copy.WorldSnapshot = append([]byte(nil), sim.WorldSnapshot...)
	copy.State = cloneMap(sim.State)
	copy.Characters = make(map[string]CharacterState, len(sim.Characters))
	for id, character := range sim.Characters {
		character.Identity = cloneMap(character.Identity)
		character.Current = cloneMap(character.Current)
		copy.Characters[id] = character
	}
	copy.Threads = make(map[string]StoryThread, len(sim.Threads))
	for key, thread := range sim.Threads {
		copy.Threads[key] = thread
	}
	return copy
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	target := make(map[string]any, len(source))
	for key, value := range source {
		target[key] = value
	}
	return target
}

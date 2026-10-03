package turnjobs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/weike-zhang/ai-novel-studio/services/server/internal/achievements"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/storyengine"
)

type NarrativeDraft struct {
	Narrative string
	Choices   []string
}

type Provider interface {
	Name() string
	Model() string
	GenerateTurnPlan(context.Context, storyengine.ContinuityContext, string, string) (storyengine.TurnProposal, map[string]any, error)
	GenerateNarrativeDraft(context.Context, storyengine.ContinuityContext, string, string, storyengine.TurnProposal) (NarrativeDraft, map[string]any, error)
}

type Processor struct {
	store        *Store
	provider     Provider
	achievements *achievements.Store
}

func NewProcessor(store *Store, provider Provider, achievementStore *achievements.Store) *Processor {
	return &Processor{store: store, provider: provider, achievements: achievementStore}
}

func (p *Processor) Process(ctx context.Context, jobID, workerID string) (storyengine.Turn, error) {
	job, err := p.store.GetForProcessing(ctx, jobID)
	if err != nil {
		return storyengine.Turn{}, err
	}
	if job.Status != StatusQueued {
		return storyengine.Turn{}, ErrConcurrentState
	}
	attempt, err := p.store.BeginAttempt(ctx, job, p.provider.Name(), p.provider.Model(), workerID)
	if err != nil {
		return storyengine.Turn{}, err
	}
	continuity, err := p.store.LoadContinuity(ctx, job)
	if err != nil {
		_ = p.store.Fail(ctx, job.ID, attempt.ID, "context_load_failed")
		return storyengine.Turn{}, err
	}
	plan, planUsage, err := p.provider.GenerateTurnPlan(ctx, continuity, job.Action, job.Interaction)
	if err != nil {
		_ = p.store.Fail(ctx, job.ID, attempt.ID, "planning_failed")
		return storyengine.Turn{}, fmt.Errorf("generate turn plan: %w", err)
	}
	authorization, err := storyengine.AuthorizePlan(continuity, plan)
	if err != nil {
		_ = p.store.Fail(ctx, job.ID, attempt.ID, validationFailureCode("plan_rejected", err))
		return storyengine.Turn{}, err
	}
	if err := p.store.MarkDrafting(ctx, job.ID); err != nil {
		if !errors.Is(err, ErrConcurrentState) {
			_ = p.store.Fail(ctx, job.ID, attempt.ID, "draft_transition_failed")
		}
		return storyengine.Turn{}, err
	}
	draft, draftUsage, err := p.provider.GenerateNarrativeDraft(ctx, continuity, job.Action, job.Interaction, plan)
	if err != nil {
		_ = p.store.Fail(ctx, job.ID, attempt.ID, "drafting_failed")
		return storyengine.Turn{}, fmt.Errorf("generate narrative draft: %w", err)
	}
	plan.Narrative = draft.Narrative
	plan.Choices = draft.Choices
	if err := p.store.MarkValidating(ctx, job.ID); err != nil {
		if !errors.Is(err, ErrConcurrentState) {
			_ = p.store.Fail(ctx, job.ID, attempt.ID, "validation_transition_failed")
		}
		return storyengine.Turn{}, err
	}
	if err := storyengine.ValidateProposalWithAuthorization(continuity, plan, authorization); err != nil {
		_ = p.store.Fail(ctx, job.ID, attempt.ID, validationFailureCode("proposal_rejected", err))
		return storyengine.Turn{}, err
	}
	turn, err := p.store.CommitSuccess(ctx, CommitInput{Job: job, Attempt: attempt, Proposal: plan, Usage: map[string]any{"planning": planUsage, "drafting": draftUsage}})
	if err != nil {
		return storyengine.Turn{}, err
	}
	turn.Achievements, err = p.evaluateAchievements(ctx, job, continuity, plan)
	if err != nil {
		// Achievements are best-effort and must never fail the committed turn.
		log.Printf("evaluate achievements for turn %s: %v", turn.ID, err)
		turn.Achievements = nil
	}
	return turn, nil
}

func (p *Processor) evaluateAchievements(ctx context.Context, job Job, continuity storyengine.ContinuityContext, plan storyengine.TurnProposal) ([]storyengine.AchievementAward, error) {
	if p.achievements == nil || len(plan.Achievements) == 0 && !hasGlobalAchievementProgress(job.Interaction, plan) {
		return nil, nil
	}
	claims, err := p.achievements.Evaluate(ctx, job.UserID, job.SimulationID, continuity.WorldSnapshot, plan, time.Now())
	if err != nil {
		return nil, err
	}
	awards := make([]storyengine.AchievementAward, 0, len(claims))
	for _, claim := range claims {
		awards = append(awards, storyengine.AchievementAward{
			Scope: claim.Scope, WorldID: claim.WorldID, Code: claim.Code,
			Title: claim.Title, Description: claim.Description, Reward: claim.Reward,
		})
	}
	return awards, nil
}

// hasGlobalAchievementProgress reports whether this turn can possibly advance a
// global milestone, avoiding an evaluation round-trip for no-ops.
func hasGlobalAchievementProgress(interaction string, plan storyengine.TurnProposal) bool {
	return len(plan.Achievements) > 0 ||
		interaction == InteractionContinue ||
		interaction == InteractionAct ||
		len(plan.Choices) == 0
}

func validationFailureCode(prefix string, err error) string {
	var validation storyengine.ValidationError
	if errors.As(err, &validation) && len(validation.Issues) > 0 {
		code := strings.TrimSpace(validation.Issues[0].Code)
		if code != "" {
			return prefix + "_" + code
		}
	}
	return prefix
}

type FakeProvider struct{}

func (FakeProvider) Name() string  { return "fake" }
func (FakeProvider) Model() string { return "deterministic-story-v1" }

func (FakeProvider) GenerateTurnPlan(_ context.Context, continuity storyengine.ContinuityContext, action, _ string) (storyengine.TurnProposal, map[string]any, error) {
	sim := continuity.Simulation
	return storyengine.TurnProposal{
		InputVersion: sim.Version, ExpectedNextSeq: sim.LastSeq + 1,
		Events: []storyengine.StoryEvent{{Type: "action_taken", SubjectRef: "player", Payload: map[string]any{"action": action}}},
	}, map[string]any{"inputTokens": 48, "outputTokens": 32}, nil
}

// fakeTurnNarrative is a fixed-length narrative so the dynamic per-character
// credit cost stays deterministic in tests and demo runs.
const fakeTurnNarrative = "雾从旧港的石阶间退去，一封没有邮戳的信停在脚边。林岚看见信封上的字，立刻收起铜匙。那是你的笔迹，但日期写在十年前。远处的钟声少了一响。信封正在变热，下一声钟即将响起。你要拆开它，还是先追上雾里的送信人？"

func (FakeProvider) GenerateNarrativeDraft(_ context.Context, _ storyengine.ContinuityContext, _, _ string, _ storyengine.TurnProposal) (NarrativeDraft, map[string]any, error) {
	return NarrativeDraft{
		Narrative: fakeTurnNarrative,
		Choices:   []string{"拆开信封", "追上远处的送信人", "先观察周围的动静"},
	}, map[string]any{"inputTokens": 96, "outputTokens": 118}, nil
}

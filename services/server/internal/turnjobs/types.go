package turnjobs

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/weike-zhang/ai-novel-studio/services/server/internal/storyengine"
)

type Status string

const (
	StatusQueued     Status = "queued"
	StatusPlanning   Status = "planning"
	StatusDrafting   Status = "drafting"
	StatusValidating Status = "validating"
	StatusCommitting Status = "committing"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
	StatusCancelled  Status = "cancelled"
)

var (
	ErrNotFound            = errors.New("turn job not found")
	ErrForbidden           = errors.New("turn job forbidden")
	ErrInsufficientCredits = errors.New("insufficient credits")
	ErrActiveJobExists     = errors.New("an active turn job already exists")
	ErrIdempotencyConflict = errors.New("idempotency key reused with different input")
	ErrCannotCancel        = errors.New("turn job has crossed the commit boundary")
	ErrCannotRetry         = errors.New("turn job cannot be retried")
	ErrConcurrentState     = errors.New("simulation state changed concurrently")
)

const (
	// InteractionAct is a free-text action typed by the reader.
	InteractionAct = "act"
	// InteractionBranch is picking one of the AI-suggested forks.
	InteractionBranch = "branch"
	// InteractionContinue advances along the obvious path without a new decision.
	InteractionContinue = "continue"
)

// Dynamic turn cost: ~1 credit per 100 narrative characters, floored, with a
// floor of 1 and a per-turn cap. The cap matches the reserved amount so a
// committed turn always settles within its reservation and any over-reserve is
// refunded.
const (
	creditsPer100Chars = 100
	MinTurnCredits     = 1
	MaxTurnCredits     = 12
)

// TurnCost returns the credit cost of a committed narrative (floor of runes/100).
func TurnCost(runes int) int {
	cost := runes / creditsPer100Chars
	if cost < MinTurnCredits {
		cost = MinTurnCredits
	}
	if cost > MaxTurnCredits {
		cost = MaxTurnCredits
	}
	return cost
}

// ValidInteraction reports whether an interaction label is accepted.
func ValidInteraction(value string) bool {
	return value == InteractionAct || value == InteractionBranch || value == InteractionContinue
}

type Job struct {
	ID                string            `json:"id"`
	UserID            string            `json:"userId"`
	SimulationID      string            `json:"simulationId"`
	IdempotencyKey    string            `json:"idempotencyKey"`
	Action            string            `json:"action"`
	Interaction       string            `json:"interaction"`
	Status            Status            `json:"status"`
	ReservedCredits   int64             `json:"reservedCredits"`
	InputVersion      int64             `json:"inputVersion"`
	LeaseOwner        string            `json:"-"`
	LeaseExpiresAt    *time.Time        `json:"-"`
	CancelRequestedAt *time.Time        `json:"cancelRequestedAt,omitempty"`
	ErrorCode         string            `json:"errorCode,omitempty"`
	AttemptNo         int               `json:"attemptNo"`
	CommittedTurn     *storyengine.Turn `json:"committedTurn,omitempty"`
	CreatedAt         time.Time         `json:"createdAt"`
	UpdatedAt         time.Time         `json:"updatedAt"`
}

type CreateInput struct {
	UserID         string
	SimulationID   string
	IdempotencyKey string
	Action         string
	Interaction    string
	Credits        int64
}

type Attempt struct {
	ID        string
	JobID     string
	Number    int
	Provider  string
	Model     string
	Status    string
	StartedAt time.Time
}

type CommitInput struct {
	Job      Job
	Attempt  Attempt
	Proposal storyengine.TurnProposal
	Usage    map[string]any
}

type SimulationSnapshot struct {
	ID            string                                `json:"id"`
	WorldID       string                                `json:"worldId"`
	WorldSnapshot json.RawMessage                       `json:"worldSnapshot"`
	State         map[string]any                        `json:"state"`
	Summary       string                                `json:"summary"`
	Version       int64                                 `json:"version"`
	LastTurnSeq   int                                   `json:"lastTurnSeq"`
	LastSceneNo   int                                   `json:"lastSceneNo"`
	Characters    map[string]storyengine.CharacterState `json:"characters"`
	Threads       map[string]storyengine.StoryThread    `json:"threads"`
	RecentTurns   []storyengine.Turn                    `json:"recentTurns"`
}

package worlddrafts

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Status string

const (
	StatusQueued     Status = "queued"
	StatusGenerating Status = "generating"
	StatusValidating Status = "validating"
	StatusCommitting Status = "committing"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
	StatusCancelled  Status = "cancelled"
)

var (
	ErrInvalidInput        = errors.New("invalid world draft input")
	ErrNotFound            = errors.New("world draft job not found")
	ErrForbidden           = errors.New("world draft job forbidden")
	ErrIdempotencyConflict = errors.New("idempotency key reused with different input")
	ErrConcurrentState     = errors.New("world draft job state changed concurrently")
	ErrAttemptsExhausted   = errors.New("world draft job attempts exhausted")
)

type GenerateInput struct {
	Topic         string `json:"topic"`
	Type          string `json:"type"`
	ContentRating string `json:"contentRating"`
	Boundaries    string `json:"boundaries"`
}
type WorldBackground struct {
	EraPlace string `json:"eraPlace"`
	Rules    string `json:"rules"`
	Conflict string `json:"conflict"`
}
type PlayerIdentity struct {
	Name       string `json:"name"`
	Background string `json:"background"`
	Goal       string `json:"goal"`
	Freedom    string `json:"freedom"`
}
type Character struct {
	Name        string `json:"name"`
	Role        string `json:"role"`
	Personality string `json:"personality"`
	Goal        string `json:"goal"`
	Relation    string `json:"relation"`
	Public      bool   `json:"public"`
}
type Narrative struct {
	Style       string `json:"style"`
	PointOfView string `json:"pointOfView"`
	TurnLength  string `json:"turnLength"`
	Openness    string `json:"openness"`
}
type Opening struct {
	Text    string   `json:"text"`
	Actions []string `json:"actions"`
}
type Discovery struct {
	Hook         string `json:"hook"`
	Promise      string `json:"promise"`
	PlayTime     string `json:"playTime"`
	Format       string `json:"format"`
	FeaturedRank int    `json:"featuredRank,omitempty"`
}

type GeneratedDraft struct {
	Title             string          `json:"title"`
	Summary           string          `json:"summary"`
	Type              string          `json:"type"`
	Tags              []string        `json:"tags"`
	Cover             string          `json:"cover"`
	WorldBackground   WorldBackground `json:"worldBackground"`
	PlayerIdentity    PlayerIdentity  `json:"playerIdentity"`
	Characters        []Character     `json:"characters"`
	Narrative         Narrative       `json:"narrative"`
	Opening           Opening         `json:"opening"`
	Discovery         Discovery       `json:"discovery"`
	ContentRating     string          `json:"contentRating"`
	ContentBoundaries string          `json:"contentBoundaries"`
}

type Usage struct {
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
	TotalTokens  int64 `json:"totalTokens"`
}
type Attempt struct {
	ID        string    `json:"id"`
	JobID     string    `json:"jobId"`
	Number    int       `json:"attemptNo"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"startedAt"`
}

type WorldResult struct {
	ID          string          `json:"id"`
	OwnerID     string          `json:"ownerId"`
	Title       string          `json:"title"`
	Summary     string          `json:"summary"`
	Setting     json.RawMessage `json:"setting"`
	Visibility  string          `json:"visibility"`
	Status      string          `json:"status"`
	Version     int64           `json:"version"`
	DraftSource string          `json:"draftSource"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

type Job struct {
	ID             string        `json:"id"`
	Kind           string        `json:"kind"`
	UserID         string        `json:"userId"`
	IdempotencyKey string        `json:"-"`
	Input          GenerateInput `json:"input"`
	InputHash      [32]byte      `json:"-"`
	Status         Status        `json:"status"`
	AttemptNo      int           `json:"attemptNo"`
	MaxAttempts    int           `json:"maxAttempts"`
	AvailableAt    time.Time     `json:"availableAt"`
	LeaseOwner     string        `json:"-"`
	LeaseExpiresAt *time.Time    `json:"-"`
	WorldID        string        `json:"worldId,omitempty"`
	ErrorCode      string        `json:"errorCode,omitempty"`
	Result         *WorldResult  `json:"result,omitempty"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`
	CanCancel      bool          `json:"canCancel"`
	CanRetry       bool          `json:"canRetry"`
}

func (j Job) withPolicy() Job {
	switch j.Status {
	case StatusCompleted, StatusFailed, StatusCancelled, StatusCommitting:
		j.CanCancel = false
	default:
		j.CanCancel = true
	}
	j.CanRetry = j.Status == StatusFailed
	return j
}

type CreateInput struct {
	UserID         string
	IdempotencyKey string
	GenerateInput  GenerateInput
}
type CommitInput struct {
	Job      Job
	Attempt  Attempt
	WorkerID string
	Draft    GeneratedDraft
	Usage    Usage
}

func NormalizeInput(input GenerateInput) (GenerateInput, error) {
	input.Topic = strings.TrimSpace(input.Topic)
	input.Type = strings.TrimSpace(input.Type)
	input.ContentRating = strings.TrimSpace(input.ContentRating)
	input.Boundaries = strings.TrimSpace(input.Boundaries)
	if input.Topic == "" || input.Type == "" || input.Boundaries == "" || runeLen(input.Topic) > 500 || runeLen(input.Type) > 40 || runeLen(input.Boundaries) > 1000 {
		return GenerateInput{}, ErrInvalidInput
	}
	switch input.ContentRating {
	case "", "全年龄", "13+", "16+", "18+":
	default:
		return GenerateInput{}, ErrInvalidInput
	}
	return input, nil
}

func NormalizeAndHashInput(input GenerateInput) (GenerateInput, [32]byte, error) {
	normalized, err := NormalizeInput(input)
	if err != nil {
		return GenerateInput{}, [32]byte{}, err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return GenerateInput{}, [32]byte{}, fmt.Errorf("encode normalized world draft input: %w", err)
	}
	return normalized, sha256.Sum256(payload), nil
}

func (draft GeneratedDraft) Validate() error {
	setting, err := draft.SettingJSON()
	if err != nil {
		return ErrInvalidInput
	}
	required := []string{draft.Title, draft.Summary, draft.Type, draft.WorldBackground.EraPlace, draft.WorldBackground.Rules, draft.WorldBackground.Conflict, draft.PlayerIdentity.Name, draft.PlayerIdentity.Background, draft.PlayerIdentity.Goal, draft.PlayerIdentity.Freedom, draft.Narrative.Style, draft.Narrative.PointOfView, draft.Narrative.TurnLength, draft.Narrative.Openness, draft.Opening.Text, draft.Discovery.Hook, draft.Discovery.Promise, draft.Discovery.PlayTime, draft.Discovery.Format, draft.ContentRating, draft.ContentBoundaries}
	for _, value := range required {
		if strings.TrimSpace(value) == "" || runeLen(value) > 4000 {
			return ErrInvalidInput
		}
	}
	if runeLen(draft.Title) > 80 || runeLen(draft.Summary) > 500 || runeLen(draft.Type) > 40 || len(setting) > 128<<10 {
		return ErrInvalidInput
	}
	if runeLen(draft.Summary) < 40 || runeLen(draft.Summary) > 120 || runeLen(draft.Discovery.Hook) < 12 || runeLen(draft.Discovery.Hook) > 60 || runeLen(draft.Discovery.Promise) < 16 || runeLen(draft.Discovery.Promise) > 80 || runeLen(draft.Opening.Text) < 120 || runeLen(draft.Opening.Text) > 320 {
		return ErrInvalidInput
	}
	if len(draft.Tags) > 20 || len(draft.Characters) == 0 || len(draft.Characters) > 30 || len(draft.Opening.Actions) != 3 {
		return ErrInvalidInput
	}
	for _, tag := range draft.Tags {
		if strings.TrimSpace(tag) == "" || runeLen(tag) > 40 {
			return ErrInvalidInput
		}
	}
	for _, character := range draft.Characters {
		if strings.TrimSpace(character.Name) == "" || strings.TrimSpace(character.Role) == "" || strings.TrimSpace(character.Personality) == "" || strings.TrimSpace(character.Goal) == "" || strings.TrimSpace(character.Relation) == "" {
			return ErrInvalidInput
		}
	}
	for _, action := range draft.Opening.Actions {
		if strings.TrimSpace(action) == "" || runeLen(action) > 200 {
			return ErrInvalidInput
		}
	}
	return nil
}

func (draft GeneratedDraft) SettingJSON() (json.RawMessage, error) {
	return json.Marshal(map[string]any{"type": draft.Type, "tags": draft.Tags, "cover": draft.Cover, "discovery": draft.Discovery, "worldBackground": draft.WorldBackground, "playerIdentity": draft.PlayerIdentity, "characters": draft.Characters, "narrative": draft.Narrative, "opening": draft.Opening, "contentRating": draft.ContentRating, "contentBoundaries": draft.ContentBoundaries})
}

func runeLen(value string) int { return len([]rune(value)) }

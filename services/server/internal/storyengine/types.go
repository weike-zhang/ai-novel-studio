package storyengine

import (
	"encoding/json"
	"time"
)

type CharacterState struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Identity map[string]any `json:"identity"`
	Current  map[string]any `json:"current"`
	Version  int64          `json:"version"`
}

type StoryThread struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority int    `json:"priority"`
}

type StoryEvent struct {
	Type       string         `json:"type"`
	SubjectRef string         `json:"subjectRef"`
	ObjectRef  string         `json:"objectRef,omitempty"`
	Payload    map[string]any `json:"payload"`
}

type CharacterPatch struct {
	CharacterID     string         `json:"characterId"`
	ExpectedVersion int64          `json:"expectedVersion"`
	ExpectedCurrent map[string]any `json:"expectedCurrent,omitempty"`
	Current         map[string]any `json:"current"`
	// Identity and Name are decode targets for untrusted model output. They are
	// never applied: validators reject either field so immutable character facts
	// cannot be smuggled through a patch.
	Identity map[string]any `json:"identity,omitempty"`
	Name     *string        `json:"name,omitempty"`
}

type ThreadPatch struct {
	Key      string `json:"key"`
	Action   string `json:"action"`
	Title    string `json:"title,omitempty"`
	Priority int    `json:"priority,omitempty"`
}

// SceneProposal declares whether this turn opens a new narrative scene. A scene
// is a maximal consecutive run of turns sharing a scene number; scene 1 is the
// world opening. When StartsNewScene is true the turn carries the new scene's
// title and a one-sentence closure summarizing how the previous scene resolved
// and what happened during the jump. The narrative stage expands the closure
// into prose, so a fast-forward never ends in a few rushed sentences.
type SceneProposal struct {
	StartsNewScene bool   `json:"startsNewScene"`
	Title          string `json:"title"`
	Closure        string `json:"closure"`
}

// AchievementProposal is a world achievement or a model-improvising emergent
// achievement proposed during planning. For catalogued world achievements only
// Key is required (the server resolves title/description/reward from the world
// setting). Emergent achievements carry their own title and description and are
// never rewarded with credits.
type AchievementProposal struct {
	Key         string `json:"key"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

type TurnProposal struct {
	InputVersion     int64                 `json:"inputVersion"`
	ExpectedNextSeq  int                   `json:"expectedNextSeq"`
	Narrative        string                `json:"narrative"`
	Choices          []string              `json:"choices"`
	Events           []StoryEvent          `json:"events"`
	CharacterPatches []CharacterPatch      `json:"characterPatches"`
	ThreadPatches    []ThreadPatch         `json:"threadPatches"`
	Scene            SceneProposal         `json:"scene"`
	Achievements     []AchievementProposal `json:"achievements,omitempty"`
}

type Simulation struct {
	ID            string                    `json:"id"`
	WorldID       string                    `json:"worldId"`
	WorldSnapshot json.RawMessage           `json:"worldSnapshot"`
	State         map[string]any            `json:"state"`
	Summary       string                    `json:"summary"`
	Version       int64                     `json:"version"`
	LastSeq       int                       `json:"lastSeq"`
	LastSceneNo   int                       `json:"lastSceneNo"`
	Characters    map[string]CharacterState `json:"characters"`
	Threads       map[string]StoryThread    `json:"threads"`
}

// AchievementAward is a newly unlocked achievement surfaced to the client on
// the committed turn so the UI can show a notification bar.
type AchievementAward struct {
	Scope       string `json:"scope"`
	WorldID     string `json:"worldId,omitempty"`
	Code        string `json:"code"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Reward      int64  `json:"reward"`
}

type Turn struct {
	ID           string             `json:"id"`
	SimulationID string             `json:"simulationId"`
	Seq          int                `json:"seq"`
	SceneNo      int                `json:"sceneNo"`
	SceneTitle   string             `json:"sceneTitle"`
	SceneClosure string             `json:"sceneClosure"`
	Interaction  string             `json:"interaction"`
	UserAction   string             `json:"userAction"`
	Proposal     TurnProposal       `json:"proposal"`
	Status       string             `json:"status"`
	Achievements []AchievementAward `json:"achievements,omitempty"`
	CreatedAt    time.Time          `json:"createdAt"`
}

type TurnInput struct {
	SimulationID string `json:"simulationId"`
	Action       string `json:"action"`
}

type ValidationIssue struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ValidationError struct {
	Issues []ValidationIssue `json:"issues"`
}

func (e ValidationError) Error() string { return "turn proposal failed validation" }

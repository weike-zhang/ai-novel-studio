package storyengine

import (
	"encoding/json"
	"testing"
)

func baseSimulation() Simulation {
	return Simulation{ID: "sim-demo", WorldID: "world-demo", WorldSnapshot: json.RawMessage(`{"version":7,"rules":["潮汐每天两次"]}`), State: map[string]any{"weather": "fog"}, Summary: "林岚在旧港追查失信。", Version: 4, LastSeq: 2, LastSceneNo: 1, Characters: map[string]CharacterState{
		"lin":  {ID: "lin", Name: "林岚", Identity: map[string]any{"name": "林岚", "role": "守信人"}, Current: map[string]any{"location": "旧港"}, Version: 3},
		"qiao": {ID: "qiao", Name: "乔安", Identity: map[string]any{"name": "乔安", "role": "船医"}, Current: map[string]any{"trust": float64(1)}, Version: 2},
	}, Threads: map[string]StoryThread{"lost-letter": {Key: "lost-letter", Status: "open"}}}
}

func TestValidateProposalRejectsIdentityMutation(t *testing.T) {
	sim := baseSimulation()
	proposal := TurnProposal{InputVersion: 4, ExpectedNextSeq: 3, Narrative: "正文", Choices: []string{"继续", "离开"}, Events: []StoryEvent{{Type: "state_changed"}}, CharacterPatches: []CharacterPatch{{CharacterID: "lin", ExpectedVersion: 3, Current: map[string]any{"name": "另一个名字"}}}}
	if err := ValidateProposal(sim, proposal); err == nil {
		t.Fatal("expected identity mutation to be rejected")
	}
}

func TestValidateProposalRequiresTraceEvent(t *testing.T) {
	sim := baseSimulation()
	proposal := TurnProposal{InputVersion: 4, ExpectedNextSeq: 3, Narrative: "正文", Choices: []string{"继续", "离开"}, CharacterPatches: []CharacterPatch{{CharacterID: "lin", ExpectedVersion: 3, Current: map[string]any{"location": "灯塔"}}}}
	if err := ValidateProposal(sim, proposal); err == nil {
		t.Fatal("expected untraceable state change to be rejected")
	}
}

func TestValidateProposalRequiresVersionOrExpectedPreviousValue(t *testing.T) {
	sim := baseSimulation()
	proposal := validProposal()
	proposal.CharacterPatches = []CharacterPatch{{CharacterID: "lin", Current: map[string]any{"location": "灯塔"}}}
	err := ValidateProposal(sim, proposal)
	assertIssueCode(t, err, "character_precondition_required")

	proposal.CharacterPatches[0].ExpectedCurrent = map[string]any{"location": "旧港"}
	if err := ValidateProposal(sim, proposal); err != nil {
		t.Fatalf("expected previous value should authorize patch: %v", err)
	}
}

func TestValidateProposalRejectsStalePreviousValue(t *testing.T) {
	proposal := validProposal()
	proposal.CharacterPatches = []CharacterPatch{{CharacterID: "lin", ExpectedCurrent: map[string]any{"location": "王城"}, Current: map[string]any{"location": "灯塔"}}}
	assertIssueCode(t, ValidateProposal(baseSimulation(), proposal), "stale_character_value")
}

func TestValidateProposalRejectsExplicitImmutableFields(t *testing.T) {
	proposal := validProposal()
	renamed := "另一个名字"
	proposal.CharacterPatches = []CharacterPatch{{CharacterID: "lin", ExpectedVersion: 3, Current: map[string]any{"location": "灯塔"}, Name: &renamed, Identity: map[string]any{"role": "陌生人"}}}
	assertIssueCode(t, ValidateProposal(baseSimulation(), proposal), "identity_mutation")
}

func TestValidateProposalSceneRules(t *testing.T) {
	sim := baseSimulation()

	// A new scene requires both a title and a closure.
	proposal := validProposal()
	proposal.Scene = SceneProposal{StartsNewScene: true}
	err := ValidateProposal(sim, proposal)
	validation, ok := err.(ValidationError)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	got := map[string]bool{}
	for _, issue := range validation.Issues {
		got[issue.Code] = true
	}
	if !got["scene_title_required"] || !got["scene_closure_required"] {
		t.Fatalf("issues = %#v, want scene_title_required and scene_closure_required", validation.Issues)
	}

	// A complete scene break passes.
	proposal.Scene = SceneProposal{StartsNewScene: true, Title: "雨巷", Closure: "你离开旧港，一夜之后抵达落雨的窄巷。"}
	if err := ValidateProposal(sim, proposal); err != nil {
		t.Fatalf("valid scene break should pass: %v", err)
	}

	// Continuation turns must not carry scene fields.
	proposal.Scene = SceneProposal{StartsNewScene: false, Title: "雨巷", Closure: ""}
	assertIssueCode(t, ValidateProposal(sim, proposal), "scene_fields_on_continuation")

	// Over-length title is rejected.
	proposal.Scene = SceneProposal{StartsNewScene: true, Title: "这是一个用来测试超长标题是否会被校验拒绝的很长很长的场景名字超过四十个字的极限测试场景标题啊", Closure: "收束。"}
	assertIssueCode(t, ValidateProposal(sim, proposal), "scene_title_too_long")
}

func validProposal() TurnProposal {
	return TurnProposal{InputVersion: 4, ExpectedNextSeq: 3, Narrative: "正文", Choices: []string{"继续", "离开"}, Events: []StoryEvent{{Type: "state_changed"}}}
}

func assertIssueCode(t *testing.T, err error, code string) {
	t.Helper()
	validation, ok := err.(ValidationError)
	if !ok {
		t.Fatalf("error = %T %v, want ValidationError with %q", err, err, code)
	}
	for _, issue := range validation.Issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("issues = %#v, want code %q", validation.Issues, code)
}

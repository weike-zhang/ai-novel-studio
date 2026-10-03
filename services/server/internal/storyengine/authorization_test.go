package storyengine

import "testing"

func TestAuthorizedProposalCannotAddOrRewriteCharacterPatch(t *testing.T) {
	context := testContinuityContext(t)
	plan := validProposal()
	plan.Narrative = ""
	plan.Choices = nil
	plan.CharacterPatches = []CharacterPatch{{CharacterID: "lin", ExpectedVersion: 3, Current: map[string]any{"location": "灯塔"}}}
	authorization, err := AuthorizePlan(context, plan)
	if err != nil {
		t.Fatal(err)
	}

	final := plan
	final.Narrative = "林岚走向灯塔。"
	final.Choices = []string{"跟上", "留在原地"}
	if err := ValidateProposalWithAuthorization(context, final, authorization); err != nil {
		t.Fatalf("authorized final proposal rejected: %v", err)
	}

	rewritten := cloneProposal(final)
	rewritten.CharacterPatches[0].Current["location"] = "王城"
	assertIssueCode(t, ValidateProposalWithAuthorization(context, rewritten, authorization), "unauthorized_character_rewrite")

	added := cloneProposal(final)
	added.CharacterPatches = append(added.CharacterPatches, CharacterPatch{CharacterID: "qiao", ExpectedVersion: 2, Current: map[string]any{"trust": float64(2)}})
	assertIssueCode(t, ValidateProposalWithAuthorization(context, added, authorization), "unauthorized_character_patch")
}

func TestAuthorizedProposalCannotAddOrRewriteThreadPatch(t *testing.T) {
	context := testContinuityContext(t)
	plan := validProposal()
	plan.Narrative = ""
	plan.Choices = nil
	plan.ThreadPatches = []ThreadPatch{{Key: "lost-letter", Action: "resolve"}}
	authorization, err := AuthorizePlan(context, plan)
	if err != nil {
		t.Fatal(err)
	}

	final := plan
	final.Narrative = "信件终于送达。"
	final.Choices = []string{"离开", "追问"}
	added := cloneProposal(final)
	added.ThreadPatches = append(added.ThreadPatches, ThreadPatch{Key: "secret-tide", Action: "open", Title: "密潮"})
	assertIssueCode(t, ValidateProposalWithAuthorization(context, added, authorization), "unauthorized_thread_patch")

	rewritten := cloneProposal(final)
	rewritten.ThreadPatches[0].Action = "open"
	rewritten.ThreadPatches[0].Title = "重写"
	assertIssueCode(t, ValidateProposalWithAuthorization(context, rewritten, authorization), "unauthorized_thread_rewrite")
}

func testContinuityContext(t *testing.T) ContinuityContext {
	t.Helper()
	sim := baseSimulation()
	context, err := BuildContinuityContext(ContinuitySources{
		WorldSnapshot:       sim.WorldSnapshot,
		CommittedSimulation: sim,
		CommittedSummary:    sim.Summary,
	})
	if err != nil {
		t.Fatal(err)
	}
	return context
}

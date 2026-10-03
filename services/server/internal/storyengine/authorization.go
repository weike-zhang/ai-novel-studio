package storyengine

import "reflect"

// ProposalAuthorization seals the state mutations accepted during planning.
// Its fields are private so model output cannot mint or expand authorization.
// The narrative phase may add prose and choices, but cannot add, retarget, or
// rewrite character/thread changes from the validated plan.
type ProposalAuthorization struct {
	inputVersion    int64
	expectedNextSeq int
	characters      map[string]CharacterPatch
	threads         map[string]ThreadPatch
}

func AuthorizePlan(context ContinuityContext, plan TurnProposal) (ProposalAuthorization, error) {
	if err := ValidatePlanAgainstContext(context, plan); err != nil {
		return ProposalAuthorization{}, err
	}
	authorization := ProposalAuthorization{
		inputVersion:    plan.InputVersion,
		expectedNextSeq: plan.ExpectedNextSeq,
		characters:      make(map[string]CharacterPatch, len(plan.CharacterPatches)),
		threads:         make(map[string]ThreadPatch, len(plan.ThreadPatches)),
	}
	for _, patch := range plan.CharacterPatches {
		authorization.characters[patch.CharacterID] = cloneProposal(TurnProposal{CharacterPatches: []CharacterPatch{patch}}).CharacterPatches[0]
	}
	for _, patch := range plan.ThreadPatches {
		authorization.threads[patch.Key] = patch
	}
	return authorization, nil
}

func ValidateProposalWithAuthorization(context ContinuityContext, proposal TurnProposal, authorization ProposalAuthorization) error {
	issues := validationIssues(ValidateProposalAgainstContext(context, proposal))
	if proposal.InputVersion != authorization.inputVersion || proposal.ExpectedNextSeq != authorization.expectedNextSeq {
		issues = append(issues, ValidationIssue{Code: "authorization_boundary_changed", Field: "proposal", Message: "成稿不能改变规划阶段绑定的故事版本或回合"})
	}
	for i, patch := range proposal.CharacterPatches {
		allowed, ok := authorization.characters[patch.CharacterID]
		if !ok {
			issues = append(issues, ValidationIssue{Code: "unauthorized_character_patch", Field: characterPatchField(i, "characterId"), Message: "成稿不能修改规划阶段未授权的角色"})
			continue
		}
		if !reflect.DeepEqual(patch, allowed) {
			issues = append(issues, ValidationIssue{Code: "unauthorized_character_rewrite", Field: characterPatchField(i, "current"), Message: "成稿不能重写规划阶段已经锁定的角色变更"})
		}
	}
	for id := range authorization.characters {
		if !containsCharacterPatch(proposal.CharacterPatches, id) {
			issues = append(issues, ValidationIssue{Code: "authorized_character_patch_missing", Field: "characterPatches", Message: "成稿缺少规划阶段已授权的角色变更"})
		}
	}
	for i, patch := range proposal.ThreadPatches {
		allowed, ok := authorization.threads[patch.Key]
		if !ok {
			issues = append(issues, ValidationIssue{Code: "unauthorized_thread_patch", Field: threadPatchField(i, "key"), Message: "成稿不能修改规划阶段未授权的剧情线程"})
			continue
		}
		if !reflect.DeepEqual(patch, allowed) {
			issues = append(issues, ValidationIssue{Code: "unauthorized_thread_rewrite", Field: threadPatchField(i, "action"), Message: "成稿不能重写规划阶段已经锁定的剧情线程变更"})
		}
	}
	for key := range authorization.threads {
		if !containsThreadPatch(proposal.ThreadPatches, key) {
			issues = append(issues, ValidationIssue{Code: "authorized_thread_patch_missing", Field: "threadPatches", Message: "成稿缺少规划阶段已授权的剧情线程变更"})
		}
	}
	if len(issues) > 0 {
		return ValidationError{Issues: issues}
	}
	return nil
}

func containsCharacterPatch(patches []CharacterPatch, id string) bool {
	for _, patch := range patches {
		if patch.CharacterID == id {
			return true
		}
	}
	return false
}

func containsThreadPatch(patches []ThreadPatch, key string) bool {
	for _, patch := range patches {
		if patch.Key == key {
			return true
		}
	}
	return false
}

package storyengine

import (
	"fmt"
	"reflect"
	"strings"
)

const maxActionRunes = 2000

const (
	maxSceneTitleRunes   = 40
	maxSceneClosureRunes = 200
)

func ValidateAction(action string) error {
	action = strings.TrimSpace(action)
	if action == "" {
		return ValidationError{Issues: []ValidationIssue{{Code: "action_required", Field: "action", Message: "请先写下这一幕的行动"}}}
	}
	if len([]rune(action)) > maxActionRunes {
		return ValidationError{Issues: []ValidationIssue{{Code: "action_too_long", Field: "action", Message: "行动不能超过 2000 个字符"}}}
	}
	return nil
}

func ValidateProposal(sim Simulation, proposal TurnProposal) error {
	return validateProposal(sim, proposal, true)
}

func ValidateProposalAgainstContext(context ContinuityContext, proposal TurnProposal) error {
	return validateProposal(context.Simulation, proposal, true)
}

func ValidatePlan(sim Simulation, proposal TurnProposal) error {
	return validateProposal(sim, proposal, false)
}

func ValidatePlanAgainstContext(context ContinuityContext, proposal TurnProposal) error {
	return validateProposal(context.Simulation, proposal, false)
}

func validateProposal(sim Simulation, proposal TurnProposal, requireNarrative bool) error {
	issues := make([]ValidationIssue, 0)
	if proposal.InputVersion != sim.Version {
		issues = append(issues, ValidationIssue{Code: "stale_input_version", Field: "inputVersion", Message: "故事状态已更新，请基于最新界标重试"})
	}
	if proposal.ExpectedNextSeq != sim.LastSeq+1 {
		issues = append(issues, ValidationIssue{Code: "invalid_turn_sequence", Field: "expectedNextSeq", Message: "回合序号与当前故事不一致"})
	}
	if requireNarrative && strings.TrimSpace(proposal.Narrative) == "" {
		issues = append(issues, ValidationIssue{Code: "narrative_required", Field: "narrative", Message: "这一幕没有生成正文"})
	}
	if requireNarrative && len(proposal.Choices) != 0 && (len(proposal.Choices) < 2 || len(proposal.Choices) > 4) {
		issues = append(issues, ValidationIssue{Code: "invalid_choice_count", Field: "choices", Message: "岔路只能为 0 条（世界不可干预）或 2 至 4 条"})
	}
	for i, award := range proposal.Achievements {
		if strings.TrimSpace(award.Key) == "" {
			issues = append(issues, ValidationIssue{Code: "achievement_key_required", Field: fmt.Sprintf("achievements.%d.key", i), Message: "成就必须声明稳定的标识"})
		}
		if (strings.TrimSpace(award.Title) == "") != (strings.TrimSpace(award.Description) == "") {
			issues = append(issues, ValidationIssue{Code: "achievement_meta_incomplete", Field: fmt.Sprintf("achievements.%d", i), Message: "成就的名称与简介必须同时提供"})
		}
	}
	sceneTitle := strings.TrimSpace(proposal.Scene.Title)
	sceneClosure := strings.TrimSpace(proposal.Scene.Closure)
	if proposal.Scene.StartsNewScene {
		if sceneTitle == "" {
			issues = append(issues, ValidationIssue{Code: "scene_title_required", Field: "scene.title", Message: "开启新场景时必须给出场景标题"})
		} else if len([]rune(sceneTitle)) > maxSceneTitleRunes {
			issues = append(issues, ValidationIssue{Code: "scene_title_too_long", Field: "scene.title", Message: "场景标题不能超过 40 个字"})
		}
		if sceneClosure == "" {
			issues = append(issues, ValidationIssue{Code: "scene_closure_required", Field: "scene.closure", Message: "开启新场景时必须一句话收束当前场景"})
		} else if len([]rune(sceneClosure)) > maxSceneClosureRunes {
			issues = append(issues, ValidationIssue{Code: "scene_closure_too_long", Field: "scene.closure", Message: "场景收束说明不能超过 200 个字"})
		}
	} else if sceneTitle != "" || sceneClosure != "" {
		issues = append(issues, ValidationIssue{Code: "scene_fields_on_continuation", Field: "scene", Message: "未开启新场景时不得填写场景标题或收束说明"})
	}
	seenCharacters := make(map[string]struct{}, len(proposal.CharacterPatches))
	for i, patch := range proposal.CharacterPatches {
		if _, duplicate := seenCharacters[patch.CharacterID]; duplicate {
			issues = append(issues, ValidationIssue{Code: "duplicate_character_patch", Field: characterPatchField(i, "characterId"), Message: "同一角色每回合只能声明一次状态变更"})
		}
		seenCharacters[patch.CharacterID] = struct{}{}
		character, ok := sim.Characters[patch.CharacterID]
		if !ok {
			issues = append(issues, ValidationIssue{Code: "unknown_character", Field: characterPatchField(i, "characterId"), Message: "角色不属于已提交的模拟状态"})
			continue
		}
		if len(patch.Identity) > 0 || patch.Name != nil {
			issues = append(issues, ValidationIssue{Code: "identity_mutation", Field: characterPatchField(i, "identity"), Message: "角色姓名和身份硬事实不可由模型修改"})
		}
		if len(patch.Current) == 0 {
			issues = append(issues, ValidationIssue{Code: "empty_character_patch", Field: characterPatchField(i, "current"), Message: "角色状态变更不能为空"})
		}
		if patch.ExpectedVersion > 0 {
			if patch.ExpectedVersion != character.Version {
				issues = append(issues, ValidationIssue{Code: "stale_character_version", Field: characterPatchField(i, "expectedVersion"), Message: "角色状态已发生变化"})
			}
		} else if !expectedValuesCoverPatch(patch.ExpectedCurrent, patch.Current) {
			issues = append(issues, ValidationIssue{Code: "character_precondition_required", Field: characterPatchField(i, "expectedCurrent"), Message: "每个角色变量变更必须声明角色版本或该变量的预期旧值"})
		}
		for key := range patch.Current {
			if _, protected := character.Identity[key]; protected {
				issues = append(issues, ValidationIssue{Code: "identity_mutation", Field: characterPatchField(i, "current."+key), Message: "角色身份硬事实不能由回合状态覆盖"})
			}
		}
		for key, expected := range patch.ExpectedCurrent {
			actual, exists := character.Current[key]
			if !exists || !reflect.DeepEqual(actual, expected) {
				issues = append(issues, ValidationIssue{Code: "stale_character_value", Field: characterPatchField(i, "expectedCurrent."+key), Message: "角色变量的已提交值与预期旧值不一致"})
			}
		}
	}
	seenThreads := make(map[string]struct{}, len(proposal.ThreadPatches))
	for i, patch := range proposal.ThreadPatches {
		if _, duplicate := seenThreads[patch.Key]; duplicate {
			issues = append(issues, ValidationIssue{Code: "duplicate_thread_patch", Field: threadPatchField(i, "key"), Message: "同一剧情线程每回合只能声明一次变更"})
		}
		seenThreads[patch.Key] = struct{}{}
		switch patch.Action {
		case "open":
			if _, exists := sim.Threads[patch.Key]; exists {
				issues = append(issues, ValidationIssue{Code: "thread_already_exists", Field: threadPatchField(i, "key"), Message: "这条线索已经存在"})
			}
			if strings.TrimSpace(patch.Key) == "" || strings.TrimSpace(patch.Title) == "" {
				issues = append(issues, ValidationIssue{Code: "invalid_new_thread", Field: threadPatchField(i, "key"), Message: "新剧情线程必须声明稳定标识和标题"})
			}
		case "resolve":
			thread, exists := sim.Threads[patch.Key]
			if !exists || thread.Status != "open" {
				issues = append(issues, ValidationIssue{Code: "thread_not_open", Field: threadPatchField(i, "key"), Message: "只能结束已提交状态中尚未完成的线索"})
			}
		default:
			issues = append(issues, ValidationIssue{Code: "invalid_thread_action", Field: threadPatchField(i, "action"), Message: "线索动作必须是 open 或 resolve"})
		}
	}
	if len(proposal.CharacterPatches)+len(proposal.ThreadPatches) > 0 && len(proposal.Events) == 0 {
		issues = append(issues, ValidationIssue{Code: "missing_trace_event", Field: "events", Message: "任何状态变化都必须能追溯到规范事件"})
	}
	if len(issues) > 0 {
		return ValidationError{Issues: issues}
	}
	return nil
}

func expectedValuesCoverPatch(expected, current map[string]any) bool {
	if len(current) == 0 {
		return false
	}
	for key := range current {
		if _, ok := expected[key]; !ok {
			return false
		}
	}
	return true
}

func validationIssues(err error) []ValidationIssue {
	if err == nil {
		return nil
	}
	if validation, ok := err.(ValidationError); ok {
		return append([]ValidationIssue(nil), validation.Issues...)
	}
	return []ValidationIssue{{Code: "validation_failed", Field: "proposal", Message: err.Error()}}
}

func characterPatchField(index int, suffix string) string {
	return fmt.Sprintf("characterPatches.%d.%s", index, suffix)
}

func threadPatchField(index int, suffix string) string {
	return fmt.Sprintf("threadPatches.%d.%s", index, suffix)
}

package worlds

import (
	"context"
	"testing"
)

func TestRuleModeratorRoutesContent(t *testing.T) {
	moderator := NewRuleModerator()
	ctx := context.Background()

	tests := []struct {
		name   string
		title  string
		action ModerationAction
	}{
		{"normal content allows", "雾灯港：这座城每晚忘掉一个人，今晚是我", ModerationAllow},
		{"hard blocked term blocks", "炸弹制作与伤害指导", ModerationBlock},
		{"high risk term reviews", "关于台湾的政治讨论", ModerationManualReview},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := moderator.ReviewPublication(ctx, PublicationContent{
				Title: tt.title, Summary: "测试简介", Setting: []byte(`{}`),
			})
			if err != nil {
				t.Fatalf("ReviewPublication() error = %v", err)
			}
			if decision.Action != tt.action {
				t.Fatalf("action = %q, want %q", decision.Action, tt.action)
			}
			if err := validateModerationDecision(decision); err != nil {
				t.Fatalf("decision validation = %v", err)
			}
		})
	}
}

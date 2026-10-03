package worlds

import (
	"context"
	"strings"
)

// RuleModerator is the production publication gate. It is a conservative
// rule engine: hard-blocked terms stop publication, high-risk political and
// reality-based terms route to manual review, and everything else is allowed.
//
// It intentionally only covers publication; model-side input/output filtering
// remains a separate, stricter layer. Rule hits never leak the exact keyword
// list to users.
type RuleModerator struct {
	RuleVersion string
}

func NewRuleModerator() *RuleModerator {
	return &RuleModerator{RuleVersion: "publish-v1"}
}

var _ PublicationModerator = (*RuleModerator)(nil)

// hardBlockedTerms are content categories the platform must never publish.
var hardBlockedTerms = []string{
	"未成年人性", "幼女", "幼童", "儿童色情", "色情交易", "卖淫", "嫖娼",
	"淫秽", "露骨性描写", "现实伤害指导", "自杀方法", "制毒", "毒品制作",
	"恐怖袭击", "炸弹制作", "枪支制作", "教唆犯罪", "诈骗话术",
}

// highRiskTerms route to manual review: reality-based political or social
// content where an automated decision is too risky.
var highRiskTerms = []string{
	"习近平", "政治局", "党中央", "国家主席", "国务院", "人民政府",
	"台湾", "香港", "澳门", "西藏", "新疆", "钓鱼岛", "南海",
	"法轮功", "邪教", "英雄烈士", "南京大屠杀", "文化大革命", "六四",
}

func (m *RuleModerator) ReviewPublication(_ context.Context, content PublicationContent) (ModerationDecision, error) {
	text := strings.ToLower(content.Title + "\n" + content.Summary + "\n" + string(content.Setting))
	for _, term := range hardBlockedTerms {
		if strings.Contains(text, term) {
			return ModerationDecision{
				Action: ModerationBlock, Category: "hard_blocked",
				ReasonCode: "illegal_content", RuleVersion: m.RuleVersion, Source: "rule_engine",
			}, nil
		}
	}
	for _, term := range highRiskTerms {
		if strings.Contains(text, term) {
			return ModerationDecision{
				Action: ModerationManualReview, Category: "high_risk",
				ReasonCode: "reality_based_content", RuleVersion: m.RuleVersion, Source: "rule_engine",
			}, nil
		}
	}
	return ModerationDecision{
		Action: ModerationAllow, Category: "allow",
		ReasonCode: "passed_rule_check", RuleVersion: m.RuleVersion, Source: "rule_engine",
	}, nil
}

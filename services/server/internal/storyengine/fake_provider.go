package storyengine

import (
	"context"
	"fmt"
)

type FakeProvider struct{}

func (FakeProvider) GenerateTurnProposal(_ context.Context, continuity ContinuityContext, action string) (TurnProposal, error) {
	sim := continuity.Simulation
	return TurnProposal{
		InputVersion:    sim.Version,
		ExpectedNextSeq: sim.LastSeq + 1,
		Narrative:       fmt.Sprintf("你选择了：%s。雾从旧港的石阶间缓慢退去，一封没有邮戳的信停在脚边。远处的钟声只响了十一次，而信封上新鲜的墨迹正写着你的名字。", action),
		Choices:         []string{"拆开信封", "追上远处的送信人", "先观察周围的动静"},
		Events:          []StoryEvent{{Type: "action_taken", SubjectRef: "player", Payload: map[string]any{"action": action}}},
	}, nil
}

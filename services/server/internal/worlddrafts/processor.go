package worlddrafts

import (
	"context"
	"errors"
	"fmt"
)

type Processor struct {
	store    *Store
	provider Provider
}

func NewProcessor(store *Store, provider Provider) *Processor {
	return &Processor{store: store, provider: provider}
}

func (p *Processor) Process(ctx context.Context, jobID, workerID string) (WorldResult, error) {
	job, err := p.store.GetForProcessing(ctx, jobID)
	if err != nil {
		return WorldResult{}, err
	}
	if job.Status != StatusQueued {
		return WorldResult{}, ErrConcurrentState
	}
	attempt, err := p.store.BeginAttempt(ctx, jobID, p.provider.Name(), p.provider.Model(), workerID)
	if err != nil {
		return WorldResult{}, err
	}
	draft, usage, err := p.provider.Generate(ctx, job.Input)
	if err != nil {
		_ = p.store.Fail(ctx, job.ID, attempt.ID, workerID, "generation_failed")
		return WorldResult{}, fmt.Errorf("generate world draft: %w", err)
	}
	if err := p.store.MarkValidating(ctx, job.ID, workerID); err != nil {
		if !errors.Is(err, ErrConcurrentState) {
			_ = p.store.Fail(ctx, job.ID, attempt.ID, workerID, "validation_transition_failed")
		}
		return WorldResult{}, err
	}
	if err := draft.Validate(); err != nil {
		_ = p.store.Fail(ctx, job.ID, attempt.ID, workerID, "draft_rejected")
		return WorldResult{}, err
	}
	return p.store.CommitSuccess(ctx, CommitInput{Job: job, Attempt: attempt, WorkerID: workerID, Draft: draft, Usage: usage})
}

type FakeProvider struct{}

func (FakeProvider) Name() string  { return "fake" }
func (FakeProvider) Model() string { return "deterministic-world-v1" }

func (FakeProvider) Generate(_ context.Context, input GenerateInput) (GeneratedDraft, Usage, error) {
	if _, err := NormalizeInput(input); err != nil {
		return GeneratedDraft{}, Usage{}, err
	}
	title := "雾灯港：这座城每晚忘掉一个人，今晚是我"
	if input.Topic != "" {
		title = input.Topic + "：初稿"
	}
	rating := input.ContentRating
	if rating == "" {
		rating = "全年龄"
	}
	return GeneratedDraft{
		Title:   title,
		Summary: "最后一班渡船离港后，雾里响起一口没有指针的潮钟。你必须在黎明前找回它，否则这座港城会忘掉一个人。",
		Type:    "推理",
		Tags:    []string{"合成", "测试"},
		Cover:   "",
		Discovery: Discovery{
			Hook:     "一座会遗忘人的港城，今晚轮到谁消失？",
			Promise:  "追踪潮雾里的旧记忆，在相信守信人和追问真相之间作出选择。",
			PlayTime: "首幕约 3 分钟",
			Format:   "短回合悬疑",
		},
		WorldBackground: WorldBackground{
			EraPlace: "一座永远被薄雾笼罩的港口城市",
			Rules:    "每次潮汐会改写一封寄出但没有收件人的信。",
			Conflict: "第七码头正在被缓慢吞没，但市长声称这是扩张计划。",
		},
		PlayerIdentity: PlayerIdentity{
			Name:       "归乡人",
			Background: "离开港口多年的游子，收到了一封来自自己笔迹的信。",
			Goal:       "查明那封信为何存在",
			Freedom:    "可以调查、追问和选择相信谁",
		},
		Characters: []Character{
			{Name: "林岚", Role: "守信人", Personality: "温和而守旧", Goal: "守住第七码头的钟", Relation: "旧友", Public: true},
			{Name: "郑泊", Role: "港务长", Personality: "务实而戒备", Goal: "推进拆除计划", Relation: "对立", Public: false},
		},
		Narrative: Narrative{
			Style:       "克制的近景叙述",
			PointOfView: "第二人称",
			TurnLength:  "中等段落",
			Openness:    "允许延伸调查",
		},
		Opening: Opening{
			Text:    "最后一班渡船离港后，码头的雾突然漫过鞋面。你听见一口没有指针的潮钟在雾里响了一次，空白航图上随即浮出一条不属于任何港口的线。\n\n守信人林岚从灯影里走出来，铜匙在她掌心发冷。她没有问你从哪里来，只盯着那条新出现的航线：‘如果你看见第七码头的第二声钟，别回答。’远处，第二声钟已经悬在雾里。",
			Actions: []string{"拆开信封", "追上远处的送信人", "先观察周围的动静"},
		},
		ContentRating:     rating,
		ContentBoundaries: "不包含现实伤害指导，不涉及真实人物",
	}, Usage{InputTokens: 128, OutputTokens: 256, TotalTokens: 384}, nil
}

var _ Provider = FakeProvider{}

package turnjobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/weike-zhang/ai-novel-studio/services/server/internal/storyengine"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

// OpenAICompatibleProvider drives the turn generation pipeline against an
// OpenAI-compatible endpoint. It is wired for the development 5.6luna test
// endpoint and must only ever run synthetic worlds, characters and accounts.
type OpenAICompatibleProvider struct {
	client openai.Client
	name   string
	model  string
}

func NewOpenAICompatibleProvider(name, baseURL, apiKey, model string, timeout time.Duration) (*OpenAICompatibleProvider, error) {
	name, baseURL, apiKey, model = strings.TrimSpace(name), strings.TrimRight(strings.TrimSpace(baseURL), "/"), strings.TrimSpace(apiKey), strings.TrimSpace(model)
	if name == "" || baseURL == "" || apiKey == "" || model == "" {
		return nil, errors.New("turn provider configuration is incomplete")
	}
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	client := openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL), option.WithHTTPClient(&http.Client{Timeout: timeout}))
	return &OpenAICompatibleProvider{client: client, name: name, model: model}, nil
}

// NewOpenAICompatibleProviderWithClient builds a provider around an existing
// OpenAI client, which may itself carry failover behaviour.
func NewOpenAICompatibleProviderWithClient(name, model string, client openai.Client) (*OpenAICompatibleProvider, error) {
	name, model = strings.TrimSpace(name), strings.TrimSpace(model)
	if name == "" || model == "" {
		return nil, errors.New("turn provider configuration is incomplete")
	}
	return &OpenAICompatibleProvider{client: client, name: name, model: model}, nil
}

func (p *OpenAICompatibleProvider) Name() string  { return p.name }
func (p *OpenAICompatibleProvider) Model() string { return p.model }

type turnPlanOutput struct {
	Events           []storyengine.StoryEvent          `json:"events"`
	CharacterPatches []storyengine.CharacterPatch      `json:"characterPatches"`
	ThreadPatches    []storyengine.ThreadPatch         `json:"threadPatches"`
	Scene            storyengine.SceneProposal         `json:"scene"`
	Achievements     []storyengine.AchievementProposal `json:"achievements"`
}

type turnNarrativeOutput struct {
	Narrative string   `json:"narrative"`
	Choices   []string `json:"choices"`
}

func (p *OpenAICompatibleProvider) GenerateTurnPlan(ctx context.Context, continuity storyengine.ContinuityContext, action, interaction string) (storyengine.TurnProposal, map[string]any, error) {
	schema, err := json.MarshalIndent(turnPlanSchema, "", "  ")
	if err != nil {
		return storyengine.TurnProposal{}, nil, fmt.Errorf("encode turn plan schema: %w", err)
	}
	actionLine := action
	switch interaction {
	case InteractionContinue:
		actionLine = "继续——沿当前最显而易见的路径前进，不制造新的玩家抉择，也不替玩家表态"
	case InteractionBranch:
		actionLine = "选择的岔路：" + action
	}
	prompt := fmt.Sprintf("你正在推进一个 AI 互动小说模拟。\n\n当前行动：%s\n\n当前场景：%s\n\n世界快照：%s\n\n模拟状态：%s\n\n角色：%s\n\n未完成线索：%s\n\n最近回合：%s\n\n规则：characterPatches.characterId 必须逐字复制‘角色’对象中的键，不能填写角色姓名或自造 ID；每个角色变更必须携带该角色当前的 version 值作为 expectedVersion（角色列表里已给出），current 只放本轮要改变的变量，不要改身份或姓名。threadPatches.key 必须逐字复制‘未完成线索’中的 key；threadPatches.action 只能是 open（开新线索，需给 title）或 resolve（结束已有线索）；没有需要结束的线索就返回空数组。events 记录本轮发生的规范事件，每个状态变化都要有对应事件可追溯。每次行动必须产生一个可观察的后果；每回合最多推进一个核心线索、揭示一个关键事实，不得无授权引入重大反转。事件 payload 中尽量记录 beat、reveal、stakes 和 choicePressure，供正文阶段使用。scene 决定这一幕是否开启一个新场景：场景是连续时空/节拍的叙事单元，只有时间、地点或事件节拍发生有意义的跳跃时才开启新场景（换场、换时间、隔夜、关键转折）；同一场景内的连续推进必须返回 startsNewScene=false。玩家行动明确快进大量剧情或时间（例如‘第二天早晨’、‘数月后’、‘直接赶往下一处’）时，应把 startsNewScene 设为 true，让正文先收束当前场景、交代中间发生的事，再开启新场景，不要硬留在旧场景里草草结束。一般 2-5 回合才出现一次场景切换，不要每回合都开新场景。新场景必须给出 4-12 字的 title，以及一句话 closure 收束当前场景并概括跳过的关键事件或时间。achievements 是本回合判定命中的成就，可为空数组：命中世界成就清单里的条目时，key 必须逐字复制清单中的 key，title 和 description 留空；只有出现了足够特别、值得纪念的瞬间而清单里没有对应条目时，才给出一个‘涌现成就’，key 用简短稳定的英文或拼音 slug，title 用 4-14 字有氛围的成就名，description 用一句简短简介。涌现成就必须真的特别，不要每回合都发，整个旅程最多出现两三个；不要为了发成就而发成就。%s\n请输出一个 JSON 对象，严格遵循以下结构，不包含任何解释文字：\n%s",
		actionLine, currentSceneLabel(continuity), continuity.WorldSnapshot, encodeAny(continuity.Simulation.State), encodeAny(continuity.Simulation.Characters), encodeAny(continuity.Simulation.Threads), encodeTurns(continuity.RecentTurns), worldAchievementCatalog(continuity.WorldSnapshot), schema)
	response, err := p.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: shared.ChatModel(p.model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.DeveloperMessage("你是AI 小说创作台的剧情规划引擎。只返回一个合法的 JSON 对象，不要使用 markdown 代码围栏，不要包含任何解释文字。先尊重玩家刚刚提交的行动，再设计一个可观察后果和一个可继续追踪的线索；characterId 和 thread key 只能使用上下文中已有的精确键，不得使用角色姓名替代 ID，不得自造键。事件、角色状态和线索必须与给定世界和当前状态一致。不虚构人物死亡或角色身份改变，不让模型替玩家做下一次关键选择；保持虚构，不生成现实伤害指导。threadPatches.action 只允许 open 或 resolve。scene.startsNewScene 只有在时间/地点/节拍发生有意义跳跃或玩家明确快进时才为 true，不允许每回合都开新场景。achievements 只在命中清单或出现真正特别的瞬间时返回，宁缺毋滥。"),
			openai.UserMessage(prompt),
		},
		MaxCompletionTokens: openai.Int(1400),
	})
	if err != nil {
		return storyengine.TurnProposal{}, nil, fmt.Errorf("generate turn plan: %w", err)
	}
	usage := map[string]any{"inputTokens": response.Usage.PromptTokens, "outputTokens": response.Usage.CompletionTokens, "totalTokens": response.Usage.TotalTokens}
	if len(response.Choices) != 1 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		return storyengine.TurnProposal{}, usage, errors.New("turn plan provider returned no structured choice")
	}
	var output turnPlanOutput
	if err := json.Unmarshal(stripArtifactWrapper(response.Choices[0].Message.Content), &output); err != nil {
		if extracted := extractFencedJSON([]byte(response.Choices[0].Message.Content)); extracted != nil {
			if nestedErr := json.Unmarshal(extracted, &output); nestedErr == nil {
				return p.buildProposal(continuity, output), usage, nil
			}
		}
		return storyengine.TurnProposal{}, usage, fmt.Errorf("decode turn plan: %w", err)
	}
	return p.buildProposal(continuity, output), usage, nil
}

func (p *OpenAICompatibleProvider) GenerateNarrativeDraft(ctx context.Context, continuity storyengine.ContinuityContext, action, interaction string, plan storyengine.TurnProposal) (NarrativeDraft, map[string]any, error) {
	schema, err := json.MarshalIndent(turnNarrativeSchema(plan.Scene.StartsNewScene), "", "  ")
	if err != nil {
		return NarrativeDraft{}, nil, fmt.Errorf("encode narrative schema: %w", err)
	}
	devMessage, sceneInstruction := narrativeInstructions(plan)
	actionLine := action
	if interaction == InteractionContinue {
		actionLine = "继续——沿当前最显而易见的路径前进"
	}
	prompt := fmt.Sprintf("你正在为以下 AI 互动小说回合撰写正文。\n\n当前行动：%s\n\n当前场景：%s\n\n世界快照：%s\n\n当前状态：%s\n\n角色：%s\n\n最近回合：%s\n\n剧情事件：%s\n\n角色状态变化：%s\n\n线索变化：%s\n\n%s\n\n请输出一个 JSON 对象，严格遵循：\n%s\n正文用中文，贴合给定世界的风格，不要使用 markdown 代码围栏，不要重复最近回合已经写过的环境和事实。",
		actionLine, currentSceneLabel(continuity), continuity.WorldSnapshot, encodeAny(continuity.Simulation.State), encodeAny(continuity.Simulation.Characters), encodeTurns(continuity.RecentTurns), encodeAny(plan.Events), encodeAny(plan.CharacterPatches), encodeAny(plan.ThreadPatches), sceneInstruction, schema)
	response, err := p.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: shared.ChatModel(p.model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.DeveloperMessage(devMessage),
			openai.UserMessage(prompt),
		},
		MaxCompletionTokens: openai.Int(3000),
	})
	if err != nil {
		return NarrativeDraft{}, nil, fmt.Errorf("generate narrative draft: %w", err)
	}
	usage := map[string]any{"inputTokens": response.Usage.PromptTokens, "outputTokens": response.Usage.CompletionTokens, "totalTokens": response.Usage.TotalTokens}
	if len(response.Choices) != 1 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		return NarrativeDraft{}, usage, errors.New("narrative provider returned no structured choice")
	}
	var output turnNarrativeOutput
	if err := json.Unmarshal(stripArtifactWrapper(response.Choices[0].Message.Content), &output); err != nil {
		if extracted := extractFencedJSON([]byte(response.Choices[0].Message.Content)); extracted != nil {
			if nestedErr := json.Unmarshal(extracted, &output); nestedErr == nil {
				return NarrativeDraft{Narrative: output.Narrative, Choices: output.Choices}, usage, nil
			}
		}
		return NarrativeDraft{}, usage, fmt.Errorf("decode narrative draft: %w", err)
	}
	return NarrativeDraft{Narrative: output.Narrative, Choices: output.Choices}, usage, nil
}

func (p *OpenAICompatibleProvider) buildProposal(continuity storyengine.ContinuityContext, output turnPlanOutput) storyengine.TurnProposal {
	achievements := make([]storyengine.AchievementProposal, 0, len(output.Achievements))
	for _, award := range output.Achievements {
		award.Key = strings.TrimSpace(award.Key)
		award.Title = strings.TrimSpace(award.Title)
		award.Description = strings.TrimSpace(award.Description)
		if award.Key != "" {
			achievements = append(achievements, award)
		}
	}
	return storyengine.TurnProposal{
		InputVersion:     continuity.Simulation.Version,
		ExpectedNextSeq:  continuity.Simulation.LastSeq + 1,
		Events:           output.Events,
		CharacterPatches: output.CharacterPatches,
		ThreadPatches:    output.ThreadPatches,
		Scene:            storyengine.SceneProposal{StartsNewScene: output.Scene.StartsNewScene, Title: strings.TrimSpace(output.Scene.Title), Closure: strings.TrimSpace(output.Scene.Closure)},
		Achievements:     achievements,
	}
}

// worldAchievementCatalog renders the creator-defined achievements of the world
// (if any) so the model can judge hits and copy exact keys.
func worldAchievementCatalog(worldSnapshot json.RawMessage) string {
	var frozen struct {
		Setting struct {
			Achievements []struct {
				Key    string `json:"key"`
				Title  string `json:"title"`
				Hint   string `json:"hint"`
				Reward int64  `json:"reward"`
			} `json:"achievements"`
		} `json:"setting"`
	}
	if err := json.Unmarshal(worldSnapshot, &frozen); err != nil {
		return "本世界没有预定义成就清单。"
	}
	if len(frozen.Setting.Achievements) == 0 {
		return "本世界没有预定义成就清单。"
	}
	var builder strings.Builder
	builder.WriteString("本世界的成就清单（key 必须逐字复制）：")
	for _, definition := range frozen.Setting.Achievements {
		key := strings.TrimSpace(definition.Key)
		title := strings.TrimSpace(definition.Title)
		if key == "" {
			continue
		}
		builder.WriteString("\n- key=" + key)
		if title != "" {
			builder.WriteString(" 成就名=" + title)
		}
		if hint := strings.TrimSpace(definition.Hint); hint != "" {
			builder.WriteString(" 达成提示=" + hint)
		}
	}
	return builder.String()
}

// currentSceneLabel renders the narrative position for the model. RecentTurns is
// ascending, so the last turn carries the current scene number and, when this
// scene was newly opened, its title.
func currentSceneLabel(continuity storyengine.ContinuityContext) string {
	if len(continuity.RecentTurns) > 0 {
		last := continuity.RecentTurns[len(continuity.RecentTurns)-1]
		if title := strings.TrimSpace(last.SceneTitle); title != "" {
			return fmt.Sprintf("第 %d 幕「%s」", last.SceneNo, title)
		}
		return fmt.Sprintf("第 %d 幕", last.SceneNo)
	}
	return "第 1 幕「旅程开场」"
}

// narrativeInstructions returns the developer message and the per-turn scene
// instruction for the narrative stage. A scene-breaking turn must first close
// the current scene and expand the closure into enough prose to cover the jump,
// instead of ending it in a few rushed sentences.
func narrativeInstructions(plan storyengine.TurnProposal) (string, string) {
	if !plan.Scene.StartsNewScene {
		return "你是AI 小说创作台的叙事作者。只返回一个合法的 JSON 对象，包含 narrative 正文和 choices 建议行动。正文控制在 220-380 个中文字符，分 3 段、段落之间用空行分隔。段落长短要根据表达需要自然错落，不要机械套用固定的长短模式：关键瞬间、一句对话或一个反转用短段（甚至一两句）制造节奏与张力；需要铺陈的环境、行动推进与后果用长段展开。让每一段的长短服务于它要表达的内容，整体读起来有张弛、有重点，绝不写成一段接一段等长的方块。每句话只能承担一个任务：事件、线索、关系、后果或选择。删掉不推进剧情的环境铺陈、抽象抒情和重复信息；开头最多用两句交代场景。默认不使用比喻，每 300 字最多一个；不要连续使用‘像……’、风声/灯光/心跳等通用氛围句；身体感受只有在会改变行动时才保留。每回合只推进一个核心线索、最多给一个新事实，不要替玩家做关键决定，不要堆砌设定或连续引入新角色。choices 只能为 0 条或 2-4 条：只有当这一幕结束时玩家真的有岔路可选，才给 2-4 条、每条 8-24 个字，分别体现追查、冒险、关系或资源取舍，必须来自正文当前局面，不能是同义的‘继续/观察/离开’；如果世界不可干预、剧情强推或玩家没有真正的抉择，必须返回空数组 []。保持虚构，不要生成现实伤害指导。", "这一幕延续当前场景，正文继续使用中文、贴合给定世界风格；段落长短按表达需要自然错落，避免每段等长。"
	}
	return "你是AI 小说创作台的叙事作者。只返回一个合法的 JSON 对象，包含 narrative 正文和 choices 建议行动。这一幕是场景切换：先用 1-2 句自然收束当前场景，再依据 scene.closure 概括中间跳过的关键事件、时间或行程，最后平滑过渡并开启新场景（标题见 scene.title）。正文应达到 600-1000 个中文字符，用足够篇幅把跳过的内容讲清楚，禁止用约 300 字草草收尾；可分成 4-7 个自然段、段落之间用空行分隔。段落长短根据表达需要自然错落，不要机械套用固定模式：短段一两句用来强调某个关键瞬间或一句对话，长段铺陈情节与过渡；每一段的长短都服务于它要表达的内容，让整段读起来有张弛、有重点，避免段落等长。收束与交代保持具体：交代关键的对话、决定、结果和可观察变化，不写空洞的‘时光飞逝’。choices 只能为 0 条或 2-4 条：新场景展开后玩家有真实岔路时给 2-4 条、每条 8-24 字，分别体现追查、冒险、关系或资源取舍；若新场景仍无玩家抉择则返回空数组 []。保持虚构，不要生成现实伤害指导。", fmt.Sprintf("这一幕开启新场景「%s」。正文必须先用 1-2 句收束当前场景，再扩写 scene.closure 交代中间发生了什么，最后进入新场景；总长度 600-1000 字，段落长短按表达需要自然错落、禁止草草结束。", strings.TrimSpace(plan.Scene.Title))
}

var turnPlanSchema = map[string]any{
	"type": "object", "additionalProperties": false,
	"required": []string{"events", "characterPatches", "threadPatches", "scene"},
	"properties": map[string]any{
		"events": arraySchema(map[string]any{
			"type": "object", "additionalProperties": false,
			"required": []string{"type", "subjectRef"},
			"properties": map[string]any{
				"type":       stringSchema(),
				"subjectRef": stringSchema(),
				"objectRef":  stringSchema(),
				"payload":    map[string]any{"type": "object", "additionalProperties": true},
			},
		}),
		"characterPatches": arraySchema(map[string]any{
			"type": "object", "additionalProperties": false,
			"required": []string{"characterId", "expectedVersion", "current"},
			"properties": map[string]any{
				"characterId":     stringSchema(),
				"expectedVersion": map[string]any{"type": "integer"},
				"current":         map[string]any{"type": "object", "additionalProperties": true},
			},
		}),
		"threadPatches": arraySchema(map[string]any{
			"type": "object", "additionalProperties": false,
			"required": []string{"key", "action"},
			"properties": map[string]any{
				"key":    stringSchema(),
				"action": map[string]any{"type": "string", "enum": []string{"open", "resolve"}},
				"title":  stringSchema(),
			},
		}),
		"scene": map[string]any{
			"type": "object", "additionalProperties": false,
			"required": []string{"startsNewScene", "title", "closure"},
			"properties": map[string]any{
				"startsNewScene": map[string]any{"type": "boolean"},
				"title":          stringSchema(),
				"closure":        stringSchema(),
			},
		},
		"achievements": map[string]any{
			"type": "array", "maxItems": 3,
			"items": map[string]any{
				"type": "object", "additionalProperties": false,
				"required": []string{"key"},
				"properties": map[string]any{
					"key":         stringSchema(),
					"title":       stringSchema(),
					"description": stringSchema(),
				},
			},
		},
	},
}

func turnNarrativeSchema(sceneBreak bool) map[string]any {
	length := map[string]any{"type": "string", "minLength": 220, "maxLength": 420}
	if sceneBreak {
		length = map[string]any{"type": "string", "minLength": 600, "maxLength": 1100}
	}
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"narrative", "choices"},
		"properties": map[string]any{
			"narrative": length,
			"choices": map[string]any{
				"type": "array", "maxItems": 4,
				"items": map[string]any{"type": "string", "minLength": 8, "maxLength": 24},
			},
		},
	}
}

func encodeAny(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func encodeTurns(turns []storyengine.Turn) string {
	raw, err := json.Marshal(turns)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func stringSchema() map[string]any         { return map[string]any{"type": "string"} }
func arraySchema(items any) map[string]any { return map[string]any{"type": "array", "items": items} }

func stripArtifactWrapper(content string) []byte {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, ":::") {
		return []byte(content)
	}
	if start := strings.Index(content, "\n"); start >= 0 {
		content = strings.TrimSpace(content[start+1:])
	}
	for strings.HasSuffix(content, ":::") {
		content = strings.TrimSpace(strings.TrimSuffix(content, ":::"))
	}
	return []byte(content)
}

func extractFencedJSON(content []byte) []byte {
	const fence = "```"
	text := string(content)
	start := strings.Index(text, fence)
	if start < 0 {
		return nil
	}
	body := text[start+len(fence):]
	if strings.HasPrefix(body, "json") {
		body = body[len("json"):]
	}
	body = strings.TrimSpace(body)
	if end := strings.LastIndex(body, fence); end >= 0 {
		body = strings.TrimSpace(body[:end])
	}
	if len(body) == 0 || body[0] != '{' {
		return nil
	}
	return []byte(body)
}

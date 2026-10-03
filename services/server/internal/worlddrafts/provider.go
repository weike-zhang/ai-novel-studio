package worlddrafts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

type Provider interface {
	Name() string
	Model() string
	Generate(context.Context, GenerateInput) (GeneratedDraft, Usage, error)
}

type OpenAICompatibleProvider struct {
	client openai.Client
	name   string
	model  string
}

func NewOpenAICompatibleProvider(name, baseURL, apiKey, model string, timeout time.Duration) (*OpenAICompatibleProvider, error) {
	name, baseURL, apiKey, model = strings.TrimSpace(name), strings.TrimRight(strings.TrimSpace(baseURL), "/"), strings.TrimSpace(apiKey), strings.TrimSpace(model)
	if name == "" || baseURL == "" || apiKey == "" || model == "" {
		return nil, errors.New("world draft provider configuration is incomplete")
	}
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	client := openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL), option.WithHTTPClient(&http.Client{Timeout: timeout}))
	return &OpenAICompatibleProvider{client: client, name: name, model: model}, nil
}

// NewOpenAICompatibleProviderWithClient builds a provider around an existing
// OpenAI client, which may itself carry failover behaviour.
func NewOpenAICompatibleProviderWithClient(name, model string, client openai.Client) (*OpenAICompatibleProvider, error) {
	name, model = strings.TrimSpace(name), strings.TrimSpace(model)
	if name == "" || model == "" {
		return nil, errors.New("world draft provider configuration is incomplete")
	}
	return &OpenAICompatibleProvider{client: client, name: name, model: model}, nil
}

func (p *OpenAICompatibleProvider) Name() string  { return p.name }
func (p *OpenAICompatibleProvider) Model() string { return p.model }

func (p *OpenAICompatibleProvider) Generate(ctx context.Context, input GenerateInput) (GeneratedDraft, Usage, error) {
	input, err := NormalizeInput(input)
	if err != nil {
		return GeneratedDraft{}, Usage{}, err
	}
	if input.ContentRating == "" {
		input.ContentRating = "全年龄"
	}
	schema, err := json.MarshalIndent(worldDraftSchema, "", "  ")
	if err != nil {
		return GeneratedDraft{}, Usage{}, fmt.Errorf("encode world draft schema: %w", err)
	}
	prompt := fmt.Sprintf("主题：%s\n类型：%s\n内容分级：%s\n内容边界：%s\n\n请先确定一个正在发生的具体异常、一个玩家身份、一个倒计时或代价，再围绕它输出完整世界。不要先写百科背景。\n\n内容要求：\n- title 适合卡片展示，优先 8-18 个中文字符，具体、有画面，不使用‘一个关于……的故事’。\n- summary 40-120 字，推荐 45-75 字，最多两句：第一句写正在发生的事件，第二句写玩家要做的事或要承担的代价。\n- discovery.hook 12-60 字，只写最想让人点开的异常或两难；discovery.promise 16-80 字，说明玩家会追什么、改变什么或失去什么；playTime 和 format 用短标签。\n- worldBackground.rules 写 1-3 条可在故事中验证的规则，不能用万能解释；conflict 必须包含当前压力。\n- characters 生成 2-4 个公开角色，每个角色都要有欲望和与玩家的关系，不要只有性格形容词。\n- opening.text 120-320 字，推荐 160-260 字，必须用两个空行分成 2-3 段：第一段立刻进入行动，第二段出现人物/声音/线索，最后停在一个没有替玩家回答的问题上。每段只保留能改变理解或选择的句子，默认不使用比喻，不写连续的风、光、汗、心跳等通用氛围铺陈。\n- opening.actions 必须恰好 3 条，每条 8-24 字，分别代表追查、冒险或资源/关系取舍，不要写三个同义的‘继续调查’。\n- narrative.turnLength 默认使用‘短（220–360 字）’，让每一幕推进一个可验证事实。\n\n请输出一个 JSON 对象，字段结构必须严格遵循：\n%s", input.Topic, input.Type, input.ContentRating, input.Boundaries, schema)
	response, err := p.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: shared.ChatModel(p.model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.DeveloperMessage("你是AI 小说创作台的首发故事编辑与世界设定生成器。只返回一个合法的 JSON 对象，不要使用 markdown 代码围栏，不要包含任何解释文字。优先写可立即进入的冲突和玩家选择，而不是百科式世界观。所有字段必须与给定结构一致，保持虚构，不生成现实伤害指导。"),
			openai.UserMessage(prompt),
		},
		MaxCompletionTokens: openai.Int(4000),
	})
	if err != nil {
		return GeneratedDraft{}, Usage{}, fmt.Errorf("generate world draft: %w", err)
	}
	usage := Usage{InputTokens: response.Usage.PromptTokens, OutputTokens: response.Usage.CompletionTokens, TotalTokens: response.Usage.TotalTokens}
	if len(response.Choices) != 1 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		return GeneratedDraft{}, usage, errors.New("world draft provider returned no structured choice")
	}
	content := stripArtifactWrapper(response.Choices[0].Message.Content)
	var draft GeneratedDraft
	if err := json.Unmarshal(content, &draft); err != nil {
		if extracted := extractFencedJSON(content); extracted != nil {
			if nestedErr := json.Unmarshal(extracted, &draft); nestedErr == nil {
				draft = normalizeDraftRating(draft, input.ContentRating)
				if validateErr := draft.Validate(); validateErr != nil {
					return GeneratedDraft{}, usage, fmt.Errorf("validate generated world draft: %w", validateErr)
				}
				return draft, usage, nil
			}
		}
		return GeneratedDraft{}, usage, fmt.Errorf("decode world draft: %w", err)
	}
	draft = normalizeDraftRating(draft, input.ContentRating)
	if err := draft.Validate(); err != nil {
		return GeneratedDraft{}, usage, fmt.Errorf("validate generated world draft: %w", err)
	}
	return draft, usage, nil
}

func normalizeDraftRating(draft GeneratedDraft, requested string) GeneratedDraft {
	rating := strings.TrimSpace(draft.ContentRating)
	switch rating {
	case "全年龄", "13+", "16+", "18+":
		return draft
	}
	draft.ContentRating = requested
	return draft
}

var worldDraftSchema = map[string]any{
	"type": "object", "additionalProperties": false,
	"required": []string{"title", "summary", "type", "tags", "cover", "discovery", "worldBackground", "playerIdentity", "characters", "narrative", "opening", "contentRating", "contentBoundaries"},
	"properties": map[string]any{
		"title": stringSchema(), "summary": stringSchema(), "type": stringSchema(), "tags": arraySchema(stringSchema()), "cover": stringSchema(),
		"discovery":       objectSchema([]string{"hook", "promise", "playTime", "format"}, map[string]any{"hook": stringSchema(), "promise": stringSchema(), "playTime": stringSchema(), "format": stringSchema()}),
		"worldBackground": objectSchema([]string{"eraPlace", "rules", "conflict"}, map[string]any{"eraPlace": stringSchema(), "rules": stringSchema(), "conflict": stringSchema()}),
		"playerIdentity":  objectSchema([]string{"name", "background", "goal", "freedom"}, map[string]any{"name": stringSchema(), "background": stringSchema(), "goal": stringSchema(), "freedom": stringSchema()}),
		"characters":      arraySchema(objectSchema([]string{"name", "role", "personality", "goal", "relation", "public"}, map[string]any{"name": stringSchema(), "role": stringSchema(), "personality": stringSchema(), "goal": stringSchema(), "relation": stringSchema(), "public": map[string]any{"type": "boolean"}})),
		"narrative":       objectSchema([]string{"style", "pointOfView", "turnLength", "openness"}, map[string]any{"style": stringSchema(), "pointOfView": stringSchema(), "turnLength": stringSchema(), "openness": stringSchema()}),
		"opening":         objectSchema([]string{"text", "actions"}, map[string]any{"text": stringSchema(), "actions": arraySchema(stringSchema())}),
		"contentRating":   stringSchema(), "contentBoundaries": stringSchema(),
	},
}

func stringSchema() map[string]any         { return map[string]any{"type": "string"} }
func arraySchema(items any) map[string]any { return map[string]any{"type": "array", "items": items} }
func objectSchema(required []string, properties map[string]any) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": required, "properties": properties}
}

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

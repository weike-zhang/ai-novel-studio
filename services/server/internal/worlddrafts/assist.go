package worlddrafts

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

// AssistAction is either an inspiration request (empty current content) or a
// polish of the existing content.
type AssistAction string

const (
	AssistActionInspire AssistAction = "inspire"
	AssistActionPolish  AssistAction = "polish"
)

// AssistRequest describes one field-level AI assist call from the studio.
type AssistRequest struct {
	Action      AssistAction `json:"action"`
	Field       string       `json:"field"`
	Current     string       `json:"current"`
	ContextHint string       `json:"contextHint"`
}

// AssistResult is the single piece of text returned for the field.
type AssistResult struct {
	Text string `json:"text"`
}

// AssistGenerator performs synchronous single-field AI assist.
type AssistGenerator struct {
	client openai.Client
	model  string
}

func NewOpenAICompatibleAssistGenerator(baseURL, apiKey, model string, timeout time.Duration) (*AssistGenerator, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(apiKey) == "" || strings.TrimSpace(model) == "" {
		return nil, errors.New("assist generator configuration is incomplete")
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	client := openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL), option.WithHTTPClient(&http.Client{Timeout: timeout}))
	return &AssistGenerator{client: client, model: model}, nil
}

// NewOpenAICompatibleAssistGeneratorWithClient builds an assist generator
// around an existing OpenAI client, which may itself carry failover behaviour.
func NewOpenAICompatibleAssistGeneratorWithClient(model string, client openai.Client) (*AssistGenerator, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, errors.New("assist generator configuration is incomplete")
	}
	return &AssistGenerator{client: client, model: model}, nil
}

var assistFieldGuides = map[string]string{
	"title":            "世界标题：优先 8-18 个中文字符，具体、有画面感，不要使用‘一个关于……的故事’，不要带标点结尾",
	"summary":          "世界简介：40-120 字，推荐 45-75 字，最多两句；第一句写正在发生的事件，第二句写玩家要做的事或代价",
	"hook":             "首屏钩子：12-60 字，只写最想让人点开的异常或两难，不解释完整背景",
	"promise":          "玩家会做什么：16-80 字，说明玩家会追什么、改变什么或失去什么，不承诺固定结局",
	"opening":          "开场文本：120-320 字，推荐 160-260 字，使用 2-3 个短段；第一段立刻进入行动，第二段出现人物/声音/线索，最后停在没有替玩家回答的问题上。每句都要推进事件、线索、关系或选择，删掉不必要的氛围铺陈",
	"actions":          "首轮建议行动：恰好 3 条，每条 8-24 字，分别体现追查、冒险、关系或资源取舍，不要写同义的继续调查",
	"eraPlace":         "时代与地点：一句话交代时间、空间与整体氛围",
	"rules":            "核心规则：这个世界独有且能在故事中验证的运作法则，1-3 条，具体而不解释，不要写万能规则",
	"playerBackground": "玩家角色背景：一句话，交代身份、随身之物与处境",
}

func fieldGuide(field string) string {
	if guide, ok := assistFieldGuides[field]; ok {
		return guide
	}
	return "针对该创作字段给出符合世界观的内容"
}

// Generate returns either an inspiration or a polished version of a field.
func (g *AssistGenerator) Generate(ctx context.Context, req AssistRequest) (AssistResult, error) {
	var instruction string
	switch req.Action {
	case AssistActionInspire:
		instruction = "给出一个充满画面感的灵感和起点"
	case AssistActionPolish:
		instruction = "在不改变原意的前提下润色，让文字更有氛围和节奏"
	default:
		return AssistResult{}, fmt.Errorf("unsupported assist action %q", req.Action)
	}

	var user string
	if req.Action == AssistActionInspire {
		user = fmt.Sprintf("领域：%s\n%s。请直接给出内容，不要解释。", fieldGuide(req.Field), instruction)
		if hint := strings.TrimSpace(req.ContextHint); hint != "" {
			user += fmt.Sprintf("\n线索：%s", hint)
		}
	} else {
		user = fmt.Sprintf("领域：%s\n现有内容：\n%s\n%s。直接给出润色后的内容，不要解释。", fieldGuide(req.Field), req.Current, instruction)
	}

	response, err := g.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: shared.ChatModel(g.model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.DeveloperMessage("你是AI 小说创作台的创作助手。你只负责打磨和生发一个具体创作字段，输出直接可用，不添加解释、不写标题前缀、不套引号。保持虚构，不生成现实伤害指导。"),
			openai.UserMessage(user),
		},
		MaxCompletionTokens: openai.Int(400),
	})
	if err != nil {
		return AssistResult{}, fmt.Errorf("assist generate: %w", err)
	}
	if len(response.Choices) != 1 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		return AssistResult{}, errors.New("assist provider returned no content")
	}
	return AssistResult{Text: strings.TrimSpace(response.Choices[0].Message.Content)}, nil
}

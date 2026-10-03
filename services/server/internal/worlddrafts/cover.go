package worlddrafts

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"strings"
	"time"

	"github.com/weike-zhang/ai-novel-studio/services/server/internal/media"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// CoverGenerator renders a 16:9 cover image for a world using an image model.
// The cover includes the title text rendered by the model.
type CoverGenerator interface {
	Generate(ctx context.Context, title, summary, genre string) ([]byte, error)
}

type OpenAICompatibleCoverGenerator struct {
	client openai.Client
	model  string
}

func NewOpenAICompatibleCoverGenerator(baseURL, apiKey, model string, timeout time.Duration) (*OpenAICompatibleCoverGenerator, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || apiKey == "" || model == "" {
		return nil, errors.New("cover generator configuration is incomplete")
	}
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	client := openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL), option.WithHTTPClient(&http.Client{Timeout: timeout}))
	return &OpenAICompatibleCoverGenerator{client: client, model: model}, nil
}

// NewOpenAICompatibleCoverGeneratorWithClient builds a cover generator around
// an existing OpenAI client, which may itself carry failover behaviour.
func NewOpenAICompatibleCoverGeneratorWithClient(model string, client openai.Client) (*OpenAICompatibleCoverGenerator, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, errors.New("cover generator configuration is incomplete")
	}
	return &OpenAICompatibleCoverGenerator{client: client, model: model}, nil
}

func (g *OpenAICompatibleCoverGenerator) Generate(ctx context.Context, title, summary, genre string) ([]byte, error) {
	prompt := buildCoverPrompt(title, summary, genre)
	response, err := g.client.Images.Generate(ctx, openai.ImageGenerateParams{
		Model:  openai.ImageModel(g.model),
		Prompt: prompt,
		// 1792x1024 is the closest supported landscape size to 16:9.
		Size: openai.ImageGenerateParamsSize1792x1024,
	})
	if err != nil {
		return nil, fmt.Errorf("generate cover: %w", err)
	}
	if len(response.Data) == 0 {
		return nil, errors.New("cover generator returned no image")
	}
	image := response.Data[0]
	if image.B64JSON != "" {
		raw, err := base64.StdEncoding.DecodeString(image.B64JSON)
		if err != nil {
			return nil, fmt.Errorf("decode cover base64: %w", err)
		}
		return raw, nil
	}
	if image.URL != "" {
		// URLs expire quickly; prefer b64_json, but fall back to fetching the URL.
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, image.URL, nil)
		if err != nil {
			return nil, fmt.Errorf("cover url request: %w", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch cover url: %w", err)
		}
		defer resp.Body.Close()
		buf := make([]byte, 0, 1<<20)
		chunk := make([]byte, 64<<10)
		for {
			n, err := resp.Body.Read(chunk)
			buf = append(buf, chunk[:n]...)
			if err != nil {
				break
			}
		}
		if resp.StatusCode != http.StatusOK || len(buf) == 0 {
			return nil, fmt.Errorf("fetch cover url status=%d", resp.StatusCode)
		}
		return buf, nil
	}
	return nil, errors.New("cover generator returned no image data")
}

// SaveCover uploads a cover image to the media bucket and returns the public
// URL. The filename is the world id so regenerating overwrites the old cover.
// The generated image is re-encoded as a JPEG (no resize) so the stored cover
// stays small for the homepage.
func SaveCover(ctx context.Context, mediaClient *media.Client, worldID string, data []byte) (string, error) {
	if mediaClient == nil {
		return "", errors.New("media client is not configured")
	}
	encoded, err := encodeCoverJPEG(data)
	if err != nil {
		return "", err
	}
	key := "covers/" + worldID + ".jpg"
	url, err := mediaClient.Upload(ctx, key, encoded, "image/jpeg")
	if err != nil {
		return "", err
	}
	return url, nil
}

// encodeCoverJPEG decodes the source image and re-encodes it as a JPEG. It
// keeps the original resolution (no downscaling) so cover clarity is preserved.
func encodeCoverJPEG(data []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode cover image: %w", err)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		return nil, fmt.Errorf("encode cover jpeg: %w", err)
	}
	return buf.Bytes(), nil
}

// buildCoverPrompt turns a world's title, summary and genre into a cinematic
// poster prompt. The model renders the title text into the cover.
func buildCoverPrompt(title, summary, genre string) string {
	return fmt.Sprintf(
		"这是小说标题：%s，小说简介：%s，题材：%s，需要生成16:9的横版封面，封面上带小说标题文字。\n\n"+
			"先理解标题、简介与题材，自主判断小说的时代背景、核心人物、关键冲突、情绪氛围和最有辨识度的视觉元素，把标题用醒目、美观的字体自然地融入画面，标题文字清晰准确，不要有错别字。\n\n"+
			"构图采用横版16:9，主体明确，层次丰富，具有电影海报级灯光、景深、环境细节和高级配色，视觉冲击力强，适合作为网页小说卡片封面。避免廉价网游风、素材拼贴感、重复人物、肢体错误、杂乱构图。",
		title, summary, genre,
	)
}

package worlddrafts

import (
	"context"
	"testing"
)

func TestFakeProviderGenerate(t *testing.T) {
	provider := FakeProvider{}
	if provider.Name() != "fake" || provider.Model() == "" {
		t.Fatalf("provider identity = %q/%q", provider.Name(), provider.Model())
	}
	draft, usage, err := provider.Generate(context.Background(), GenerateInput{Topic: "雾灯港", Type: "推理", ContentRating: "16+", Boundaries: "无现实伤害指导"})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if err := draft.Validate(); err != nil {
		t.Fatalf("generated draft invalid: %v", err)
	}
	if usage.TotalTokens == 0 {
		t.Fatal("usage missing")
	}
}

func TestExtractFencedJSON(t *testing.T) {
	raw := []byte("```json\n{\"title\":\"雾灯港\"}\n```")
	got := extractFencedJSON(raw)
	if string(got) != `{"title":"雾灯港"}` {
		t.Fatalf("extractFencedJSON = %q", got)
	}
	if extractFencedJSON([]byte("no fence here")) != nil {
		t.Fatal("expected nil for unfenced content")
	}
}

func TestStripArtifactWrapper(t *testing.T) {
	raw := ":::writing\n```json\n{\"title\":\"雾灯港\"}\n```\n:::"
	got := stripArtifactWrapper(raw)
	if string(got) != "```json\n{\"title\":\"雾灯港\"}\n```" {
		t.Fatalf("stripArtifactWrapper = %q", got)
	}
}

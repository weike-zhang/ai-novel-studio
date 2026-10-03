package worlddrafts

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func TestBuildCoverPromptContainsWorldInfo(t *testing.T) {
	prompt := buildCoverPrompt("雾灯港：这座城每晚忘掉一个人，今晚是我", "一座靠潮汐记忆运转的港城", "悬疑")
	if !strings.Contains(prompt, "雾灯港：这座城每晚忘掉一个人，今晚是我") {
		t.Fatal("prompt should contain title")
	}
	if !strings.Contains(prompt, "悬疑") {
		t.Fatal("prompt should contain genre")
	}
	if !strings.Contains(prompt, "16:9") {
		t.Fatal("prompt should ask for a 16:9 landscape cover")
	}
	if !strings.Contains(prompt, "封面上带小说标题文字") {
		t.Fatal("prompt should ask the model to render the title")
	}
	if strings.Contains(prompt, "不要出现任何文字") {
		t.Fatal("prompt must not forbid text since the title is rendered on the cover")
	}
}

func TestEncodeCoverJPEGReEncodes(t *testing.T) {
	// A tiny 4x4 PNG; encodeCoverJPEG must decode it and emit JPEG bytes.
	var src bytes.Buffer
	if err := png.Encode(&src, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	out, err := encodeCoverJPEG(src.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 4 || out[0] != 0xFF || out[1] != 0xD8 {
		t.Fatalf("output is not a JPEG: %x", out[:4])
	}
	if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
		t.Fatalf("decoding encoded jpeg: %v", err)
	}
}

func TestEncodeCoverJPEGRejectsGarbage(t *testing.T) {
	if _, err := encodeCoverJPEG([]byte("not an image")); err == nil {
		t.Fatal("expected an error for non-image input")
	}
}

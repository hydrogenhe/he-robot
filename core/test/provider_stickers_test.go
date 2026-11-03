package core_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"gpt-robot/core"
	"gpt-robot/model"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type providerStickerGenerator struct {
	t        *testing.T
	provider string
	switchTo func()
	calls    int
}

func (g *providerStickerGenerator) Generate(_ context.Context, prompt *model.ReplyPrompt) (*model.GeneratedReply, error) {
	g.calls++
	selected := ""
	for _, item := range prompt.Stickers {
		if strings.HasPrefix(item.Label, "curated-") {
			if item.Label != "curated-"+g.provider {
				g.t.Fatalf("wrong provider supplied: %s", item.Label)
			}
			selected = item.ID
		}
	}
	if selected == "" {
		g.t.Fatal("provider collection missing from real reply prompt")
	}
	if g.switchTo != nil {
		g.switchTo()
	}
	usage := model.TokenUsage{Provider: g.provider, Model: "fake", RequestID: model.ID(), Input: 100, Output: 20}
	result := model.GeneratedReply{Text: "开心呀", Messages: []string{"开心呀"}, StickerID: selected, Usage: &usage}
	return &result, nil
}

func writeProviderFixture(t *testing.T, s *core.Service, data []byte) {
	t.Helper()
	sum := sha256.Sum256(data)
	id := hex.EncodeToString(sum[:])
	for _, provider := range []string{"codex", "deepseek"} {
		folder := filepath.Join(s.Store.Root, "assets/images/stickers/providers", provider)
		if err := os.MkdirAll(folder, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, id+".image"), data, 0600); err != nil {
			t.Fatal(err)
		}
		item := model.LearnedSticker{
			ID:        id,
			Label:     "curated-" + provider,
			Image:     model.ImageAttachment{URL: "https://example.com/public.png", Width: 8, Height: 8, Format: "png"},
			SourceKey: "https://example.com/source",
		}
		library := struct {
			Items []model.LearnedSticker `json:"items"`
		}{Items: []model.LearnedSticker{item}}
		raw, err := json.Marshal(&library)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, "catalog.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProviderStickerRoutesWithoutPrivateLearning(t *testing.T) {
	for _, provider := range []string{"codex", "deepseek"} {
		t.Run(provider, func(t *testing.T) {
			s, f, _ := expressionService(t)
			writeProviderFixture(t, s, f.data)
			settings := model.ModelProviderInput{Mode: "local", Provider: provider, CodexModel: "chosen", CodexEffort: "low", DeepSeekModel: "deepseek-flash", HarnessEffort: "low"}
			if _, err := s.SaveModelProviderConfig(ctx, &settings); err != nil {
				t.Fatal(err)
			}
			generator := &providerStickerGenerator{t: t, provider: provider}
			s.Generator = generator
			f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "发个表情包")}
			if err := s.PrivateTick(ctx); err != nil {
				t.Fatal(err)
			}
			if f.images != 1 || generator.calls != 1 || f.downloads != 0 {
				t.Fatal("expected one local image and one generation", f.images, generator.calls, f.downloads)
			}
			status, err := s.PrivateOverview(ctx)
			if err != nil || status.Config.MemoryEnabled {
				t.Fatal("public collection unexpectedly requires private learning", err)
			}
		})
	}
}

func TestProviderSwitchDuringGenerationRejectsOldCharacter(t *testing.T) {
	s, f, _ := expressionService(t)
	writeProviderFixture(t, s, f.data)
	generator := &providerStickerGenerator{t: t, provider: "codex"}
	generator.switchTo = func() {
		input := model.ModelProviderInput{Mode: "local", Provider: "deepseek", DeepSeekModel: "deepseek-flash", HarnessEffort: "low"}
		if _, err := s.SaveModelProviderConfig(ctx, &input); err != nil {
			t.Fatal(err)
		}
	}
	s.Generator = generator
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "发个表情包")}
	if err := s.PrivateTick(ctx); err == nil {
		t.Fatal("sent stale provider sticker")
	}
	if f.images != 0 || f.attempts != 0 {
		t.Fatal("old character sent after provider switch")
	}
}

func TestCorruptProviderStickerNeverUploads(t *testing.T) {
	s, f, _ := expressionService(t)
	writeProviderFixture(t, s, f.data)
	sum := sha256.Sum256(f.data)
	name := filepath.Join(s.Store.Root, "assets/images/stickers/providers/codex", hex.EncodeToString(sum[:])+".image")
	if err := os.WriteFile(name, []byte("corrupted image"), 0600); err != nil {
		t.Fatal(err)
	}
	s.Generator = &providerStickerGenerator{t: t, provider: "codex"}
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "发个表情包")}
	if err := s.PrivateTick(ctx); err == nil {
		t.Fatal("corrupt local image accepted")
	}
	if f.images != 0 || f.attempts != 0 {
		t.Fatal("corrupt asset reached platform")
	}
}

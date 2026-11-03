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
	t             *testing.T
	provider      string
	switchTo      func()
	calls         int
	only          bool
	unsuitable    bool
	noMatch       bool
	selection     string
	failSelection bool
}

func (g *providerStickerGenerator) Generate(_ context.Context, prompt *model.ReplyPrompt) (*model.GeneratedReply, error) {
	g.calls++
	usage := model.TokenUsage{Provider: g.provider, Model: "fake", RequestID: model.ID(), Input: 100, Output: 20}
	if !prompt.SelectSticker {
		if !prompt.AnalyzeSticker || len(prompt.Stickers) > 0 {
			g.t.Fatal("first round must analyze without choosing images")
		}
		tags := []string{"开心"}
		if g.noMatch {
			tags = []string{"道歉"}
		}
		intent := model.StickerIntent{Suitable: !g.unsuitable, ImageOnly: g.only, Tags: tags, Scene: "闲聊"}
		result := model.GeneratedReply{Text: "开心呀", Messages: []string{"开心呀"}, StickerIntent: &intent, Usage: &usage}
		return &result, nil
	}
	if prompt.Conversation != nil || prompt.AllowRecall || prompt.Draft != "开心呀" || prompt.StickerIntent == nil {
		g.t.Fatal("selection leaked conversation/actions or lost context")
	}
	if g.failSelection {
		return &model.GeneratedReply{Usage: &usage}, model.Fail("AI_REPLY", "模拟选图失败")
	}
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
	if g.selection != "" {
		selected = g.selection
	}
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
			StickerMeaning: model.StickerMeaning{Tags: []string{"开心"}, Description: "开心的角色", Avoid: "严肃求助"},
			ID:             id,
			Label:          "curated-" + provider,
			Image:          model.ImageAttachment{URL: "https://example.com/public.png", Width: 8, Height: 8, Format: "png"},
			SourceKey:      "https://example.com/source",
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
			if f.images != 1 || generator.calls != 2 || f.downloads != 0 {
				t.Fatal("expected one local image and two generations", f.images, generator.calls, f.downloads)
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
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if f.images != 0 || f.attempts != 1 {
		t.Fatal("corrupt asset must fall back to text")
	}
}

func TestStickerModesAndFallback(t *testing.T) {
	cases := []struct {
		name                            string
		only, unsuitable, noMatch, fail bool
		selection                       string
		draw                            int
		calls, texts, images            int
	}{
		{name: "image-only", only: true, calls: 2, images: 1},
		{name: "text-and-image", calls: 2, texts: 1, images: 1},
		{name: "random-text", draw: 99, calls: 1, texts: 1},
		{name: "unsuitable", unsuitable: true, calls: 1, texts: 1},
		{name: "no-candidate", noMatch: true, calls: 1, texts: 1},
		{name: "invalid-id", only: true, selection: "invented", calls: 2, texts: 1},
		{name: "no-selection", only: true, selection: "none", calls: 2, texts: 1},
		{name: "selection-failed", only: true, fail: true, calls: 2, texts: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, f, _ := expressionService(t)
			writeProviderFixture(t, s, f.data)
			g := &providerStickerGenerator{t: t, provider: "codex", only: tc.only, unsuitable: tc.unsuitable, noMatch: tc.noMatch, selection: tc.selection, failSelection: tc.fail}
			s.Generator = g
			s.ExpressionDraw = func(n int) int {
				if n == 100 {
					return tc.draw
				}
				return 0
			}
			f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "今天很开心")}
			if err := s.PrivateTick(ctx); err != nil {
				t.Fatal(err)
			}
			if g.calls != tc.calls || f.attempts != tc.texts || f.images != tc.images {
				t.Fatalf("calls/texts/images=%d/%d/%d", g.calls, f.attempts, f.images)
			}
			state, err := s.PrivateOverview(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(state.State.Outbox) != tc.texts+tc.images {
				t.Fatal("wrong delivery ledger")
			}
			for _, out := range state.State.Outbox {
				if out.Status != "sent" {
					t.Fatal("missing sent receipt")
				}
			}
		})
	}
}

func TestAllRobotInstancesShareProviderCollection(t *testing.T) {
	for _, provider := range []string{"codex", "deepseek"} {
		t.Run(provider, func(t *testing.T) {
			primary, f, _ := expressionService(t)
			writeProviderFixture(t, primary, f.data)
			// 与工作台一致：001在根目录，002拥有独立状态，但没有自己的图库。
			instanceRoot := filepath.Join(primary.Store.Root, "data/instances/002")
			instance := core.NewService(instanceRoot, f)
			if err := instance.Initialize(ctx); err != nil {
				t.Fatal(err)
			}
			credentials := model.Credentials{UID: "123", Cookies: map[string]string{}}
			if err := instance.Store.Write(ctx, ".private/bilibili.json", &credentials); err != nil {
				t.Fatal(err)
			}
			permissions := model.DefaultPermissions()
			permissions.Enabled = true
			permissions.AllowedVideos = []string{bvid}
			if _, err := instance.SavePermissions(ctx, &permissions); err != nil {
				t.Fatal(err)
			}
			settings := model.AutoReplyConfig{Enabled: true, AccountUID: "123", PriorityUID: "789", Videos: []string{bvid}, IntervalSeconds: 300, OtherDailyLimit: 20, PerUserDailyLimit: 3}
			if _, err := instance.SaveAutoConfig(ctx, &settings); err != nil {
				t.Fatal(err)
			}
			private := model.PrivateSettings{Enabled: true, AllowSend: true, AutoReply: true, IntervalSeconds: 120}
			if _, err := instance.SavePrivateConfig(ctx, &private); err != nil {
				t.Fatal(err)
			}
			modelInput := model.ModelProviderInput{Mode: "local", Provider: provider, DeepSeekModel: "deepseek-flash", HarnessEffort: "low"}
			if _, err := instance.SaveModelProviderConfig(ctx, &modelInput); err != nil {
				t.Fatal(err)
			}
			generator := &providerStickerGenerator{t: t, provider: provider, only: true}
			instance.Generator = generator
			instance.ExpressionDraw = func(int) int { return 0 }
			if err := instance.PrivateTick(ctx); err != nil {
				t.Fatal(err)
			}
			f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "发个表情包")}
			if err := instance.PrivateTick(ctx); err != nil {
				t.Fatal(err)
			}
			if generator.calls != 2 || f.images != 1 || f.attempts != 0 {
				t.Fatal("instance failed to use shared collection", generator.calls, f.images, f.attempts)
			}
			if _, err := os.Stat(filepath.Join(instanceRoot, "assets")); !os.IsNotExist(err) {
				t.Fatal("unexpected per-robot collection", err)
			}
		})
	}
}

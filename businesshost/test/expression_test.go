package businesshost_test

import (
	"context"
	"encoding/json"
	"gpt-robot/businesshost"
	"gpt-robot/model"
	"strings"
	"testing"
)

type expressionBackend struct {
	calls    int
	text     string
	captured model.ReplyPrompt
}

func (b *expressionBackend) GenerateJSON(_ context.Context, _ string, body []byte) (*model.GeneratedReply, error) {
	b.calls++
	json.Unmarshal(body, &b.captured)
	return &model.GeneratedReply{Text: b.text}, nil
}

func TestExpressionOneGenerationReturnsTextPartsAndAllowlistedSticker(t *testing.T) {
	backend := &expressionBackend{text: `{"messages":["哈哈～","这个有点可爱"],"sticker":"known"}`}
	g := businesshost.NewGenerator(t.TempDir(), backend)
	prompt := model.ReplyPrompt{Channel: "private-message", Text: "看这个呀", ReplyParts: 3, Stickers: []model.StickerChoice{{ID: "known", Label: "主人教的表情"}}}
	result, err := g.Generate(context.Background(), &prompt)
	if err != nil || backend.calls != 1 || len(result.Messages) != 2 || result.StickerID != "known" || backend.captured.ReplyParts != 3 {
		t.Fatal(result, err, backend.calls)
	}
	backend.text = `{"reply":"你好","sticker":"https://evil.example/image.png"}`
	result, err = g.Generate(context.Background(), &prompt)
	if err != nil || result.StickerID != "" || backend.calls != 2 {
		t.Fatal("unlisted sticker selected or generation repeated", result, err)
	}
	backend.text = `{"messages":["第一条","第二条","第三条","第四条"]}`
	if _, err = g.Generate(context.Background(), &prompt); err == nil || backend.calls != 3 {
		t.Fatal("too many parts accepted or retried")
	}
	prompt.Channel = "video-comment"
	if _, err = g.Generate(context.Background(), &prompt); err == nil || backend.calls != 3 {
		t.Fatal("private sticker catalog sent to public generator")
	}
}

func TestStickerAnalysisAndSelectionProtocols(t *testing.T) {
	backend := &expressionBackend{text: `{"reply":"抱抱你","sticker":"ignored","stickerIntent":{"suitable":true,"imageOnly":true,"tags":["安慰"],"scene":"闲聊"}}`}
	generator := businesshost.NewGenerator(t.TempDir(), backend)
	prompt := model.ReplyPrompt{
		Channel:        "private-message",
		Text:           "想要一个抱抱",
		AnalyzeSticker: true,
		ReplyParts:     3,
	}
	first, err := generator.Generate(context.Background(), &prompt)
	if err != nil || first.StickerIntent == nil || !first.StickerIntent.ImageOnly || first.StickerID != "" || first.Text != "抱抱你" {
		t.Fatal(first, err)
	}
	meaning := model.StickerMeaning{Tags: []string{"安慰"}, Description: "角色张开双臂", Caption: "抱抱", Avoid: "拒绝亲密时", Direction: "机器人安慰用户"}
	choice := model.StickerChoice{ID: "known", Label: "拥抱", StickerMeaning: meaning}
	prompt = model.ReplyPrompt{
		Channel:       "private-message",
		Text:          "想要一个抱抱",
		SelectSticker: true,
		Draft:         first.Text,
		StickerIntent: first.StickerIntent,
		Stickers:      []model.StickerChoice{choice},
	}
	backend.text = `{"sticker":"known","reply":"不能替换正文","actions":[{"action":"save_content"}],"remember":["不应记住"],"recall":"不应回忆"}`
	selected, err := generator.Generate(context.Background(), &prompt)
	if err != nil || selected.StickerID != "known" || selected.Text != "" || len(selected.Actions) > 0 || len(selected.Remember) > 0 || selected.RecallQuery != "" {
		t.Fatal(selected, err)
	}
	if backend.captured.Stickers[0].Avoid != meaning.Avoid || backend.captured.Draft != first.Text {
		t.Fatal("metadata lost")
	}
	for _, body := range []string{`{"sticker":"invented"}`, `{"sticker":""}`} {
		backend.text = body
		selected, err = generator.Generate(context.Background(), &prompt)
		if err != nil || selected.StickerID != "" {
			t.Fatal("invalid or empty choice accepted", selected, err)
		}
	}
	prompt.Channel = "video-comment"
	if _, err = generator.Generate(context.Background(), &prompt); err == nil {
		t.Fatal("private selection allowed in public channel")
	}
}

func TestStickerAnalysisRespectsCombinedInstructionBudget(t *testing.T) {
	backend := &expressionBackend{text: `{"reply":"你好"}`}
	generator := businesshost.NewGenerator(t.TempDir(), backend)
	prompt := model.ReplyPrompt{
		Channel:        "private-message",
		Text:           strings.Repeat("问", 500),
		Persona:        strings.Repeat("人", 800),
		State:          strings.Repeat("静", 240),
		Development:    strings.Repeat("长", 240),
		AnalyzeSticker: true,
		ReplyParts:     3,
		Owner:          true,
		AllowRecall:    true,
		Conversation:   &model.ConversationScope{},
	}
	for range 6 {
		prompt.Recent = append(prompt.Recent, strings.Repeat("聊", 120))
	}
	result, err := generator.Generate(context.Background(), &prompt)
	if err != nil || result.Text != "你好" || backend.calls != 1 {
		t.Fatal(result, err)
	}
	if backend.captured.Text != prompt.Text || !backend.captured.AnalyzeSticker {
		t.Fatal("lost current message or semantic task")
	}
}

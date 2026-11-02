package businesshost_test

import (
	"context"
	"encoding/json"
	"gpt-robot/businesshost"
	"gpt-robot/model"
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

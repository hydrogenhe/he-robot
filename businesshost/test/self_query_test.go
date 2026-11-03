package businesshost_test

import (
	"context"
	"gpt-robot/businesshost"
	"gpt-robot/model"
	"testing"
)

func TestGeneratorSelfQueryProtocolAndPrivateScope(t *testing.T) {
	backend := &expressionBackend{text: `{"reply":"我先查一下。","self":{"action":"query_self","resource":"diary","date":"today","full":true}}`}
	generator := businesshost.NewGenerator(t.TempDir(), backend)
	prompt := model.ReplyPrompt{
		Channel:          "private-message",
		Owner:            true,
		Text:             "把那篇给我看看",
		SelfCapabilities: []string{"diary", "memories", "experiences", "growth", "activities", "status", "write_diary"},
		SelfToday:        "2026-11-03",
	}
	result, err := generator.Generate(context.Background(), &prompt)
	if err != nil || result.SelfAction == nil || !result.SelfAction.Full || result.SelfAction.Resource != "diary" {
		t.Fatal(result, err)
	}
	prompt.Owner = false
	if _, err = generator.Generate(context.Background(), &prompt); err == nil {
		t.Fatal("nonowner requested self query")
	}
	prompt.Owner = true
	prompt.SelfResult = &model.SelfQueryResult{
		Resource: "diary",
		Records:  []model.SelfRecord{},
	}
	if _, err = generator.Generate(context.Background(), &prompt); err == nil {
		t.Fatal("self query loop accepted")
	}
	prompt.Channel = "video-comment"
	if _, err = generator.Generate(context.Background(), &prompt); err == nil {
		t.Fatal("private query data admitted to public prompt")
	}
}

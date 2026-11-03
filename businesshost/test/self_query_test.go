package businesshost_test

import (
	"context"
	"gpt-robot/businesshost"
	"gpt-robot/model"
	"strings"
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

func TestCompanionProtocolIsOwnerPrivateOnly(t *testing.T) {
	backend := &expressionBackend{text: `{"reply":"明白。","companion":{"quote":"我在忙","busyMinutes":120}}`}
	generator := businesshost.NewGenerator(t.TempDir(), backend)
	prompt := model.ReplyPrompt{Channel: "private-message", Owner: true, Text: "我在忙", SelfToday: "2026-11-03", SelfCapabilities: []string{"diary"}}
	result, err := generator.Generate(context.Background(), &prompt)
	if err != nil || result.Companion == nil || result.Companion.BusyMinutes == nil || *result.Companion.BusyMinutes != 120 {
		t.Fatal(result, err)
	}
	prompt.Owner = false
	result, err = generator.Generate(context.Background(), &prompt)
	if err != nil || result.Companion != nil {
		t.Fatal("nonowner companion cue", result, err)
	}
}

func TestCompanionContextFitsOwnerPromptBudget(t *testing.T) {
	backend := &expressionBackend{text: `{"reply":"明白啦。"}`}
	generator := businesshost.NewGenerator(t.TempDir(), backend)
	prompt := model.ReplyPrompt{Channel: "private-message", Owner: true, Text: "我明天有面试", Persona: strings.Repeat("人设", 400), SelfToday: "2026-11-03", SelfCapabilities: []string{"diary", "memories", "experiences", "growth", "activities", "status", "write_diary"}, CompanionContext: strings.Repeat("实际跟进资料", 100), ReplyParts: 3, Development: strings.Repeat("成长", 120), InterestCandidates: []string{"狗"}, Recent: []string{strings.Repeat("聊", 120), strings.Repeat("聊", 120), strings.Repeat("聊", 120), strings.Repeat("聊", 120)}, PrivateMemories: []model.RecalledMemory{{Quote: strings.Repeat("记忆", 80)}}}
	if _, err := generator.Generate(context.Background(), &prompt); err != nil {
		t.Fatal("owner context rejected", err)
	}
}

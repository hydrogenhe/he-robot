package model_test

import (
	"encoding/json"
	"gpt-robot/model"
	"strings"
	"testing"
)

func TestReplyBudgetIncludesEscapingMetadataAndPreservesCurrentMessage(t *testing.T) {
	text := strings.Repeat("\x00", 500)
	input := model.ReplyPrompt{Channel: "private-message", Text: text, Persona: strings.Repeat("🐉", 20000), State: strings.Repeat("情绪", 1000)}
	for i := 0; i < 1000; i++ {
		input.Recent = append(input.Recent, strings.Repeat("旧", 300))
		memory := model.RecalledMemory{Quote: strings.Repeat("记", 500), OccurredAt: strings.Repeat("日", 1000), MessageKey: strings.Repeat("号", 1000)}
		input.PrivateMemories = append(input.PrivateMemories, memory)
	}
	result, err := model.BoundReplyPrompt(&input)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(result)
	if len(body) > model.ReplyContextMaxBytes || result.Text != text || len(result.PrivateMemories) > 3 {
		t.Fatal("budget violated", len(body))
	}
	if len(input.Recent) != 1000 || len(input.PrivateMemories) != 1000 || len([]rune(input.Persona)) != 20000 {
		t.Fatal("mutated source")
	}
	again, err := model.BoundReplyPrompt(result)
	after, _ := json.Marshal(again)
	if err != nil || string(body) != string(after) {
		t.Fatal("budget differs between business and host boundaries")
	}
}
func TestPublicContextRejectsPrivateHistoryAndUnknownChannels(t *testing.T) {
	inputs := []model.ReplyPrompt{
		{Channel: "video-comment", Text: "你好", Recent: []string{"私聊内容"}},
		{Channel: "video-comment", Text: "你好", PrivateMemories: []model.RecalledMemory{{Quote: "私聊事实"}}},
		{Channel: "unknown", Text: "你好"},
	}
	for _, input := range inputs {
		if _, err := model.BoundReplyPrompt(&input); err == nil {
			t.Fatal("unscoped context accepted")
		}
	}
}

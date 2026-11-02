package core_test

import (
	"context"
	"gpt-robot/model"
	"testing"
)

type recallGenerator struct {
	prompts []model.ReplyPrompt
	unknown bool
	over    bool
}

func (g *recallGenerator) Generate(_ context.Context, p *model.ReplyPrompt) (*model.GeneratedReply, error) {
	g.prompts = append(g.prompts, *p)
	usage := &model.TokenUsage{Provider: "codex", Model: "test-model", RequestID: model.ID(), Input: 100, Output: 23}
	if g.unknown {
		usage = nil
	}
	if g.over {
		usage.Input = 10000
	}
	result := &model.GeneratedReply{Text: "这次没有找到足够明确的记录。", RecallQuery: "龟背竹", Usage: usage}
	if p.RecallComplete {
		result.Text = "记得那份龟背竹资料，叶子的孔洞很特别。"
	}
	return result, nil
}
func TestRecallBoundedSecondCallAndUnknownUsageStops(t *testing.T) {
	for _, mode := range []string{"known", "unknown", "over"} {
		t.Run(mode, func(t *testing.T) {
			s, f, _ := setupAuto(t)
			if err := s.AutoTick(ctx); err != nil {
				t.Fatal(err)
			}
			source, err := s.Store.Event(ctx, "read", "公开的龟背竹资料", nil)
			if err != nil {
				t.Fatal(err)
			}
			note := model.NoteInput{Kind: "memory", Title: "龟背竹", Text: "资料描述龟背竹叶片上的孔洞", Sources: []string{source.ID}}
			if _, err = s.Note(ctx, &note); err != nil {
				t.Fatal(err)
			}
			generator := &recallGenerator{unknown: mode == "unknown", over: mode == "over"}
			s.Generator = generator
			f.roots = []model.Comment{autoComment("8899", "789", "之前那份资料你还记得吗")}
			if err = s.AutoTick(ctx); err != nil {
				t.Fatal(err)
			}
			expected := 1
			if mode == "known" {
				expected = 2
			}
			if len(generator.prompts) != expected {
				t.Fatalf("calls=%d want=%d", len(generator.prompts), expected)
			}
			if expected == 2 {
				last := generator.prompts[1]
				if last.AllowRecall || !last.RecallComplete || len(last.PublicMemories) != 1 || len(last.PrivateMemories) != 0 {
					t.Fatal("recall budget or scope violated", last)
				}
			}
		})
	}
}

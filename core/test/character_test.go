package core_test

import (
	"gpt-robot/core"
	"gpt-robot/model"
	"strings"
	"testing"
	"time"
)

func TestActualInteractionsChangeMoodSlowTraitsFeedNextReplyAndPersist(t *testing.T) {
	s, f, _ := setupAuto(t)
	generator := s.Generator.(*fakeGenerator)
	generator.signal = &model.CharacterSignal{Mood: "happy", Intensity: 60, Warmth: 1, Curiosity: 0, Playfulness: -1}
	if err := s.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := s.CharacterStatus(ctx)
	if err != nil || before.Experiences != 0 {
		t.Fatal(before, err)
	}
	for i := range 5 {
		f.roots = append([]model.Comment{autoComment(model.Number(i+2001), "789", "这条新互动很开心")}, f.roots...)
		if err := s.AutoTick(ctx); err != nil {
			t.Fatal(err)
		}
		state, err := s.CharacterStatus(ctx)
		if err != nil || state.Experiences != i+1 || state.Mood != "happy" {
			t.Fatal(state, err)
		}
		if i < 4 && state.Traits.Warmth != 50 {
			t.Fatal("personality changed too fast", state)
		}
		// 仅回拨临时发送账本以越过测试冷却，不等待真实时间，不调用外部服务。
		drafts, err := s.Drafts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for j := range drafts {
			drafts[j].AttemptAt = time.Now().Add(-3 * time.Minute).UTC().Format(time.RFC3339Nano)
		}
		if err := s.Store.Write(ctx, "data/events/drafts.json", drafts); err != nil {
			t.Fatal(err)
		}
	}
	state, err := s.CharacterStatus(ctx)
	if err != nil || state.Traits.Warmth != 51 || state.Traits.Playfulness != 49 || generator.count() != 5 {
		t.Fatal(state, err)
	}
	if !strings.Contains(generator.prompts[1].State, "开心") {
		t.Fatal("new mood absent from next prompt")
	}
	restored := core.NewService(s.Store.Root, f)
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := restored.CharacterStatus(ctx)
	if err != nil || status.Experiences != 5 || status.Traits.Warmth != 51 {
		t.Fatal("growth lost on restart", status, err)
	}
	if err := restored.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	again, _ := restored.CharacterStatus(ctx)
	if again.Experiences != 5 {
		t.Fatal("same interactions counted twice")
	}
}
func TestPrivateGrowthHasNoMessageBodyAndMoodSettlesWithoutModel(t *testing.T) {
	s, f, generator := setupPrivate(t)
	generator.signal = &model.CharacterSignal{Mood: "curious", Intensity: 40, Curiosity: 1}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "这是不能进入公开状态的私密正文")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	data, err := s.Store.Text(ctx, "data/character/state.json")
	if err != nil || strings.Contains(string(data), "私密正文") {
		t.Fatal("private body leaked", err)
	}
	state, err := s.CharacterStatus(ctx)
	if err != nil || state.Experiences != 1 || state.Mood != "curious" {
		t.Fatal(state, err)
	}
	state.UpdatedAt = time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339Nano)
	if err := s.Store.Write(ctx, "data/character/state.json", state); err != nil {
		t.Fatal(err)
	}
	restored := core.NewService(s.Store.Root, f)
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	settled, err := restored.CharacterStatus(ctx)
	if err != nil || settled.Mood != "calm" || settled.Intensity != 0 || generator.count() != 1 {
		t.Fatal("mood did not settle without AI", settled, err)
	}
}

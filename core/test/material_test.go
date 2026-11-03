package core_test

import (
	"gpt-robot/core"
	"gpt-robot/model"
	"gpt-robot/storage"
	"strings"
	"testing"
)

func TestMaterialExperiencePersistsGrowthRecallHistoryAndDeduplicates(t *testing.T) {
	s, fake := setup(t)
	credentials := model.Credentials{UID: "123", Cookies: map[string]string{"SESSDATA": "fixture", "bili_jct": "fixture"}}
	if err := s.Store.Write(ctx, ".private/bilibili.json", &credentials); err != nil {
		t.Fatal(err)
	}
	cached := int64(0)
	input := model.MaterialExperienceInput{
		Platform: "bilibili", SourceID: "BV161nZ6JE6b", Title: "虚构养犬视频",
		Summary: "视频材料介绍动物和狗，比熊、柯基、雪纳瑞、金毛、柴犬及中华田园犬；作者说法尚未验证。",
		Memory:  "视频标题说五种，字幕实际提到六种犬。", Reaction: "金毛吃饭的样子让我想多了解狗。", FrameCount: 14,
		Signal:    &model.CharacterSignal{Mood: "curious", Intensity: 30, Curiosity: 1},
		Interests: []model.InterestSignal{{Topic: "犬类照料", Delta: 1}},
		Usage:     []model.TokenUsage{{Provider: "codex", Model: "fixture", RequestID: "material-fixture", Input: 120, Output: 10, CachedInput: &cached}},
	}
	before, err := s.CharacterStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	start := model.OperationStart{Platform: "bilibili", Action: "material-understood", Source: "automation"}
	operation, err := s.StartOperation(ctx, &start)
	if err != nil {
		t.Fatal(err)
	}
	finish := model.OperationFinish{Status: "failed", ErrorCode: "INPUT"}
	if err = s.FinishOperation(ctx, operation, &finish); err != nil {
		t.Fatal(err)
	}
	input.OperationID = operation
	input.ResumeProcessing = true
	result, err := s.RecordMaterialExperience(ctx, &input)
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.CharacterStatus(ctx)
	if err != nil || after.Experiences != before.Experiences+1 || after.Mood != "curious" {
		t.Fatal(after, err)
	}
	query := storage.MemoryQuery{Terms: []string{"动物", "狗"}, Limit: 3}
	episodes, err := s.Store.Database.RecallEpisodes(ctx, &query)
	if err != nil || len(episodes) != 1 || episodes[0].Reaction != input.Reaction {
		t.Fatal(episodes, err)
	}
	balances, err := s.Store.Database.GrowthBalances(ctx)
	if err != nil || len(balances) < 1 || balances[0].Familiarity <= 0 {
		t.Fatal(balances, err)
	}
	notes, err := s.Store.Notes(ctx)
	if err != nil || len(notes) != 1 || !strings.Contains(notes[0].Text, "非全程观看") {
		t.Fatal(notes, err)
	}
	operations, err := s.Store.Operations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, op := range operations {
		if op.ID == result.OperationID {
			found = op.Status == "recorded" && op.Reference == result.EventID && len(op.Usage) == 1
		}
	}
	if !found {
		t.Fatal("material history or usage missing", operations)
	}
	if _, err = s.RecordMaterialExperience(ctx, &input); err == nil {
		t.Fatal("duplicate material recorded")
	}
	if fake.counts()["send"] != 0 || fake.counts()["status"] != 0 {
		t.Fatal("material import contacted platform")
	}
	// 材料经历进入日记素材，但不调用真实日记模型。
	diary := &diaryGenerator{}
	s.DiaryGenerator = diary
	diaryInput := model.DiaryGenerateInput{}
	if _, err = s.GenerateDiary(ctx, &diaryInput); err != nil {
		t.Fatal(err)
	}
	included := false
	for _, prompt := range diary.prompts {
		for _, fact := range prompt.Facts {
			if fact.Kind == "material-understood" && strings.Contains(fact.Summary, "未听音频") {
				included = true
			}
		}
	}
	if !included {
		t.Fatal("material missing from diary sources")
	}
	root := s.Store.Root
	s.Store.Database.Close()
	reopened := core.NewService(root, fake)
	if err = reopened.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	defer reopened.Store.Database.Close()
	episodes, err = reopened.Store.Database.RecallEpisodes(ctx, &query)
	if err != nil || len(episodes) != 1 {
		t.Fatal("recall lost after reopen", episodes, err)
	}
	if _, err = reopened.RecordMaterialExperience(ctx, &input); err == nil {
		t.Fatal("dedupe lost after reopen")
	}
	input.SourceID = "BV1111111111"
	input.Signal.Intensity = 101
	if _, err = reopened.RecordMaterialExperience(ctx, &input); err == nil {
		t.Fatal("invalid signal persisted")
	}
}

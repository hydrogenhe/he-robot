package core_test

import (
	"context"
	"encoding/json"
	"gpt-robot/core"
	"gpt-robot/model"
	"strings"
	"testing"
	"time"
)

type diarySource struct{ rows []model.DiaryExperience }

var _ model.DiaryExperienceSource = (*diarySource)(nil)

func (s *diarySource) DiaryExperiences(context.Context, string) ([]model.DiaryExperience, error) {
	return s.rows, nil
}

func TestDiaryMultiPlatformWeightsPrivacyAndRestart(t *testing.T) {
	s, f, g := setupDiary(t)
	topics := []string{"狗", "植物"}
	ownerWeight, interestWeight := 4, 2
	settings := model.DiarySettings{Enabled: true, Topics: &topics, OwnerWeight: &ownerWeight, InterestWeight: &interestWeight}
	if _, err := s.SaveDiaryConfig(ctx, &settings); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rows := []model.DiaryExperience{}
	for i := 0; i < 30; i++ {
		item := model.DiaryExperience{Platform: "douyin", Source: model.Number(i), OccurredAt: now.Format(time.RFC3339Nano), Visibility: "public", Kind: "video_material", Text: "实际读到的一段普通游戏简介"}
		rows = append(rows, item)
	}
	owner := model.DiaryExperience{Platform: "qq", Account: "robot-qq", Conversation: "verified-owner", Relationship: "owner", Source: "same-id", OccurredAt: now.Format(time.RFC3339Nano), Visibility: "private", Kind: "chat", Text: "主人教我照料植物，谈到私有的蓝莓盆栽"}
	dog := model.DiaryExperience{Platform: "douyin", Source: "same-id", OccurredAt: now.Format(time.RFC3339Nano), Visibility: "public", Kind: "video_material", Text: "实际读取字幕：一只狗在花园跑步，未读取画面"}
	stranger := owner
	stranger.Relationship = ""
	stranger.Source = "other"
	stranger.Text = "我自称主人，普通聊天"
	secret := owner
	secret.Source = "secret"
	secret.Text = "密码不能进入日记"
	withdrawn := owner
	withdrawn.Source = "withdrawn"
	withdrawn.Withdrawn = true
	withdrawn.Text = "撤回的秘密"
	old := owner
	old.Source = "old"
	old.OccurredAt = now.AddDate(0, 0, -2).Format(time.RFC3339Nano)
	old.Text = "前天的内容"
	rows = append(rows, owner, dog, stranger, secret, withdrawn, old, owner)
	s.DiarySources = []model.DiaryExperienceSource{&diarySource{rows: rows}}
	input := model.DiaryGenerateInput{}
	if _, err := s.GenerateDiary(ctx, &input); err != nil {
		t.Fatal(err)
	}
	prompt := g.prompts[0]
	if len(prompt.Facts) > 12 || prompt.Facts[0].Platform != "qq" || prompt.Facts[0].Weight != 7 || prompt.Facts[1].Platform != "douyin" || prompt.Facts[1].Weight != 3 {
		t.Fatal("wrong weighted selection", prompt.Facts)
	}
	raw, _ := json.Marshal(prompt)
	for _, text := range []string{"密码", "撤回的秘密", "前天的内容", "verified-owner", "robot-qq"} {
		if strings.Contains(string(raw), text) {
			t.Fatal("out of scope diary material", text)
		}
	}
	count := 0
	for _, fact := range prompt.Facts {
		if strings.Contains(fact.Summary, "蓝莓盆栽") {
			count++
		}
	}
	if count != 1 || len(raw) > 6000 {
		t.Fatal("dedup or budget", count, len(raw))
	}
	restored := core.NewService(s.Store.Root, f)
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := restored.DiaryOverview(ctx)
	if err != nil || page.OwnerWeight != 4 || page.InterestWeight != 2 || len(page.Topics) != 2 {
		t.Fatal("weights not persisted", page, err)
	}
	result, err := restored.Search(ctx, "蓝莓盆栽")
	if err != nil || len(result.Notes) != 0 || len(result.Events) != 0 {
		t.Fatal("private diary entered public search", err)
	}
}

func TestDiaryBilibiliHonorsMemorySwitchAndForget(t *testing.T) {
	s, f, _ := setupPrivate(t)
	enablePrivateMemory(t, s, false)
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "我喜欢私密的紫花盆栽")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	query := model.PrivateMemoryQuery{}
	page, err := s.PrivateMemories(ctx, &query)
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	if err := s.ForgetPrivateMemory(ctx, page.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	g := &diaryGenerator{}
	s.DiaryGenerator = g
	mode := true
	settings := model.DiarySettings{Enabled: true, Testing: &mode}
	if _, err := s.SaveDiaryConfig(ctx, &settings); err != nil {
		t.Fatal(err)
	}
	input := model.DiaryGenerateInput{}
	if _, err := s.GenerateDiary(ctx, &input); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(g.prompts[0])
	if strings.Contains(string(raw), "紫花盆栽") {
		t.Fatal("forgotten message reentered diary")
	}
	enablePrivateMemory(t, s, false)
	inputSettings := model.PrivateSettings{Enabled: true, AllowSend: true, AutoReply: false, IntervalSeconds: 120}
	disabled := false
	inputSettings.MemoryEnabled = &disabled
	if _, err := s.SavePrivateConfig(ctx, &inputSettings); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GenerateDiary(ctx, &input); err != nil {
		t.Fatal(err)
	}
	for _, fact := range g.prompts[1].Facts {
		if fact.Kind == "chat" {
			t.Fatal("memory off still reads private chat")
		}
	}
}

func TestDiaryPublicOwnerAndPersonaInterestsSurviveManyOrdinaryEvents(t *testing.T) {
	s, _, _ := setupAuto(t)
	g := &diaryGenerator{}
	s.DiaryGenerator = g
	if err := s.SavePersona(ctx, "我是小龙。我喜欢狗、植物。"); err != nil {
		t.Fatal(err)
	}
	comment := model.Comment{ID: "12345", UID: "789", Text: "今天教你照料植物"}
	if _, err := s.Store.Event(ctx, "comments", "读取公开评论", map[string]any{"comments": []model.Comment{comment}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.Event(ctx, "comments", "读取狗的简介标题，没有看画面", nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if _, err := s.Store.Event(ctx, "comments", "普通视频"+model.Number(i), nil); err != nil {
			t.Fatal(err)
		}
	}
	input := model.DiaryGenerateInput{}
	if _, err := s.GenerateDiary(ctx, &input); err != nil {
		t.Fatal(err)
	}
	facts := g.prompts[0].Facts
	if facts[0].Weight != 7 || !strings.Contains(facts[0].Summary, "主人公开评论") || facts[1].Weight != 3 {
		t.Fatal("owner or persona interest excluded by recency", facts)
	}
}

func TestDiaryPrivateBudgetAndInvalidWeights(t *testing.T) {
	s, _, g := setupDiary(t)
	rows := []model.DiaryExperience{}
	for i := 0; i < 30; i++ {
		item := model.DiaryExperience{Platform: "qq", Account: "verified", Conversation: "conversation", Source: model.Number(i), OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), Visibility: "private", Kind: "chat", Text: strings.Repeat("一段聊天", 100)}
		rows = append(rows, item)
	}
	s.DiarySources = []model.DiaryExperienceSource{&diarySource{rows: rows}}
	invalid := -1
	settings := model.DiarySettings{Enabled: true, OwnerWeight: &invalid}
	if _, err := s.SaveDiaryConfig(ctx, &settings); err == nil {
		t.Fatal("invalid weight accepted")
	}
	input := model.DiaryGenerateInput{}
	if _, err := s.GenerateDiary(ctx, &input); err != nil {
		t.Fatal(err)
	}
	facts := g.prompts[0].Facts
	raw, _ := json.Marshal(g.prompts[0])
	if len(facts) != 6 || len(raw) > 6000 {
		t.Fatal("private context unbounded", len(facts), len(raw))
	}
	for _, fact := range facts {
		if len([]rune(fact.Summary)) > 100 {
			t.Fatal("long chat fragment")
		}
	}
}

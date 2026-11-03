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

type companionGenerator struct {
	cue     *model.CompanionCue
	prompts []model.ReplyPrompt
	signal  *model.CharacterSignal
}

func (g *companionGenerator) Generate(_ context.Context, p *model.ReplyPrompt) (*model.GeneratedReply, error) {
	g.prompts = append(g.prompts, *p)
	result := &model.GeneratedReply{Text: "好的，我记住这件事啦。", Companion: g.cue, Signal: g.signal, Usage: &model.TokenUsage{
		Provider:  "codex",
		Model:     "fixture",
		RequestID: model.ID(),
		Input:     100,
		Output:    20,
	}}
	return result, nil
}
func companionService(t *testing.T) (*core.Service, *privateBili, *companionGenerator) {
	t.Helper()
	s, f, _ := setupPrivate(t)
	enabled := true
	settings := model.PrivateSettings{
		Enabled:         true,
		AllowSend:       true,
		AutoReply:       true,
		IntervalSeconds: 120,
		MemoryEnabled:   &enabled,
	}
	if _, err := s.SavePrivateConfig(ctx, &settings); err != nil {
		t.Fatal(err)
	}
	g := &companionGenerator{}
	s.Generator = g
	s.ExpressionDraw = func(int) int { return 0 }
	s.ExpressionWait = func(context.Context, time.Duration) error { return nil }
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	return s, f, g
}
func readCompanion(t *testing.T, s *core.Service) model.CompanionState {
	t.Helper()
	rows, err := s.Store.Database.ListDocuments(ctx, "data/private/companion/", 20)
	if err != nil || len(rows) != 1 {
		t.Fatal("missing companion state", len(rows), err)
	}
	state := model.CompanionState{}
	if err := json.Unmarshal(rows[0], &state); err != nil {
		t.Fatal(err)
	}
	return state
}
func restartCompanion(t *testing.T, s *core.Service, f *privateBili, g *companionGenerator) *core.Service {
	t.Helper()
	page, err := s.PrivateOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range page.State.Outbox {
		page.State.Outbox[i].At = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	}
	if err := s.Store.Write(ctx, "data/private/state.json", &page.State); err != nil {
		t.Fatal(err)
	}
	root := s.Store.Root
	s.Store.Database.Close()
	fresh := core.NewService(root, f)
	fresh.Generator = g
	if err := fresh.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	fresh.ExpressionDraw = func(int) int { return 0 }
	fresh.ExpressionWait = func(context.Context, time.Duration) error { return nil }
	return fresh
}
func seedRandomSlot(t *testing.T, s *core.Service, now time.Time) {
	t.Helper()
	cfg := model.ProactiveConfig{
		Enabled:         true,
		RandomMin:       3,
		RandomMax:       5,
		VideoDailyLimit: 10,
	}
	if _, err := s.SaveProactiveConfig(ctx, &cfg); err != nil {
		t.Fatal(err)
	}
	job := model.ProactiveJob{
		Key:       "fixture-random",
		Kind:      "random",
		Status:    "pending",
		DueAt:     now.Add(-time.Minute).UTC().Format(time.RFC3339Nano),
		ExpiresAt: now.Add(time.Hour).UTC().Format(time.RFC3339Nano),
		SenderUID: "123",
		PeerUID:   "789",
	}
	state := model.ProactiveState{
		Day:  now.In(time.FixedZone("CN", 8*3600)).Format("2006-01-02"),
		Jobs: []model.ProactiveJob{job},
	}
	if err := s.Store.Write(ctx, "data/private/proactive.json", &state); err != nil {
		t.Fatal(err)
	}
}
func TestCompanionFollowupPersistsUsesExistingSlotAndDoesNotRepeat(t *testing.T) {
	s, f, g := companionService(t)
	now := proactiveClock(10, 0)
	s.ProactiveNow = func() time.Time { return now }
	g.cue = &model.CompanionCue{
		Quote:  "明天要面试",
		Topic:  "面试",
		DueAt:  now.Add(time.Hour).Format(time.RFC3339Nano),
		Status: "pending",
	}
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "明天要面试")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	state := readCompanion(t, s)
	if len(state.Followups) != 1 || state.Followups[0].Status != "pending" {
		t.Fatal(state)
	}
	now = proactiveClock(12, 0)
	seedRandomSlot(t, s, now)
	s = restartCompanion(t, s, f, g)
	defer s.Store.Database.Close()
	s.ProactiveNow = func() time.Time { return now }
	if err := s.ProactiveTick(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(g.prompts[len(g.prompts)-1].Text, "面试") || readCompanion(t, s).Followups[0].Status != "asked" {
		t.Fatal("missing followup", g.prompts)
	}
	count := len(g.prompts)
	if err := s.ProactiveTick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(g.prompts) != count {
		t.Fatal("followup repeated")
	}
	proactive, _ := s.ProactiveStatus(ctx)
	if len(proactive.State.Jobs) != 1 {
		t.Fatal("added quota", proactive)
	}
	g.cue = &model.CompanionCue{
		Quote:      "不想聊这个",
		FollowupID: state.Followups[0].ID,
		Status:     "dismissed",
	}
	s = restartCompanion(t, s, f, g)
	s.ProactiveNow = func() time.Time { return now }
	f.messages = append(f.messages, privateMessage(2, "789", "123", "不想聊这个"))
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if readCompanion(t, s).Followups[0].Status != "dismissed" {
		t.Fatal("ignored dismissal")
	}
	s.Store.Database.Close()
}
func TestCompanionBusySuppressesProactiveButRepliesAndCombinesBurst(t *testing.T) {
	s, f, g := companionService(t)
	now := proactiveClock(12, 0)
	s.ProactiveNow = func() time.Time { return now }
	minutes := 120
	g.cue = &model.CompanionCue{
		Quote:       "我在忙",
		BusyMinutes: &minutes,
	}
	f.messages = []model.PrivateMessage{
		privateMessage(1, "789", "123", "我在忙"),
		privateMessage(2, "789", "123", "稍后再聊"),
	}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(g.prompts) != 1 || !strings.Contains(g.prompts[0].Text, "我在忙\n稍后再聊") {
		t.Fatal("burst not combined", g.prompts)
	}
	seedRandomSlot(t, s, now)
	s = restartCompanion(t, s, f, g)
	defer s.Store.Database.Close()
	s.ProactiveNow = func() time.Time { return now }
	count := len(g.prompts)
	if err := s.ProactiveTick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(g.prompts) != count {
		t.Fatal("busy proactively contacted")
	}
	g.cue = nil
	f.messages = append(f.messages, privateMessage(3, "789", "123", "我有个问题"))
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(g.prompts) != count+1 {
		t.Fatal("busy blocked incoming reply")
	}
}
func TestCompanionRejectsInventedEvidence(t *testing.T) {
	s, f, g := companionService(t)
	g.cue = &model.CompanionCue{
		Quote:  "明天去医院",
		Topic:  "医院",
		DueAt:  time.Now().Add(time.Hour).Format(time.RFC3339Nano),
		Status: "pending",
	}
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "今天聊聊天")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Store.Database.ListDocuments(ctx, "data/private/companion/", 20)
	if err != nil || len(rows) != 0 {
		t.Fatal("invented event saved", err)
	}
}
func TestCompanionMoodDoesNotFlipOnSingleOpposingMessage(t *testing.T) {
	s, f, g := companionService(t)
	g.signal = &model.CharacterSignal{
		Mood:      "happy",
		Intensity: 90,
	}
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "开心")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	s = restartCompanion(t, s, f, g)
	defer s.Store.Database.Close()
	g.signal = &model.CharacterSignal{
		Mood:      "sad",
		Intensity: 70,
	}
	f.messages = append(f.messages, privateMessage(2, "789", "123", "有点失落"))
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := s.CharacterStatus(ctx)
	if err != nil || state.Mood != "happy" || state.PendingMood != "sad" {
		t.Fatal("abrupt mood change", state, err)
	}
}

func TestCompanionExplorationAndGroundedSharingUseActualMaterial(t *testing.T) {
	s, f, g := companionService(t)
	now := time.Now()
	// 固定为当日白天，独立于执行机器时区。
	local := now.In(time.FixedZone("CN", 8*3600))
	now = time.Date(local.Year(), local.Month(), local.Day(), 14, 0, 0, 0, local.Location())
	s.ProactiveNow = func() time.Time { return now }
	reader := &materialReaderFixture{}
	request := model.VideoReadInput{Video: model.Video{BVID: "BV161nZ6JE6b"}}
	input, err := reader.ReadVideo(ctx, &request)
	if err != nil {
		t.Fatal(err)
	}
	input.ExplorationQuestion = "犬类训练方法"
	if _, err = s.RecordMaterialExperience(ctx, input); err != nil {
		t.Fatal(err)
	}
	browsing, err := s.BrowsingOverview(ctx)
	if err != nil || len(browsing.EffectiveKeywords) == 0 || browsing.EffectiveKeywords[0] != "犬类训练方法" {
		t.Fatal("missing continued exploration", browsing, err)
	}
	seedRandomSlot(t, s, now)
	s = restartCompanion(t, s, f, g)
	defer s.Store.Database.Close()
	s.ProactiveNow = func() time.Time { return now }
	if err = s.ProactiveTick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(g.prompts) != 1 || !strings.Contains(g.prompts[0].Text, "犬类生活") {
		t.Fatal("ungrounded sharing", g.prompts)
	}
	state := readCompanion(t, s)
	if len(state.SharedSources) != 1 {
		t.Fatal("sharing provenance missing", state)
	}
	now = now.AddDate(0, 0, 4)
	browsing, err = s.BrowsingOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range browsing.EffectiveKeywords {
		if word == "犬类训练方法" {
			t.Fatal("exploration never expires")
		}
	}
}

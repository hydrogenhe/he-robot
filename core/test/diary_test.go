package core_test

import (
	"context"
	"encoding/json"
	"gpt-robot/core"
	"gpt-robot/model"
	"strings"
	"sync"
	"testing"
	"time"
)

type diaryGenerator struct {
	mu      sync.Mutex
	prompts []model.DiaryPrompt
	err     error
	started chan struct{}
}

func (g *diaryGenerator) GenerateDiary(_ context.Context, input *model.DiaryPrompt) (*model.GeneratedDiary, error) {
	g.mu.Lock()
	g.prompts = append(g.prompts, *input)
	g.mu.Unlock()
	if g.started != nil {
		select {
		case g.started <- struct{}{}:
		default:
		}
	}
	usage := &model.TokenUsage{Provider: "codex", Model: "test", RequestID: model.ID(), Input: 120, Output: 45}
	result := &model.GeneratedDiary{Title: "今天的小小回声", Body: "今天读到了几句有趣的讨论，也收到了一些回应。\n\n我想把这份好奇留到明天，慢慢认识这个世界。", Usage: usage}
	return result, g.err
}
func (g *diaryGenerator) count() int { g.mu.Lock(); defer g.mu.Unlock(); return len(g.prompts) }
func setupDiary(t *testing.T) (*core.Service, *fakeBili, *diaryGenerator) {
	t.Helper()
	s, f := setup(t)
	g := &diaryGenerator{}
	s.DiaryGenerator = g
	return s, f, g
}
func TestDiaryEmptyZeroModelThenOnePerDayRestartAndPrivateIsolation(t *testing.T) {
	s, f, g := setupDiary(t)
	input := model.DiaryGenerateInput{}
	empty, err := s.GenerateDiary(ctx, &input)
	if err != nil || empty.Status != "empty" || g.count() != 0 {
		t.Fatal(empty, err)
	}
	if _, err := s.Store.Event(ctx, "comments", "读取《公开植物视频》评论", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.WriteText(ctx, "data/private/state-secret.txt", "不应读取的私密原文"); err != nil {
		t.Fatal(err)
	}
	entry, err := s.GenerateDiary(ctx, &input)
	if err != nil || entry.Status != "completed" || entry.OperationID == "" || g.count() != 1 {
		t.Fatal(entry, err)
	}
	if _, err := s.GenerateDiary(ctx, &input); err != nil || g.count() != 1 {
		t.Fatal("same day repeated", err)
	}
	raw, _ := json.Marshal(g.prompts[0])
	if len(raw) > 6000 || strings.Contains(string(raw), "私密原文") {
		t.Fatal("context leaked or exceeded budget")
	}
	query := model.OperationQuery{}
	usage, err := s.QueryOperations(ctx, &query)
	if err != nil || usage.Input != 120 || usage.Output != 45 || usage.Reported != 1 {
		t.Fatal(usage, err)
	}
	restored := core.NewService(s.Store.Root, f)
	restored.DiaryGenerator = g
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.GenerateDiary(ctx, &input); err != nil || g.count() != 1 {
		t.Fatal("restart repeated AI", err)
	}
	page, err := restored.DiaryOverview(ctx)
	if err != nil || page.TodayStatus != "completed" || len(page.Entries) != 1 || page.Entries[0].Body != entry.Body {
		t.Fatal(page, err)
	}
	public, err := restored.Search(ctx, "小小回声")
	if err != nil || len(public.Notes) != 0 || len(public.Events) != 0 {
		t.Fatal("diary entered public search", public, err)
	}
	context, err := restored.Context(ctx)
	raw, _ = json.Marshal(context)
	if err != nil || strings.Contains(string(raw), entry.Body) {
		t.Fatal("diary entered public context", err)
	}
}
func TestDiaryFailureRequiresExplicitRetryAndKeepsActualUsage(t *testing.T) {
	s, _, g := setupDiary(t)
	if _, err := s.Store.Event(ctx, "comments", "读取《游戏》评论", nil); err != nil {
		t.Fatal(err)
	}
	settings := model.DiarySettings{Enabled: true}
	if _, err := s.SaveDiaryConfig(ctx, &settings); err != nil {
		t.Fatal(err)
	}
	g.err = model.Fail("AI_DIARY", "mock failure")
	input := model.DiaryGenerateInput{}
	if _, err := s.GenerateDiary(ctx, &input); err == nil {
		t.Fatal("failure accepted")
	}
	automatic := model.DiaryGenerateInput{Automatic: true}
	if _, err := s.GenerateDiary(ctx, &automatic); err == nil || g.count() != 1 {
		t.Fatal("automatic repeated failed model", err)
	}
	if _, err := s.GenerateDiary(ctx, &input); err == nil || g.count() != 1 {
		t.Fatal("manual retry missing explicit flag")
	}
	g.err = nil
	retry := model.DiaryGenerateInput{Retry: true}
	if _, err := s.GenerateDiary(ctx, &retry); err != nil || g.count() != 2 {
		t.Fatal(err)
	}
}
func TestDiarySchedulerAt23WritesOnceAndDoesNotReadPrivateBodies(t *testing.T) {
	s, f, g := setupDiary(t)
	now := time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60))
	fixed := time.Date(now.Year(), now.Month(), now.Day(), 23, 5, 0, 0, now.Location())
	s.DiaryNow = func() time.Time { return fixed }
	if _, err := s.Store.Event(ctx, "comments", "读取《植物》评论，未看视频画面", nil); err != nil {
		t.Fatal(err)
	}
	setting := model.DiarySettings{Enabled: true}
	if _, err := s.SaveDiaryConfig(ctx, &setting); err != nil {
		t.Fatal(err)
	}
	g.started = make(chan struct{}, 1)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { s.RunAutomation(runCtx); close(done) }()
	select {
	case <-g.started:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("23:00 schedule did not generate")
	}
	// 生成完成前不能模拟进程退出；等可观察日记状态。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		page, err := s.DiaryOverview(ctx)
		if err == nil && page.TodayStatus == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if g.count() != 1 {
		t.Fatal(g.count())
	}
	restored := core.NewService(s.Store.Root, f)
	restored.DiaryGenerator = g
	restored.DiaryNow = func() time.Time { return fixed }
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	short, stop := context.WithTimeout(ctx, 80*time.Millisecond)
	defer stop()
	restored.RunAutomation(short)
	if g.count() != 1 {
		t.Fatal("scheduled restart replayed diary")
	}
}
func TestDiaryPrivateContentOnlyInPrivateDiaryNotPublicContext(t *testing.T) {
	s, f, reply := setupPrivate(t)
	enablePrivateMemory(t, s, true)
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "我养了一盆不能公开的龟背竹")}
	reply.signal = &model.CharacterSignal{Mood: "happy", Intensity: 30}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	diary := &diaryGenerator{}
	s.DiaryGenerator = diary
	input := model.DiaryGenerateInput{}
	if _, err := s.GenerateDiary(ctx, &input); err != nil {
		t.Fatal(err)
	}
	if diary.count() != 1 {
		t.Fatal("private interaction missing")
	}
	raw, _ := json.Marshal(diary.prompts[0])
	if !strings.Contains(string(raw), "龟背竹") || !strings.Contains(string(raw), `"visibility":"private"`) {
		t.Fatal("authorized chat absent from private diary")
	}
	public, err := s.Context(ctx)
	raw, _ = json.Marshal(public)
	if err != nil || strings.Contains(string(raw), "龟背竹") {
		t.Fatal("private diary leaked to public context", err)
	}
}

func TestDiaryTestingAllowsEmptyAndRepeatedManualWithoutAutomaticLoop(t *testing.T) {
	s, f, g := setupDiary(t)
	testingMode := true
	settings := model.DiarySettings{Enabled: true, Testing: &testingMode}
	if _, err := s.SaveDiaryConfig(ctx, &settings); err != nil {
		t.Fatal(err)
	}
	input := model.DiaryGenerateInput{}
	first, err := s.GenerateDiary(ctx, &input)
	if err != nil || first.Status != "completed" || !first.Testing || len(first.Sources) != 0 || g.count() != 1 {
		t.Fatal(first, err)
	}
	if _, err := s.GenerateDiary(ctx, &input); err != nil || g.count() != 2 {
		t.Fatal("testing repeat blocked", err)
	}
	g.err = model.Fail("AI_DIARY", "测试生成失败")
	retained, failedErr := s.GenerateDiary(ctx, &input)
	if failedErr == nil || retained.Status != "completed" || retained.Body != first.Body {
		t.Fatal("failed rewrite lost successful diary", retained, failedErr)
	}
	g.err = nil
	raw, _ := json.Marshal(g.prompts[0])
	if !strings.Contains(string(raw), "暂无已记录") {
		t.Fatal("empty test fabricated source")
	}
	automatic := model.DiaryGenerateInput{Automatic: true}
	if _, err := s.GenerateDiary(ctx, &automatic); err != nil || g.count() != 3 {
		t.Fatal("automatic testing loop", err)
	}
	restored := core.NewService(s.Store.Root, f)
	restored.DiaryGenerator = g
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := restored.DiaryOverview(ctx)
	if err != nil || !page.Testing {
		t.Fatal("testing setting lost", err)
	}
	testingMode = false
	settings.Testing = &testingMode
	if _, err := restored.SaveDiaryConfig(ctx, &settings); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.GenerateDiary(ctx, &input); err != nil || g.count() != 3 {
		t.Fatal("formal mode failed to restore daily cap", err)
	}
}

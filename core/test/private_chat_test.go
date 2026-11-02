package core_test

import (
	"context"
	"gpt-robot/core"
	"gpt-robot/model"
	"sync"
	"testing"
	"time"
)

type fakeGenerator struct {
	text    *string
	signal  *model.CharacterSignal
	mu      sync.Mutex
	prompts []model.ReplyPrompt
	before  func()
}

func (g *fakeGenerator) Generate(ctx context.Context, input *model.ReplyPrompt) (*model.GeneratedReply, error) {
	g.mu.Lock()
	g.prompts = append(g.prompts, *input)
	g.mu.Unlock()
	if g.before != nil {
		g.before()
	}
	usage := &model.TokenUsage{Provider: "codex", Model: "test-model", RequestID: model.ID(), Input: 100, Output: 23}
	result := &model.GeneratedReply{Text: "这是AI生成的简短回应🐉", Usage: usage, Signal: g.signal}
	if g.text != nil {
		result.Text = *g.text
	}
	return result, nil
}
func (g *fakeGenerator) count() int { g.mu.Lock(); defer g.mu.Unlock(); return len(g.prompts) }

// 默认替身不连接B站；私信测试使用privateBili覆盖真实边界。
func (f *fakeBili) PrivateMessages(context.Context, string, string) (*model.PrivatePage, error) {
	result := &model.PrivatePage{Messages: []model.PrivateMessage{}}
	return result, nil
}
func (f *fakeBili) SendPrivate(context.Context, string, string, string, string) (string, error) {
	f.count("private-send")
	return "9001", nil
}

func (f *fakeBili) NativeStickers() []model.NativeSticker { return nil }
func (f *fakeBili) DownloadPrivateImage(context.Context, *model.ImageAttachment) (*model.ImageAttachment, error) {
	return nil, model.Fail("IMAGE_DOWNLOAD", "测试替身无图片")
}
func (f *fakeBili) SendPrivateImage(context.Context, string, string, *model.ImageAttachment, string) (string, error) {
	return "", model.Fail("IMAGE_UPLOAD", "测试替身无图片")
}

type privateBili struct {
	*autoBili
	messages   []model.PrivateMessage
	recipients []string
	sendError  error
	readError  error
}

func (f *privateBili) PrivateMessages(_ context.Context, sender, peer string) (*model.PrivatePage, error) {
	f.count("private-read")
	if sender != "123" || peer != "789" {
		return nil, model.Fail("ACCOUNT", "wrong scope")
	}
	if f.readError != nil {
		return nil, f.readError
	}
	result := &model.PrivatePage{Messages: append([]model.PrivateMessage{}, f.messages...)}
	return result, nil
}
func (f *privateBili) SendPrivate(_ context.Context, sender, peer, text, request string) (string, error) {
	f.count("private-send")
	f.recipients = append(f.recipients, peer)
	if sender != "123" || peer != "789" || text == "" {
		return "", model.Fail("ACCOUNT", "wrong scope")
	}
	return "999999999999999999", f.sendError
}
func setupPrivate(t *testing.T) (*core.Service, *privateBili, *fakeGenerator) {
	t.Helper()
	s, f, _ := setupAuto(t)
	fake := &privateBili{autoBili: f}
	generator := &fakeGenerator{}
	s.Bili, s.Generator = fake, generator
	input := model.PrivateSettings{Enabled: true, AllowSend: true, AutoReply: true, IntervalSeconds: 120}
	if _, err := s.SavePrivateConfig(ctx, &input); err != nil {
		t.Fatal(err)
	}
	return s, fake, generator
}
func privateMessage(seq int64, from, to, text string) model.PrivateMessage {
	result := model.PrivateMessage{Key: model.Number(seq + 10000), Seq: seq, Sender: from, Receiver: to, Text: text, Timestamp: time.Now().Unix() + 1}
	return result
}
func TestPrivateBaselineOwnerOnlyAIGenerationAndRestart(t *testing.T) {
	s, f, generator := setupPrivate(t)
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "历史消息")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if generator.count() != 0 {
		t.Fatal("history invoked model")
	}
	withdrawn := privateMessage(3, "789", "123", "不应进入上下文")
	withdrawn.Withdrawn = true
	f.messages = append(f.messages, privateMessage(2, "456", "123", "陌生人"), withdrawn, privateMessage(4, "789", "123", "帮我算2+2"), privateMessage(5, "789", "123", "今天过得怎么样呀"))
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if generator.count() != 1 || len(f.recipients) != 1 || f.recipients[0] != "789" {
		t.Fatal(generator.count(), f.recipients)
	}
	state, err := s.PrivateOverview(ctx)
	if err != nil || len(state.State.Outbox) != 1 || state.State.Outbox[0].Text != "这是AI生成的简短回应🐉" {
		t.Fatal(state, err)
	}
	for _, row := range state.State.Inbox {
		if row.Sender == "456" || row.Withdrawn && row.Text != "" {
			t.Fatal("foreign or withdrawn body leaked")
		}
	}
	query := model.OperationQuery{}
	page, err := s.QueryOperations(ctx, &query)
	if err != nil || page.Input+page.Output != 123 || page.Reported != 1 {
		t.Fatal(page, err)
	}
	restored := core.NewService(s.Store.Root, f)
	restored.Generator = generator
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := restored.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if generator.count() != 1 || len(f.recipients) != 1 {
		t.Fatal("restart repeated private reply")
	}
}
func TestRobotPauseStopsBothChannelsAndSurvivesRestart(t *testing.T) {
	s, f, generator := setupPrivate(t)
	if _, err := s.SetRobot(ctx, false); err != nil {
		t.Fatal(err)
	}
	before := f.counts()
	if err := s.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	after := f.counts()
	if after["comments"] != before["comments"] || after["private-read"] != before["private-read"] || generator.count() != 0 {
		t.Fatal("paused robot queried platforms or AI")
	}
	if _, err := s.Overview(ctx); err != nil {
		t.Fatal("paused web unavailable", err)
	}
	restored := core.NewService(s.Store.Root, f)
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := restored.RobotStatus(ctx)
	if err != nil || status.Enabled {
		t.Fatal("pause lost on restart", status, err)
	}
}
func TestPauseDuringGenerationPreventsPrivateSendAndRecordsActualUsage(t *testing.T) {
	s, f, generator := setupPrivate(t)
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	generator.before = func() {
		if _, err := s.SetRobot(ctx, false); err != nil {
			t.Fatal(err)
		}
	}
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "你好呀")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if generator.count() != 1 || len(f.recipients) != 0 {
		t.Fatal("paused generation sent message")
	}
	query := model.OperationQuery{}
	page, err := s.QueryOperations(ctx, &query)
	if err != nil || page.Input+page.Output != 123 || page.Items[0].Status != "cancelled" {
		t.Fatal(page, err)
	}
}
func TestPrivateUnknownIsNotRetriedWithSameNonceOrAfterRestart(t *testing.T) {
	s, f, _ := setupPrivate(t)
	f.sendError = model.Fail("SEND_UNKNOWN", "unknown")
	input := model.PrivateSendInput{RequestID: model.ID(), Text: "一次发送"}
	out, err := s.SendPrivate(ctx, &input)
	if err == nil || out.Status != "unknown" {
		t.Fatal(out, err)
	}
	again, err := s.SendPrivate(ctx, &input)
	if err != nil || again.Status != "unknown" || len(f.recipients) != 1 {
		t.Fatal(again, err)
	}
	restored := core.NewService(s.Store.Root, f)
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := restored.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.recipients) != 1 {
		t.Fatal("unknown retried")
	}
	disable := model.PrivateSettings{Enabled: false, AllowSend: false, AutoReply: false, IntervalSeconds: 120}
	if _, err := restored.SavePrivateConfig(ctx, &disable); err != nil {
		t.Fatal("cannot disable after unknown", err)
	}
}
func TestCommentQuotaAndFilterBeforeModel(t *testing.T) {
	s, f, config := setupAuto(t)
	generator := s.Generator.(*fakeGenerator)
	config.OtherDailyLimit = 1
	if _, err := s.SaveAutoConfig(ctx, &config); err != nil {
		t.Fatal(err)
	}
	if err := s.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	old := priorAutoDraft("456")
	old.AttemptAt = time.Now().Add(-3 * time.Minute).UTC().Format(time.RFC3339Nano)
	if err := s.Store.Write(ctx, "data/events/drafts.json", []model.Draft{old}); err != nil {
		t.Fatal(err)
	}
	f.roots = []model.Comment{autoComment("1001", "456", "你好"), autoComment("1002", "457", "数学题2+2")}
	if err := s.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	if generator.count() != 0 || len(f.sentParents) != 0 {
		t.Fatal("quota or rejected input consumed AI")
	}
}

func TestPrivateIntervalEditPreservesPauseCursorAndReadBackoff(t *testing.T) {
	s, f, g := setupPrivate(t)
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	var state model.PrivateState
	if _, err := s.Store.Read(ctx, "data/private/state.json", &state); err != nil {
		t.Fatal(err)
	}
	state.Cursor = 777
	state.Paused, state.ReadPaused = true, true
	state.Reason = "保留安全暂停"
	state.ReadFailures = 5
	state.NextAt = time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339Nano)
	if err := s.Store.Write(ctx, "data/private/state.json", &state); err != nil {
		t.Fatal(err)
	}
	restored := core.NewService(s.Store.Root, f)
	restored.Generator = g
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	input := model.PrivateSettings{Enabled: true, AllowSend: true, AutoReply: true, IntervalSeconds: 30}
	result, err := restored.SavePrivateConfig(ctx, &input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.State.Paused || !result.State.ReadPaused || result.State.Cursor != 777 || result.State.ReadFailures != 5 || result.State.NextAt != state.NextAt {
		t.Fatal("schedule edit reset safety state", result.State)
	}
	if err := restored.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if g.count() != 0 || len(f.recipients) != 0 {
		t.Fatal("interval edit replayed a reply")
	}
}

func TestPrivateShortCooldownStillGuardsBeforeModelAndAllowsConversation(t *testing.T) {
	s, f, g := setupPrivate(t)
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	var state model.PrivateState
	if _, err := s.Store.Read(ctx, "data/private/state.json", &state); err != nil {
		t.Fatal(err)
	}
	out := model.PrivateOutbox{RequestID: model.ID(), Text: "先前一条不同的回复", At: time.Now().Add(-5 * time.Second).UTC().Format(time.RFC3339Nano), Status: "sent"}
	state.Outbox = append(state.Outbox, out)
	if err := s.Store.Write(ctx, "data/private/state.json", &state); err != nil {
		t.Fatal(err)
	}
	restored := core.NewService(s.Store.Root, f)
	restored.Generator = g
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "今天开心吗")}
	beforeReads := f.calls["private-read"]
	if err := restored.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if g.count() != 0 {
		t.Fatal("cooldown consumed model")
	}
	if f.calls["private-read"] != beforeReads {
		t.Fatal("known cooldown caused unnecessary platform query")
	}
	page, err := restored.PrivateOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := time.Parse(time.RFC3339Nano, out.At)
	scheduled, err := time.Parse(time.RFC3339Nano, page.State.NextAt)
	if err != nil || !scheduled.Equal(expected.Add(10*time.Second)) || page.State.Cursor != state.Cursor {
		t.Fatal("cooldown overslept or consumed message", page.State.NextAt, err)
	}
	state.Outbox[0].At = time.Now().Add(-11 * time.Second).UTC().Format(time.RFC3339Nano)
	if err := s.Store.Write(ctx, "data/private/state.json", &state); err != nil {
		t.Fatal(err)
	}
	restored = core.NewService(s.Store.Root, f)
	restored.Generator = g
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := restored.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if g.count() != 1 || len(f.recipients) != 1 {
		t.Fatal("old 120s cooldown still blocks conversation", g.count())
	}
}

func (f *fakeBili) WatchPrivate(ctx context.Context, _ string, _ string, _ func(*model.PrivatePushEvent)) error {
	<-ctx.Done()
	return ctx.Err()
}

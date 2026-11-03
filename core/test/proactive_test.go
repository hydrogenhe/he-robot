package core_test

import (
	"context"
	"gpt-robot/core"
	"gpt-robot/model"
	"strings"
	"testing"
	"time"
)

func proactiveClock(hour, minute int) time.Time {
	return time.Date(2026, 11, 3, hour, minute, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600))
}
func enableProactive(t *testing.T, s *core.Service) {
	t.Helper()
	c := model.ProactiveConfig{Enabled: true, VideoReflections: true, DiaryDelivery: true, Greetings: true, RandomMin: 3, RandomMax: 5, VideoDailyLimit: 10}
	if _, err := s.SaveProactiveConfig(ctx, &c); err != nil {
		t.Fatal(err)
	}
}
func TestProactiveQuietScheduleBoundOwnerAndOneGeneration(t *testing.T) {
	s, f, g := setupPrivate(t)
	now := proactiveClock(8, 0)
	s.ProactiveNow = func() time.Time { return now }
	s.ExpressionDraw = func(n int) int { return 0 }
	enableProactive(t, s)
	if err := s.ProactiveTick(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := s.ProactiveStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.State.Jobs) != 5 || g.count() != 0 || f.counts()["private-send"] != 0 {
		t.Fatal("quiet hours generated or wrong schedule", status, g.count())
	}
	for _, j := range status.State.Jobs {
		due, _ := time.Parse(time.RFC3339Nano, j.DueAt)
		hour := due.In(now.Location()).Hour()
		if hour < 9 || hour >= 24 {
			t.Fatal("scheduled during quiet", j)
		}
	}
	now = proactiveClock(9, 0)
	if err = s.ProactiveTick(ctx); err != nil {
		t.Fatal(err)
	}
	if g.count() != 1 || len(f.recipients) != 1 || f.recipients[0] != "789" {
		t.Fatal("wrong recipient or duplicate generation", g.count(), f.recipients)
	}
	overview, overviewErr := s.PrivateOverview(ctx)
	if overviewErr != nil || len(overview.State.Outbox) != 1 || !overview.State.Outbox[0].Proactive {
		t.Fatal("proactive delivery mislabeled as manual", overviewErr)
	}
	if err = s.ProactiveTick(ctx); err != nil {
		t.Fatal(err)
	}
	if g.count() != 1 || len(f.recipients) != 1 {
		t.Fatal("duplicate proactive send")
	}
	// 接收账号切换不能沿用旧主动队列。
	var binding model.PrivateConfig
	if _, err = s.Store.Read(ctx, "config/private-chat.json", &binding); err != nil {
		t.Fatal(err)
	}
	binding.PeerUID = "999"
	if err = s.Store.Write(ctx, "config/private-chat.json", &binding); err != nil {
		t.Fatal(err)
	}
	root := s.Store.Root
	s.Store.Database.Close()
	restored := core.NewService(root, f)
	restored.Generator = g
	restored.ProactiveNow = func() time.Time { return proactiveClock(13, 0) }
	if err = restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	defer restored.Store.Database.Close()
	if err = restored.ProactiveTick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.recipients) != 1 {
		t.Fatal("account change reused pending jobs")
	}
}

type longDiary struct{}

func (g *longDiary) GenerateDiary(context.Context, *model.DiaryPrompt) (*model.GeneratedDiary, error) {
	usage := model.TokenUsage{Provider: "codex", Model: "fixture", RequestID: model.ID(), Input: 10, Output: 20}
	return &model.GeneratedDiary{Title: "今天的日记", Body: strings.Repeat("今天我记下了一点有趣的观察。", 45), Usage: &usage}, nil
}
func TestProactiveDiaryFullBodyQuietQueueAndNoExtraGeneration(t *testing.T) {
	s, f, g := setupPrivate(t)
	now := proactiveClock(0, 15)
	s.ProactiveNow = func() time.Time { return now }
	s.DiaryGenerator = &longDiary{}
	enableProactive(t, s)
	// 日记素材允许测试空日重写；复用既有保存入口，不模拟发送正文。
	testing := true
	settings := model.DiarySettings{Enabled: true, Testing: &testing}
	if _, err := s.SaveDiaryConfig(ctx, &settings); err != nil {
		t.Fatal(err)
	}
	input := model.DiaryGenerateInput{}
	entry, err := s.GenerateDiary(ctx, &input)
	if err != nil {
		t.Fatal(err)
	}
	status, err := s.ProactiveStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.State.Jobs) != 1 {
		t.Fatal(status)
	}
	job := status.State.Jobs[0]
	if job.Kind != "diary" || strings.Join(job.Parts, "") != "《"+entry.Title+"》\n"+entry.Body || len(job.Parts) < 2 {
		t.Fatal("diary truncated", job)
	}
	due, _ := time.Parse(time.RFC3339Nano, job.DueAt)
	if due.In(now.Location()).Hour() != 9 {
		t.Fatal("diary not delayed to 9", due)
	}
	if err = s.ProactiveTick(ctx); err != nil {
		t.Fatal(err)
	}
	if g.count() != 0 || f.counts()["private-send"] != 0 {
		t.Fatal("quiet diary sent or called extra model")
	}
}
func TestProactiveUnknownSendNotRetriedAfterRestart(t *testing.T) {
	s, f, g := setupPrivate(t)
	now := proactiveClock(8, 0)
	s.ProactiveNow = func() time.Time { return now }
	s.ExpressionDraw = func(n int) int { return 0 }
	enableProactive(t, s)
	if err := s.ProactiveTick(ctx); err != nil {
		t.Fatal(err)
	}
	f.sendError = model.Fail("SEND_UNKNOWN", "fixture unknown")
	now = proactiveClock(9, 0)
	if err := s.ProactiveTick(ctx); err == nil {
		t.Fatal("unknown not reported")
	}
	if g.count() != 1 || len(f.recipients) != 1 {
		t.Fatal(g.count(), f.recipients)
	}
	if err := s.ProactiveTick(ctx); err != nil {
		t.Fatal(err)
	}
	if g.count() != 1 || len(f.recipients) != 1 {
		t.Fatal("unknown send retried")
	}
	root := s.Store.Root
	s.Store.Database.Close()
	restored := core.NewService(root, f)
	restored.Generator = g
	restored.ProactiveNow = func() time.Time { return now }
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	defer restored.Store.Database.Close()
	if err := restored.ProactiveTick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.recipients) != 1 {
		t.Fatal("unknown replayed after restart")
	}
}
func TestProactivePauseDuringGenerationPreventsSending(t *testing.T) {
	s, f, g := setupPrivate(t)
	now := proactiveClock(8, 0)
	s.ProactiveNow = func() time.Time { return now }
	s.ExpressionDraw = func(n int) int { return 0 }
	enableProactive(t, s)
	if err := s.ProactiveTick(ctx); err != nil {
		t.Fatal(err)
	}
	now = proactiveClock(9, 0)
	g.before = func() {
		input := model.RobotControlInput{Action: "pause"}
		if _, err := s.ControlRobot(ctx, &input); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ProactiveTick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.recipients) != 0 {
		t.Fatal("paused generation sent")
	}
}

func TestProactiveDisableAfterPlanningCancelsQueuedGreetings(t *testing.T) {
	s, f, g := setupPrivate(t)
	now := proactiveClock(8, 0)
	s.ProactiveNow = func() time.Time { return now }
	s.ExpressionDraw = func(n int) int { return 0 }
	enableProactive(t, s)
	if err := s.ProactiveTick(ctx); err != nil {
		t.Fatal(err)
	}
	overview, err := s.ProactiveStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cfg := overview.Config
	cfg.Greetings = false
	cfg.RandomMin = 0
	cfg.RandomMax = 0
	if _, err = s.SaveProactiveConfig(ctx, &cfg); err != nil {
		t.Fatal(err)
	}
	now = proactiveClock(9, 30)
	if err = s.ProactiveTick(ctx); err != nil {
		t.Fatal(err)
	}
	if g.count() != 0 || len(f.recipients) != 0 {
		t.Fatal("disabled greeting still generated or sent")
	}
}

type materialReaderFixture struct{ calls int }

func (r *materialReaderFixture) ReadVideo(_ context.Context, input *model.VideoReadInput) (*model.MaterialExperienceInput, error) {
	r.calls++
	signal := model.CharacterSignal{Mood: "curious", Intensity: 20, Curiosity: 1}
	usage := model.TokenUsage{Provider: "codex", Model: "fixture", RequestID: model.ID(), Input: 100, Output: 20}
	return &model.MaterialExperienceInput{Platform: "bilibili", SourceID: input.Video.BVID, Title: "狗的材料", Summary: "视频材料介绍狗与犬类生活，作者的观点未核实。", Memory: "读到狗的材料，只有抽帧和OCR。", Reaction: "我还想了解狗的真实照料需要。", FrameCount: 30, Signal: &signal, Interests: []model.InterestSignal{{Topic: "狗", Delta: 1}}, Usage: []model.TokenUsage{usage}}, nil
}
func TestFullVideoBrowsingDuringQuietStoresAndQueuesOnceWithoutDuplicateActivity(t *testing.T) {
	s, private, g := setupPrivate(t)
	f := &browsingBili{autoBili: private.autoBili}
	s.Bili = f
	s.ProactiveNow = func() time.Time { return proactiveClock(0, 30) }
	enableProactive(t, s)
	reader := &materialReaderFixture{}
	s.VideoReader = reader
	config := model.BrowsingConfig{Enabled: true, Mode: "custom", Keywords: []string{"狗"}, Excluded: []string{}, IntervalSeconds: 300, DailyLimit: 10, MaxDurationMinutes: 10}
	if _, err := s.SaveBrowsing(ctx, &config); err != nil {
		t.Fatal(err)
	}
	if err := s.BrowseNow(ctx); err != nil {
		t.Fatal(err)
	}
	proactive, err := s.ProactiveStatus(ctx)
	if err != nil || len(proactive.State.Jobs) != 1 || proactive.State.Jobs[0].Kind != "video" || !proactive.State.Jobs[0].ParentOperation {
		t.Fatal(proactive, err)
	}
	due, _ := time.Parse(time.RFC3339Nano, proactive.State.Jobs[0].DueAt)
	if due.In(proactiveClock(0, 0).Location()).Hour() != 9 {
		t.Fatal("quiet video notification was not deferred", due)
	}
	opID := proactive.State.Jobs[0].OperationID
	ops, err := s.Store.Operations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, op := range ops {
		if op.Action == "browsing-read" {
			t.Fatal("extra browsing activity for understood video", op)
		}
		if op.Action == "material-understood" {
			found++
			if op.ID != opID || op.NotificationStatus != "queued" || len(op.Usage) != 1 {
				t.Fatal("video queue split or lost usage", op)
			}
		}
	}
	if found != 1 {
		t.Fatal("multiple activities for one video", ops)
	}
	if err = s.BrowseNow(ctx); err != nil {
		t.Fatal(err)
	}
	if reader.calls != 1 || g.count() != 0 || f.counts()["private-send"] != 0 {
		t.Fatal("duplicate video or extra generation/send", reader.calls, g.count())
	}
	s.ProactiveNow = func() time.Time { return proactiveClock(9, 30) }
	if err = s.ProactiveTick(ctx); err != nil {
		t.Fatal(err)
	}
	ops, err = s.Store.Operations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	materialCount := 0
	for _, op := range ops {
		if op.Action == "material-understood" && op.Status != "skipped" {
			materialCount++
			if op.NotificationStatus != "sent" || op.Timings == nil || op.Timings.AccountCheckMS == nil || op.Timings.SendMS == nil || len(op.Usage) != 1 {
				t.Fatal("delivery stages or unique usage missing", op)
			}
		}
	}
	if materialCount != 1 || f.counts()["private-send"] != 1 || g.count() != 0 {
		t.Fatal("notification split activity or generated again", materialCount, g.count())
	}
}

func TestQuietHoursStillRepliesToOwnerIncomingMessage(t *testing.T) {
	s, f, g := setupPrivate(t)
	s.ProactiveNow = func() time.Time { return proactiveClock(0, 15) }
	enableProactive(t, s)
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "旧消息")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	f.messages = append(f.messages, privateMessage(2, "789", "123", "小龙，你在吗，想聊聊天"))
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if g.count() != 1 || len(f.recipients) != 1 || f.recipients[0] != "789" {
		t.Fatal("quiet hours blocked normal owner reply", g.count(), f.recipients)
	}
	status, err := s.PrivateOverview(ctx)
	if err != nil || len(status.State.Outbox) != 1 || status.State.Outbox[0].Proactive {
		t.Fatal("owner reply mislabeled proactive", err)
	}
}

type blockingMaterialReader struct {
	started, release chan struct{}
	materialReaderFixture
}

func (r *blockingMaterialReader) ReadVideo(ctx context.Context, input *model.VideoReadInput) (*model.MaterialExperienceInput, error) {
	close(r.started)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.release:
		return r.materialReaderFixture.ReadVideo(ctx, input)
	}
}
func TestOwnerReplyNotBlockedByQuietVideoReading(t *testing.T) {
	s, private, g := setupPrivate(t)
	s.ProactiveNow = func() time.Time { return proactiveClock(0, 30) }
	enableProactive(t, s)
	reader := &blockingMaterialReader{started: make(chan struct{}), release: make(chan struct{})}
	s.VideoReader = reader
	s.Bili = &browsingBili{autoBili: private.autoBili}
	config := model.BrowsingConfig{Enabled: true, Mode: "custom", Keywords: []string{"狗"}, Excluded: []string{}, IntervalSeconds: 300, DailyLimit: 10, MaxDurationMinutes: 10}
	if _, err := s.SaveBrowsing(ctx, &config); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- s.BrowseNow(ctx) }()
	select {
	case <-reader.started:
	case <-time.After(3 * time.Second):
		t.Fatal("video did not start")
	}
	s.Bili = private
	private.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "旧消息")}
	if err := s.PrivateTick(ctx); err != nil {
		close(reader.release)
		t.Fatal(err)
	}
	private.messages = append(private.messages, privateMessage(2, "789", "123", "小龙，现在想和你说说话"))
	if err := s.PrivateTick(ctx); err != nil {
		close(reader.release)
		t.Fatal(err)
	}
	if g.count() != 1 || len(private.recipients) != 1 {
		close(reader.release)
		t.Fatal("video blocked owner reply", g.count(), private.recipients)
	}
	close(reader.release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

// invalidMaterialReader 保留真实用量协议，只模拟落库前材料校验失败。
type invalidMaterialReader struct{ materialReaderFixture }

func (r *invalidMaterialReader) ReadVideo(ctx context.Context, input *model.VideoReadInput) (*model.MaterialExperienceInput, error) {
	result, err := r.materialReaderFixture.ReadVideo(ctx, input)
	if result != nil {
		result.Summary = strings.Repeat("字", 351)
	}
	return result, err
}

func TestVideoValidationFailureKeepsUsageInSingleActivity(t *testing.T) {
	s, private, _ := setupPrivate(t)
	s.Bili = &browsingBili{autoBili: private.autoBili}
	s.ProactiveNow = func() time.Time { return proactiveClock(0, 30) }
	enableProactive(t, s)
	s.VideoReader = &invalidMaterialReader{}
	config := model.BrowsingConfig{
		Enabled:            true,
		Mode:               "custom",
		Keywords:           []string{"狗"},
		Excluded:           []string{},
		IntervalSeconds:    300,
		DailyLimit:         10,
		MaxDurationMinutes: 10,
	}
	if _, err := s.SaveBrowsing(ctx, &config); err != nil {
		t.Fatal(err)
	}
	if err := s.BrowseNow(ctx); err == nil {
		t.Fatal("invalid material accepted")
	}
	ops, err := s.Store.Operations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, op := range ops {
		if op.Action == "browsing-read" {
			t.Fatal("duplicate metadata activity", op)
		}
		if op.Action == "material-understood" {
			found++
			if op.Status != "failed" || op.ErrorCode != "INPUT" || len(op.Usage) != 1 || op.Usage[0].Input != 100 {
				t.Fatal("failed material lost native usage", op)
			}
		}
	}
	if found != 1 {
		t.Fatal("expected single material activity", ops)
	}
}

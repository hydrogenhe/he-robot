package core_test

import (
	"context"
	"gpt-robot/core"
	"gpt-robot/model"
	"strings"
	"testing"
	"time"
)

type selfQueryGenerator struct {
	action  model.SelfAction
	prompts []model.ReplyPrompt
	before  func()
}

func (g *selfQueryGenerator) Generate(_ context.Context, prompt *model.ReplyPrompt) (*model.GeneratedReply, error) {
	g.prompts = append(g.prompts, *prompt)
	if g.before != nil {
		g.before()
	}
	usage := &model.TokenUsage{
		Provider:  "codex",
		Model:     "fixture",
		RequestID: model.ID(),
		Input:     100,
		Output:    20,
	}
	result := &model.GeneratedReply{
		Text:  "还不能确定，先查一下。",
		Usage: usage,
	}
	if prompt.SelfResult == nil {
		action := g.action
		result.SelfAction = &action
	} else {
		result.Text = "我查到自己的实际记录啦。"
	}
	return result, nil
}

func selfQueryService(t *testing.T, action *model.SelfAction) (*core.Service, *privateBili, *selfQueryGenerator) {
	t.Helper()
	s, f, _ := setupPrivate(t)
	g := &selfQueryGenerator{action: *action}
	s.Generator = g
	s.ExpressionDraw = func(int) int { return 0 }
	s.ExpressionWait = func(context.Context, time.Duration) error { return nil }
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	return s, f, g
}

func TestSelfDiaryStatusQueriesRealDataAndSharesReplyUsage(t *testing.T) {
	action := model.SelfAction{
		Action:   "query_self",
		Resource: "diary",
		Date:     "today",
	}
	s, f, g := selfQueryService(t, &action)
	entry := model.DiaryEntry{
		Date:   "2026-11-03",
		Status: "completed",
		Title:  "今天",
		Body:   "这是真正保存的日记。",
	}
	s.DiaryNow = func() time.Time { return proactiveClock(1, 0) }
	if err := s.Store.Write(ctx, "data/private/diary/2026-11-03.json", &entry); err != nil {
		t.Fatal(err)
	}
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "昨晚说的那个，你今天写好了没？")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(g.prompts) != 2 || len(g.prompts[0].SelfCapabilities) < 6 || g.prompts[1].SelfResult == nil || g.prompts[1].SelfResult.Records[0].Status != "completed" || len(g.prompts[1].SelfCapabilities) != 0 {
		t.Fatal("missing live query or query bound", g.prompts)
	}
	operations, err := s.Store.Operations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range operations {
		if op.Action == "private-auto-reply" && len(op.Usage) != 2 {
			t.Fatal("query generation usage not attached to same reply", op)
		}
	}
	if len(f.recipients) != 1 {
		t.Fatal("query sent extra messages", f.recipients)
	}
}

func TestSelfDiaryFullTextDeliveredWithoutModelRewriteDuringQuiet(t *testing.T) {
	action := model.SelfAction{
		Action:   "query_self",
		Resource: "diary",
		Date:     "today",
		Full:     true,
	}
	s, f, g := selfQueryService(t, &action)
	s.DiaryNow = func() time.Time { return proactiveClock(1, 0) }
	s.ProactiveNow = s.DiaryNow
	entry := model.DiaryEntry{
		Date:   "2026-11-03",
		Status: "completed",
		Title:  "小小记录",
		Body:   strings.Repeat("这是我保存在本机的真实记录。", 40),
	}
	if err := s.Store.Write(ctx, "data/private/diary/2026-11-03.json", &entry); err != nil {
		t.Fatal(err)
	}
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "那篇拿来给我看看")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := s.PrivateOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for _, out := range status.State.Outbox {
		if out.Proactive || out.Status != "sent" || len([]rune(out.Text)) > 450 {
			t.Fatal("explicit diary reply wrongly quieted or split", out)
		}
		body.WriteString(out.Text)
	}
	if body.String() != entry.Date+"《"+entry.Title+"》\n"+entry.Body || len(g.prompts) != 1 || len(f.recipients) != 2 {
		t.Fatal("diary not delivered verbatim", body.String(), len(g.prompts), len(f.recipients))
	}
}

func TestSelfWriteDiaryUsesSharedGenerationAndDoesNotQueueDuplicate(t *testing.T) {
	action := model.SelfAction{
		Action:   "write_diary",
		Resource: "diary",
		Date:     "today",
	}
	s, f, _ := selfQueryService(t, &action)
	s.DiaryNow = func() time.Time { return proactiveClock(1, 0) }
	enableProactive(t, s)
	diary := &diaryGenerator{}
	s.DiaryGenerator = diary
	if _, err := s.Store.Event(ctx, "comments", "读到有趣的公开讨论", nil); err != nil {
		t.Fatal(err)
	}
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "现在把今天记下来，写好给我看")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := s.DiaryOverview(ctx)
	if err != nil || page.TodayStatus != "completed" || diary.count() != 1 || len(f.recipients) == 0 {
		t.Fatal("shared diary write failed", page, err, diary.count())
	}
	proactive, err := s.ProactiveStatus(ctx)
	if err != nil || len(proactive.State.Jobs) != 0 {
		t.Fatal("explicit diary reply also queued unsolicited duplicate", proactive, err)
	}
}

func TestSelfQueryResourcesBoundedAndUnknownResourceDoesNotReadFile(t *testing.T) {
	for _, resource := range []string{"memories", "experiences", "growth", "activities", "status", "../.private/bilibili.json"} {
		t.Run(resource, func(t *testing.T) {
			action := model.SelfAction{
				Action:   "query_self",
				Resource: resource,
				Query:    "植物",
			}
			s, f, g := selfQueryService(t, &action)
			f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "你自己查查记录再告诉我")}
			if err := s.PrivateTick(ctx); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(resource, "/") {
				if len(g.prompts) != 1 {
					t.Fatal("unsupported resource queried")
				}
			} else if resource == "memories" {
				if len(g.prompts) != 1 {
					t.Fatal("disabled private memory queried")
				}
			} else if len(g.prompts) != 2 || g.prompts[1].SelfResult == nil || len(g.prompts[1].SelfResult.Records) > 5 {
				t.Fatal("missing controlled result", g.prompts)
			}
		})
	}
}

func TestSelfQueryNotExecutedAfterOwnerBindingChanges(t *testing.T) {
	action := model.SelfAction{
		Action:   "query_self",
		Resource: "diary",
		Full:     true,
	}
	s, f, g := selfQueryService(t, &action)
	g.before = func() {
		input := model.PrivateSettings{
			Enabled:         false,
			AllowSend:       false,
			AutoReply:       false,
			IntervalSeconds: 120,
		}
		if _, err := s.SavePrivateConfig(ctx, &input); err != nil {
			t.Fatal(err)
		}
	}
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "给我看你的日记")}
	_ = s.PrivateTick(ctx)
	if len(f.recipients) != 0 {
		t.Fatal("changed binding received diary", f.recipients)
	}
}

func TestSelfWritePreservesCompletedDiaryEvenInTestingMode(t *testing.T) {
	action := model.SelfAction{
		Action:   "write_diary",
		Resource: "diary",
		Date:     "today",
	}
	s, f, _ := selfQueryService(t, &action)
	s.DiaryNow = func() time.Time { return proactiveClock(1, 0) }
	testingMode := true
	settings := model.DiarySettings{
		Enabled: true,
		Testing: &testingMode,
	}
	if _, err := s.SaveDiaryConfig(ctx, &settings); err != nil {
		t.Fatal(err)
	}
	diary := &diaryGenerator{}
	s.DiaryGenerator = diary
	entry := model.DiaryEntry{
		Date:   "2026-11-03",
		Status: "completed",
		Title:  "原来的日记",
		Body:   "这篇已经保存成功，不应被普通聊天重复生成覆盖。",
	}
	if err := s.Store.Write(ctx, "data/private/diary/2026-11-03.json", &entry); err != nil {
		t.Fatal(err)
	}
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "写今天的日记给我看看")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := s.DiaryOverview(ctx)
	if err != nil || diary.count() != 0 || len(page.Entries) != 1 || page.Entries[0].Body != entry.Body {
		t.Fatal("chat overwrote completed diary", page, err, diary.count())
	}
}

func TestSelfWriteDoesNotRetryFailedDiaryOrInventCompletion(t *testing.T) {
	action := model.SelfAction{
		Action:   "write_diary",
		Resource: "diary",
		Date:     "today",
	}
	s, f, _ := selfQueryService(t, &action)
	s.DiaryNow = func() time.Time { return proactiveClock(1, 0) }
	diary := &diaryGenerator{}
	s.DiaryGenerator = diary
	entry := model.DiaryEntry{
		Date:      "2026-11-03",
		Status:    "failed",
		ErrorCode: "TIMEOUT",
	}
	if err := s.Store.Write(ctx, "data/private/diary/2026-11-03.json", &entry); err != nil {
		t.Fatal(err)
	}
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "现在把日记写好给我")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := s.PrivateOverview(ctx)
	if err != nil || diary.count() != 0 || len(page.State.Outbox) != 1 || !strings.Contains(page.State.Outbox[0].Text, "没有完成") {
		t.Fatal("failed diary retried or claimed success", page, err, diary.count())
	}
}

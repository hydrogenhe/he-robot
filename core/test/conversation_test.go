package core_test

import (
	"encoding/json"
	"gpt-robot/core"
	"gpt-robot/model"
	"strings"
	"testing"
	"time"
)

func TestConversationPolicyPersistsWithoutGenerationOrSending(t *testing.T) {
	service, bili, generator := setupPrivate(t)
	policy := model.DefaultConversationConfig()
	policy.MaxTurns = 5
	policy.IdleSeconds = 300
	if _, err := service.SaveConversationConfig(ctx, &policy); err != nil {
		t.Fatal(err)
	}
	policy.MaxTurns = 0
	if _, err := service.SaveConversationConfig(ctx, &policy); err == nil {
		t.Fatal("invalid config saved")
	}
	restored := core.NewService(service.Store.Root, bili)
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := restored.ConversationStatus(ctx)
	if err != nil || status.Config.MaxTurns != 5 || status.Config.IdleSeconds != 300 || generator.count() != 0 || len(bili.recipients) != 0 {
		t.Fatal(status, err)
	}
}

func TestPrivateConversationHandoffIsLocalOnlyAndSentOnly(t *testing.T) {
	service, bili, generator := setupPrivate(t)
	enablePrivateMemory(t, service, true)
	if err := service.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	bili.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "我喜欢阳台植物")}
	if err := service.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	files, err := service.Store.Database.ListDocuments(ctx, "data/private/conversations/", 20)
	if err != nil || len(files) != 1 {
		t.Fatal("handoff not persisted", files, err)
	}
	// 重启后只读取本地交接，不靠复活供应商线程，也不导入其他对话。
	restored := core.NewService(service.Store.Root, bili)
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	restored.Generator = generator
	var saved model.PrivateState
	if _, err := restored.Store.Read(ctx, "data/private/state.json", &saved); err != nil {
		t.Fatal(err)
	}
	for i := range saved.Outbox {
		saved.Outbox[i].At = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	}
	saved.NextAt = ""
	if err := restored.Store.Write(ctx, "data/private/state.json", &saved); err != nil {
		t.Fatal(err)
	}
	restored = core.NewService(service.Store.Root, bili)
	restored.Generator = generator
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	secondReply := "好的，我们接着聊植物呀"
	generator.text = &secondReply
	bili.messages = append(bili.messages, privateMessage(2, "789", "123", "我们刚刚聊了什么呀"))
	if err := restored.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if generator.count() != 2 || len(bili.recipients) != 2 {
		t.Fatal("extra calls or fake sends", generator.count(), len(bili.recipients))
	}
	last := generator.prompts[len(generator.prompts)-1]
	if last.Conversation == nil || last.Conversation.Account != "123" || last.Conversation.Peer != "789" || !strings.Contains(strings.Join(last.Handoff, " "), "我喜欢阳台植物") {
		t.Fatal("scoped local handoff missing", last.Handoff)
	}
	public, err := restored.Context(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(public)
	if strings.Contains(string(raw), "我喜欢阳台植物") {
		t.Fatal("private handoff leaked into public context")
	}
}

func TestFailedPrivateSendDoesNotSaveHandoff(t *testing.T) {
	service, bili, generator := setupPrivate(t)
	enablePrivateMemory(t, service, true)
	if err := service.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	bili.sendError = model.Fail("NETWORK", "模拟发送结果未知")
	bili.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "我喜欢阳台植物")}
	if err := service.PrivateTick(ctx); err == nil {
		t.Fatal("expected failed send")
	}
	files, _ := service.Store.Database.ListDocuments(ctx, "data/private/conversations/", 20)
	if len(files) != 0 || generator.count() != 1 || len(bili.recipients) != 1 {
		t.Fatal("failed send persisted or retried", files, generator.count(), len(bili.recipients))
	}
}

func TestForgetDuringGenerationCannotResurrectHandoff(t *testing.T) {
	service, bili, generator := setupPrivate(t)
	enablePrivateMemory(t, service, true)
	if err := service.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	bili.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "我喜欢阳台植物")}
	generator.before = func() {
		page := memoryPage(t, service, "")
		if len(page.Items) != 1 {
			t.Fatal("expected sourced user statement")
		}
		if err := service.ForgetPrivateMemory(ctx, page.Items[0].ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.PrivateTick(ctx); err == nil {
		t.Fatal("forgotten context was sent")
	}
	if len(bili.recipients) != 0 {
		t.Fatal("sent stale generated reply")
	}
	files, _ := service.Store.Database.ListDocuments(ctx, "data/private/conversations/", 20)
	if len(files) != 1 {
		t.Fatal("missing cleared marker")
	}
	raw := files[0]
	if strings.Contains(string(raw), "阳台植物") {
		t.Fatal("forgotten memory was resurrected by pending send")
	}
}

package core_test

import (
	"gpt-robot/core"
	"gpt-robot/model"
	"testing"
	"time"
)

func expirePrivateBackoff(t *testing.T, service *core.Service, f *privateBili, generator *fakeGenerator) *core.Service {
	t.Helper()
	var state model.PrivateState
	if _, err := service.Store.Read(ctx, "data/private/state.json", &state); err != nil {
		t.Fatal(err)
	}
	state.NextAt = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
	if err := service.Store.Write(ctx, "data/private/state.json", &state); err != nil {
		t.Fatal(err)
	}
	restored := core.NewService(service.Store.Root, f)
	restored.Generator = generator
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	return restored
}
func TestReadNetworkFailureBacksOffWithoutPausingAIOrRetryingModel(t *testing.T) {
	service, f, generator := setupPrivate(t)
	f.readError = model.Fail("NETWORK", "模拟只读网络错误")
	if err := service.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := service.PrivateOverview(ctx)
	if err != nil || state.State.Paused || state.State.ReadPaused || state.State.ReadFailures != 1 || state.State.LastErrorStage != "read" {
		t.Fatal(state, err)
	}
	calls := f.counts()["private-read"]
	f.readError = nil
	if err := service.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if f.counts()["private-read"] != calls || generator.count() != 0 {
		t.Fatal("backoff queried early or called model")
	}
	service = expirePrivateBackoff(t, service, f, generator)
	if err := service.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	state, err = service.PrivateOverview(ctx)
	if err != nil || state.State.ReadFailures != 0 || state.State.Paused || state.State.LastErrorCode != "" || generator.count() != 0 {
		t.Fatal("read recovery not clean", state, err)
	}
}
func TestReadRetriesAreBoundedEvenWhenMemoryIsEnabled(t *testing.T) {
	service, f, generator := setupPrivate(t)
	enablePrivateMemory(t, service, true)
	f.readError = model.Fail("NETWORK", "模拟只读网络错误")
	for i := range 5 {
		if err := service.PrivateTick(ctx); err != nil {
			t.Fatal(err)
		}
		state, err := service.PrivateOverview(ctx)
		if err != nil || state.State.ReadFailures != i+1 {
			t.Fatal(state, err)
		}
		if i < 4 {
			service = expirePrivateBackoff(t, service, f, generator)
		}
	}
	state, _ := service.PrivateOverview(ctx)
	if !state.State.ReadPaused || !state.State.Paused {
		t.Fatal("five failures did not stop reads", state)
	}
	calls := f.counts()["private-read"]
	if err := service.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if f.counts()["private-read"] != calls || generator.count() != 0 || len(f.recipients) != 0 {
		t.Fatal("terminal read pause retried")
	}
}
func TestResumeUsesLatestWatermarkAndOnlyNewMessageIsRepliedOnce(t *testing.T) {
	service, f, generator := setupPrivate(t)
	// 登录错误应保持安全暂停，不能作为普通网络重试。
	f.readError = model.Fail("LOGIN", "模拟登录失效")
	if err := service.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	state, _ := service.PrivateOverview(ctx)
	if !state.State.Paused || !state.State.ReadPaused || state.State.LastErrorCode != "LOGIN" {
		t.Fatal(state)
	}
	f.readError = nil
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "之前失败的旧消息"), privateMessage(2, "789", "123", "暂停期间的旧消息")}
	if _, err := service.ResumePrivateReplies(ctx); err != nil {
		t.Fatal(err)
	}
	state, _ = service.PrivateOverview(ctx)
	if state.State.Cursor != 2 || state.State.Paused || state.State.ReadPaused || generator.count() != 0 || len(f.recipients) != 0 {
		t.Fatal("resume sent or kept old watermark", state)
	}
	if err := service.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if generator.count() != 0 {
		t.Fatal("old message invoked AI")
	}
	f.messages = append(f.messages, privateMessage(3, "789", "123", "你好呀"))
	if err := service.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if generator.count() != 1 || len(f.recipients) != 1 {
		t.Fatal("new message missing or repeated", generator.count(), f.recipients)
	}
}
func TestResumeRejectsUnknownSendBeforeQueryingPlatform(t *testing.T) {
	service, f, _ := setupPrivate(t)
	f.sendError = model.Fail("SEND_UNKNOWN", "模拟发送结果未知")
	send := model.PrivateSendInput{RequestID: model.ID(), Text: "已尝试的消息"}
	if _, err := service.SendPrivate(ctx, &send); err == nil {
		t.Fatal("expected unknown")
	}
	calls := f.counts()["status"]
	if _, err := service.ResumePrivateReplies(ctx); err == nil {
		t.Fatal("unknown bypassed")
	}
	if len(f.recipients) != 1 || f.counts()["status"] != calls {
		t.Fatal("unknown retried or queried")
	}
}

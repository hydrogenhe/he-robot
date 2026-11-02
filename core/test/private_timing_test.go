package core_test

import (
	"context"
	"encoding/json"
	"gpt-robot/core"
	"gpt-robot/http/handler"
	"gpt-robot/model"
	"testing"
	"time"
)

type timedPrivateBili struct{ *privateBili }

func (f *timedPrivateBili) Status(ctx context.Context) (model.Account, error) {
	time.Sleep(20 * time.Millisecond)
	return f.privateBili.Status(ctx)
}

func (f *timedPrivateBili) SendPrivate(ctx context.Context, sender, peer, text, request string) (string, error) {
	time.Sleep(30 * time.Millisecond)
	return f.privateBili.SendPrivate(ctx, sender, peer, text, request)
}

func delayPrivateGeneration() { time.Sleep(40 * time.Millisecond) }

func TestPrivateTimingsPersistAndExposeOnlyExecutedStagesWithoutExtraCalls(t *testing.T) {
	service, base, generator := setupPrivate(t)
	if err := service.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	before := base.counts()
	service.Bili = &timedPrivateBili{privateBili: base}
	generator.before = delayPrivateGeneration
	base.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "你好呀")}
	if err := service.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	action := "private-auto-reply"
	query := model.OperationQuery{Action: &action}
	page, err := service.QueryOperations(ctx, &query)
	if err != nil || len(page.Items) != 1 {
		t.Fatal("missing operation", err)
	}
	op := page.Items[0]
	timing := op.Timings
	if timing == nil || timing.GenerationMS == nil || timing.AccountCheckMS == nil || timing.SendMS == nil {
		t.Fatal("completed stages missing")
	}
	if *timing.GenerationMS < 40 || *timing.AccountCheckMS < 20 || *timing.SendMS < 30 || timing.AccountAttempts != 1 || *timing.GenerationMS+*timing.AccountCheckMS+*timing.SendMS > op.DurationMS {
		t.Fatal("incorrect stage boundaries", timing, op.DurationMS)
	}
	after := base.counts()
	if generator.count() != 1 || after["status"]-before["status"] != 1 || after["private-send"]-before["private-send"] != 1 || after["private-read"]-before["private-read"] != 1 {
		t.Fatal("timing increased model or platform calls", before, after, generator.count())
	}
	// 通过真实网页业务入口检查白名单响应，且查询输出不能改写存储中的计时。
	actions := handler.NewActions(service)
	response, err := actions.Execute(ctx, "operations", json.RawMessage(`{"action":"private-auto-reply"}`), true)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Items []struct {
			Timings *model.OperationTimings `json:"timings"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || len(wire.Items) != 1 || wire.Items[0].Timings == nil || *wire.Items[0].Timings.SendMS != *timing.SendMS {
		t.Fatal("timing response missing", err)
	}
	storedGeneration := *timing.GenerationMS
	*timing.GenerationMS = 999999
	restored := core.NewService(service.Store.Root, base)
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	page, err = restored.QueryOperations(ctx, &query)
	if err != nil || len(page.Items) != 1 || page.Items[0].Timings == nil || *page.Items[0].Timings.GenerationMS != storedGeneration {
		t.Fatal("timing lost across restart", err)
	}
}

type failingPrivateGenerator struct{ calls int }

func (g *failingPrivateGenerator) Generate(context.Context, *model.ReplyPrompt) (*model.GeneratedReply, error) {
	g.calls++
	return nil, model.Fail("AI_REPLY", "模拟生成失败")
}

func TestPrivateGenerationFailureDoesNotInventUnexecutedTimings(t *testing.T) {
	service, base, _ := setupPrivate(t)
	if err := service.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	generator := &failingPrivateGenerator{}
	service.Generator = generator
	base.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "你好呀")}
	if err := service.PrivateTick(ctx); err == nil {
		t.Fatal("expected generation failure")
	}
	action := "private-auto-reply"
	query := model.OperationQuery{Action: &action}
	page, err := service.QueryOperations(ctx, &query)
	if err != nil || len(page.Items) != 1 || page.Items[0].Timings == nil {
		t.Fatal("missing failed generation timing", err)
	}
	timing := page.Items[0].Timings
	if timing.GenerationMS == nil || timing.AccountCheckMS != nil || timing.SendMS != nil || timing.AccountAttempts != 0 || generator.calls != 1 || len(base.recipients) != 0 {
		t.Fatal("unexecuted stages invented or generation retried")
	}
}

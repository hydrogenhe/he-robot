package core_test

import (
	"context"
	"gpt-robot/model"
	"testing"
)

type preflightBili struct {
	*privateBili
	failures int
	err      error
	checks   int
	uid      string
	cancel   context.CancelFunc
}

func (f *preflightBili) Status(context.Context) (model.Account, error) {
	f.checks++
	if f.cancel != nil {
		f.cancel()
	}
	if f.checks <= f.failures {
		return model.Account{}, f.err
	}
	account := model.Account{UID: f.uid}
	return account, nil
}

func TestPrivatePreflightRetriesOnlyReadWithoutRegeneratingOrResending(t *testing.T) {
	cases := []struct {
		name     string
		code     string
		failures int
		uid      string
		checks   int
		sends    int
	}{
		{name: "transient", code: "NETWORK", failures: 1, uid: "123", checks: 2, sends: 1},
		{name: "bounded", code: "NETWORK", failures: 9, uid: "123", checks: 3},
		{name: "login", code: "LOGIN", failures: 1, uid: "123", checks: 1},
		{name: "risk", code: "PAUSED", failures: 1, uid: "123", checks: 1},
		{name: "account", uid: "456", checks: 1},
	}
	for _, row := range cases {
		t.Run(row.name, func(t *testing.T) {
			service, base, generator := setupPrivate(t)
			if err := service.PrivateTick(ctx); err != nil {
				t.Fatal(err)
			}
			fake := &preflightBili{
				privateBili: base,
				failures:    row.failures,
				err:         model.Fail(row.code, "模拟核验失败"),
				uid:         row.uid,
			}
			service.Bili = fake
			base.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "你好呀")}
			err := service.PrivateTick(ctx)
			if (err == nil) != (row.sends == 1) {
				t.Fatal("unexpected tick result", err)
			}
			state, stateErr := service.PrivateOverview(ctx)
			if stateErr != nil || fake.checks != row.checks || generator.count() != 1 || len(base.recipients) != row.sends || len(state.State.Outbox) != row.sends {
				t.Fatal("incorrect checks/model/sends", stateErr, fake.checks, generator.count(), len(base.recipients))
			}
			if row.sends == 0 && (!state.State.Paused || state.State.LastErrorStage != "send-preflight") {
				t.Fatal("missing preflight pause diagnostic", state.State.LastErrorStage)
			}
			action := "private-auto-reply"
			query := model.OperationQuery{Action: &action}
			page, queryErr := service.QueryOperations(ctx, &query)
			if queryErr != nil || len(page.Items) != 1 || page.Items[0].Timings == nil {
				t.Fatal("missing timing", queryErr)
			}
			timing := page.Items[0].Timings
			if timing.GenerationMS == nil || timing.AccountCheckMS == nil || timing.AccountAttempts != row.checks || (timing.SendMS != nil) != (row.sends == 1) {
				t.Fatal("incorrect executed stages", timing)
			}
			if row.failures > 0 && row.code == "NETWORK" && *timing.AccountCheckMS < 1000 {
				t.Fatal("read backoff excluded from timing")
			}
			if err := service.PrivateTick(ctx); err != nil {
				t.Fatal(err)
			}
			if fake.checks != row.checks || generator.count() != 1 || len(base.recipients) != row.sends {
				t.Fatal("later tick repeated work")
			}
		})
	}
}

func TestPrivatePreflightCancellationStopsBeforeRetryOrSend(t *testing.T) {
	service, base, _ := setupPrivate(t)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	fake := &preflightBili{
		privateBili: base,
		failures:    9,
		err:         model.Fail("NETWORK", "模拟网络失败"),
		cancel:      cancel,
	}
	service.Bili = fake
	input := model.PrivateSendInput{RequestID: model.ID(), Text: "测试回复"}
	if _, err := service.SendPrivate(runCtx, &input); err == nil {
		t.Fatal("cancelled check accepted")
	}
	if fake.checks != 1 || len(base.recipients) != 0 {
		t.Fatal("cancelled preflight retried or sent")
	}
}

func TestPrivateUnknownSendIsNotRetriedAfterSuccessfulPreflight(t *testing.T) {
	service, base, generator := setupPrivate(t)
	if err := service.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	base.sendError = model.Fail("SEND_UNKNOWN", "模拟回执丢失")
	base.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "你好呀")}
	if err := service.PrivateTick(ctx); err == nil {
		t.Fatal("unknown send accepted")
	}
	state, err := service.PrivateOverview(ctx)
	if err != nil || !state.State.Paused || state.State.LastErrorStage != "send" || len(state.State.Outbox) != 1 || state.State.Outbox[0].Status != "unknown" {
		t.Fatal("unknown send protection lost", err)
	}
	if err := service.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if generator.count() != 1 || len(base.recipients) != 1 {
		t.Fatal("unknown send retried")
	}
}

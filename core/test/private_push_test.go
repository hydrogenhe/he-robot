package core_test

import (
	"context"
	"gpt-robot/core"
	"gpt-robot/model"
	"slices"
	"sync"
	"testing"
	"time"
)

type privatePushFixture struct {
	failure chan error
	events  chan model.PrivatePushEvent
	closed  chan struct{}
}

var _ model.PrivatePushSource = (*privatePushFixture)(nil)

func (p *privatePushFixture) WatchPrivate(ctx context.Context, _, _ string, notify func(*model.PrivatePushEvent)) error {
	defer close(p.closed)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-p.failure:
			return err
		case event := <-p.events:
			notify(&event)
		}
	}
}

type pushReadBili struct {
	*privateBili
	rowsMu sync.Mutex
	rows   []model.PrivateMessage
	reads  int
}

func (f *pushReadBili) PrivateMessages(context.Context, string, string) (*model.PrivatePage, error) {
	f.rowsMu.Lock()
	defer f.rowsMu.Unlock()
	f.reads++
	result := &model.PrivatePage{Messages: slices.Clone(f.rows)}
	return result, nil
}
func TestPushWakesBeforeLongPollDeduplicatesAndStopsOnPause(t *testing.T) {
	for _, mode := range []string{"direct", "hint"} {
		t.Run(mode, func(t *testing.T) { testPushWake(t, mode == "hint") })
	}
}
func testPushWake(t *testing.T, generic bool) {
	s, base, g := setupPrivate(t)
	enablePrivateMemory(t, s, true)
	settings := model.PrivateSettings{
		Enabled:         true,
		AllowSend:       true,
		AutoReply:       true,
		IntervalSeconds: 15,
	}
	if _, err := s.SavePrivateConfig(ctx, &settings); err != nil {
		t.Fatal(err)
	}
	f := &pushReadBili{privateBili: base}
	s.Bili = f
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	source := &privatePushFixture{
		events: make(chan model.PrivatePushEvent, 8),
		closed: make(chan struct{}),
	}
	s.PrivatePush = source
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go runPushAutomation(s, run, done)
	source.events <- model.PrivatePushEvent{Connected: true}
	waitPushReads(t, f, 2)
	unverified, _ := s.PrivateOverview(ctx)
	lastBaseline, _ := time.Parse(time.RFC3339Nano, unverified.State.LastAt)
	nextBaseline, _ := time.Parse(time.RFC3339Nano, unverified.State.NextAt)
	if unverified.Push.Verified || nextBaseline.Sub(lastBaseline) > 16*time.Second {
		t.Fatal("connected without notification incorrectly disabled short fallback")
	}
	f.rowsMu.Lock()
	f.rows = []model.PrivateMessage{privateMessage(1, "789", "123", "植物长出新叶啦")}
	f.rowsMu.Unlock()
	source.events <- model.PrivatePushEvent{
		Connected:   true,
		Wake:        true,
		Key:         "message-1",
		GenericHint: generic,
	}
	deadline := time.After(4 * time.Second)
	timer := time.NewTicker(5 * time.Millisecond)
	defer timer.Stop()
	completed := false
	for !completed {
		select {
		case <-deadline:
			t.Fatal("push did not wake before normal 120-second interval")
		case <-timer.C:
			page, err := s.PrivateOverview(ctx)
			if err != nil {
				t.Fatal(err)
			}
			completed = len(page.State.Outbox) == 1 && page.State.Processing == ""
		}
	}
	for i := 0; i < 5; i++ {
		source.events <- model.PrivatePushEvent{
			Connected:   true,
			Wake:        true,
			Key:         "message-1",
			GenericHint: generic,
		}
	}
	source.events <- model.PrivatePushEvent{Connected: true}
	time.Sleep(40 * time.Millisecond)
	f.rowsMu.Lock()
	reads := f.reads
	f.rowsMu.Unlock()
	if reads != 3 || g.count() != 1 {
		t.Fatal("duplicate or heartbeat consumed query/model", reads, g.count())
	}
	page, _ := s.PrivateOverview(ctx)
	expected := "push"
	if generic {
		expected = "push-hint"
	}
	if page.State.LastTriggerKind != expected || page.State.LastTriggerKey != "10001" {
		t.Fatal("message cause not recorded", page.State.LastTriggerKind, page.State.LastTriggerKey)
	}
	if !page.Push.Connected || !page.Push.Verified || page.Push.Mode != "push" {
		t.Fatal(page.Push)
	}
	last, _ := time.Parse(time.RFC3339Nano, page.State.LastAt)
	next, _ := time.Parse(time.RFC3339Nano, page.State.NextAt)
	if next.Sub(last) < 119*time.Second {
		t.Fatal("push did not reduce idle polling", next.Sub(last))
	}
	input := model.RobotControlInput{Action: "pause"}
	if _, err := s.ControlRobot(ctx, &input); err != nil {
		t.Fatal(err)
	}
	select {
	case <-source.closed:
	case <-time.After(time.Second):
		t.Fatal("pause did not close push")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("push worker leaked")
	}
}
func waitPushReads(t *testing.T, f *pushReadBili, count int) {
	t.Helper()
	deadline := time.After(time.Second)
	timer := time.NewTicker(5 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("baseline read missing")
		case <-timer.C:
			f.rowsMu.Lock()
			ready := f.reads >= count
			f.rowsMu.Unlock()
			if ready {
				return
			}
		}
	}
}

func runPushAutomation(s *core.Service, ctx context.Context, done chan<- struct{}) {
	defer close(done)
	s.RunAutomation(ctx)
}

func TestPushDisconnectFallsBackWithoutLosingWatermark(t *testing.T) {
	s, base, g := setupPrivate(t)
	enablePrivateMemory(t, s, true)
	settings := model.PrivateSettings{
		Enabled:         true,
		AllowSend:       true,
		AutoReply:       true,
		IntervalSeconds: 15,
	}
	if _, err := s.SavePrivateConfig(ctx, &settings); err != nil {
		t.Fatal(err)
	}
	f := &pushReadBili{privateBili: base}
	s.Bili = f
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	before, _ := s.PrivateOverview(ctx)
	source := &privatePushFixture{
		events:  make(chan model.PrivatePushEvent, 1),
		closed:  make(chan struct{}),
		failure: make(chan error, 1),
	}
	s.PrivatePush = source
	run, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go runPushAutomation(s, run, done)
	defer cancel()
	source.events <- model.PrivatePushEvent{Connected: true}
	waitPushReads(t, f, 2)
	source.failure <- model.Fail("PUSH_NETWORK", "fixture disconnected")
	deadline := time.After(time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("push did not fall back")
		case <-ticker.C:
			page, _ := s.PrivateOverview(ctx)
			if page.Push.Mode != "polling" {
				continue
			}
			next, err := time.Parse(time.RFC3339Nano, page.State.NextAt)
			if err != nil || time.Until(next) > 16*time.Second || page.Push.Connected || page.State.Cursor != before.State.Cursor || g.count() != 0 {
				t.Fatal("fallback changed safety state or missed poll deadline", page.Push, err)
			}
			cancel()
			select {
			case <-done:
				return
			case <-time.After(time.Second):
				t.Fatal("retry worker leaked")
			}
		}
	}
}

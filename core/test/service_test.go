package core_test

import (
	"context"
	"errors"
	"fmt"
	"gpt-robot/core"
	"gpt-robot/model"
	"gpt-robot/platforms/bilibili"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

var ctx = context.Background()
var bvid = "BV161nZ6JE6b"

type fakeBili struct {
	state      *int
	mu         sync.Mutex
	calls      map[string]int
	uid        string
	targetText string
	unknown    bool
	block      chan struct{}
	entered    chan struct{}
}

var _ bilibili.ClientInterface = (*fakeBili)(nil)

func (f *fakeBili) count(name string) { f.mu.Lock(); defer f.mu.Unlock(); f.calls[name]++ }
func (f *fakeBili) counts() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := map[string]int{}
	for k, v := range f.calls {
		r[k] = v
	}
	return r
}
func (f *fakeBili) Status(context.Context) (model.Account, error) {
	f.count("status")
	if f.block != nil {
		close(f.entered)
		<-f.block
	}
	a := model.Account{UID: f.uid, Name: "测试账号"}
	return a, nil
}
func (f *fakeBili) QR(context.Context) (string, string, error) {
	f.count("qr")
	return "https://example.com/qr", "key", nil
}
func (f *fakeBili) Poll(context.Context, string) (model.PollResult, error) {
	f.count("poll")
	p := model.PollResult{Code: 0, Credentials: &model.Credentials{UID: f.uid, Cookies: map[string]string{"SESSDATA": "fake", "bili_jct": "fake", "DedeUserID": f.uid}}}
	return p, nil
}
func (f *fakeBili) Video(context.Context, string) (model.Video, error) {
	f.count("video")
	v := model.Video{Aid: "100", BVID: bvid, Title: "测试视频", Owner: "123"}
	return v, nil
}
func (f *fakeBili) Comments(context.Context, string, int, string) (model.CommentPage, error) {
	f.count("comments")
	c := model.Comment{ID: "1001", Root: "0", Parent: "0", UID: "456", Name: "观众", Text: f.targetText}
	p := model.CommentPage{Comments: []model.Comment{c}, Page: 1, Total: 1}
	return p, nil
}
func (f *fakeBili) Target(context.Context, string, string, string) (model.Comment, error) {
	f.count("target")
	c := model.Comment{ID: "1001", Root: "0", Parent: "0", UID: "456", Name: "观众", Text: f.targetText}
	return c, nil
}
func (f *fakeBili) Send(context.Context, string, string, string, string, string) (model.CommentReceipt, error) {
	f.count("send")
	if f.unknown {
		return model.CommentReceipt{}, model.Fail("SEND_UNKNOWN", "模拟断开")
	}
	return model.CommentReceipt{ID: "999", State: f.state, Message: "发送成功"}, nil
}
func (f *fakeBili) SendDynamic(context.Context, string) (string, error) {
	f.count("sendDynamic")
	return "888", nil
}
func (f *fakeBili) Dynamic(context.Context, string) (model.Dynamic, error) {
	f.count("dynamic")
	d := model.Dynamic{ID: "888", UID: f.uid, Text: "测试动态"}
	return d, nil
}
func (f *fakeBili) Visible(context.Context, string, string, string) (model.Visibility, error) {
	f.count("visible")
	return model.Visibility{Found: true, State: f.state}, nil
}
func setup(t *testing.T) (*core.Service, *fakeBili) {
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Helper()
	f := &fakeBili{uid: "123", targetText: "原评论", calls: map[string]int{}}
	s := core.NewService(t.TempDir(), f)
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	return s, f
}
func allow(t *testing.T, s *core.Service) {
	t.Helper()
	p := model.DefaultPermissions()
	p.Enabled = true
	p.AllowedVideos = []string{bvid}
	if _, err := s.SavePermissions(ctx, &p); err != nil {
		t.Fatal(err)
	}
}
func reply(text string) *model.DraftInput {
	input := &model.DraftInput{BVID: bvid, Root: "1001", Parent: "1001", Text: text}
	return input
}
func prepare(t *testing.T, s *core.Service, text string) model.Draft {
	t.Helper()
	d, err := s.Prepare(ctx, reply(text))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func wantError(t *testing.T, err error, code string) {
	t.Helper()
	var e *model.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}
func TestDefaultDeniedAndDuplicateQueries(t *testing.T) {
	s, f := setup(t)
	_, err := s.Prepare(ctx, reply("你好"))
	wantError(t, err, "DENIED")
	if len(f.counts()) != 0 {
		t.Fatal(f.counts())
	}
	allow(t, s)
	a := prepare(t, s, "你好")
	b := prepare(t, s, "你好")
	if a.ID != b.ID {
		t.Fatal("duplicate draft")
	}
	calls := f.counts()
	if calls["status"] != 2 || calls["video"] != 1 || calls["target"] != 1 {
		t.Fatal(calls)
	}
	p, _ := s.Permissions(ctx)
	p.BlockedUsers = []string{"456"}
	s.SavePermissions(ctx, &p)
	_, err = s.Prepare(ctx, reply("你好"))
	wantError(t, err, "DENIED")
	if f.counts()["target"] != 1 {
		t.Fatal("repeated target lookup")
	}
}
func TestPermissionsAndTargetFreshAtSend(t *testing.T) {
	s, f := setup(t)
	allow(t, s)
	d := prepare(t, s, "你好")
	p, _ := s.Permissions(ctx)
	p.Enabled = false
	s.SavePermissions(ctx, &p)
	before := f.counts()
	_, err := s.Send(ctx, d.ID)
	wantError(t, err, "DENIED")
	if !reflect.DeepEqual(before, f.counts()) {
		t.Fatal("closed send queried platform")
	}
	allow(t, s)
	f.uid = "777"
	_, err = s.Send(ctx, d.ID)
	wantError(t, err, "ACCOUNT")
	f.uid = "123"
	f.targetText = "修改过的评论"
	_, err = s.Send(ctx, d.ID)
	wantError(t, err, "TARGET")
	if f.counts()["send"] != 0 {
		t.Fatal("unexpected send")
	}
}
func TestSendOnceAndRestartDataCompatibility(t *testing.T) {
	s, f := setup(t)
	allow(t, s)
	d := prepare(t, s, "你好")
	result, err := s.Send(ctx, d.ID)
	if err != nil || result.Status != "visible" || result.RPID != "999" {
		t.Fatal(result, err)
	}
	_, err = s.Send(ctx, d.ID)
	wantError(t, err, "STATE")
	if f.counts()["send"] != 1 {
		t.Fatal(f.counts())
	}
	restored := core.NewService(s.Store.Root, f)
	if err = restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	ds, _ := restored.Drafts(ctx)
	if ds[0].Status != "visible" {
		t.Fatal(ds)
	}
	a, err := restored.Context(ctx)
	if err != nil || len(a.RecentEvents) == 0 {
		t.Fatal(err)
	}
}
func TestConcurrentSendAndNotes(t *testing.T) {
	s, f := setup(t)
	allow(t, s)
	d := prepare(t, s, "你好")
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { s.Send(ctx, d.ID) })
	}
	wg.Wait()
	if f.counts()["send"] != 1 {
		t.Fatal(f.counts())
	}
	e, err := s.Store.Event(ctx, "test", "并发来源", nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 8 {
		wg.Go(func() {
			input := model.NoteInput{Kind: "memory", Title: "记忆", Text: "真实经历", Sources: []string{e.ID}}
			if _, err := s.Note(ctx, &input); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	notes, err := s.Store.Notes(ctx)
	if err != nil || len(notes) != 8 {
		t.Fatal(notes, err)
	}
}
func TestUnknownNeverRetriesAndSendingRecovery(t *testing.T) {
	s, f := setup(t)
	allow(t, s)
	d := prepare(t, s, "你好")
	f.unknown = true
	result, err := s.Send(ctx, d.ID)
	if err != nil || result.Status != "unknown" {
		t.Fatal(result, err)
	}
	_, err = s.Send(ctx, d.ID)
	wantError(t, err, "STATE")
	ds, _ := s.Drafts(ctx)
	ds[0].Status = "sending"
	s.Store.Write(ctx, "data/events/drafts.json", ds)
	restored := core.NewService(s.Store.Root, f)
	if err = restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	ds, _ = restored.Drafts(ctx)
	if ds[0].Status != "unknown" || f.counts()["send"] != 1 {
		t.Fatal(ds)
	}
}
func TestCooldownAndQuotaRejectWithoutQueries(t *testing.T) {
	s, f := setup(t)
	allow(t, s)
	d := prepare(t, s, "一")
	other := prepare(t, s, "二")
	if _, err := s.Send(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	before := f.counts()
	_, err := s.Send(ctx, other.ID)
	wantError(t, err, "COOLDOWN")
	if !reflect.DeepEqual(before, f.counts()) {
		t.Fatal("cooldown queried platform")
	}
	ds, _ := s.Drafts(ctx)
	ds[0].AttemptAt = time.Now().UTC().Add(-180 * time.Second).Format(time.RFC3339Nano)
	s.Store.Write(ctx, "data/events/drafts.json", ds)
	p, _ := s.Permissions(ctx)
	p.DailyLimit = 1
	s.SavePermissions(ctx, &p)
	_, err = s.Send(ctx, other.ID)
	wantError(t, err, "LIMIT")
	if !reflect.DeepEqual(before, f.counts()) {
		t.Fatal("quota queried platform")
	}
}
func TestDynamicIndependentSwitchAndRecheck(t *testing.T) {
	s, f := setup(t)
	allow(t, s)
	_, err := s.PrepareDynamic(ctx, "测试动态")
	wantError(t, err, "DENIED")
	if len(f.counts()) != 0 {
		t.Fatal(f.counts())
	}
	p, _ := s.Permissions(ctx)
	p.AllowDynamic = true
	s.SavePermissions(ctx, &p)
	d, err := s.PrepareDynamic(ctx, "测试动态")
	if err != nil {
		t.Fatal(err)
	}
	same, err := s.PrepareDynamic(ctx, "测试动态")
	if err != nil || same.ID != d.ID {
		t.Fatal(same, err)
	}
	result, err := s.Send(ctx, d.ID)
	if err != nil || result.Status != "visible" {
		t.Fatal(result, err)
	}
	result, err = s.Recheck(ctx, d.ID)
	if err != nil || result.Status != "visible" {
		t.Fatal(result, err)
	}
	calls := f.counts()
	if calls["video"] != 0 || calls["dynamic"] != 2 || calls["sendDynamic"] != 1 {
		t.Fatal(calls)
	}
}
func TestMemorySourcesChineseAndRestart(t *testing.T) {
	s, _ := setup(t)
	input := model.CommentsInput{BVID: bvid}
	comments, err := s.Comments(ctx, &input)
	if err != nil {
		t.Fatal(err)
	}
	note := model.NoteInput{Kind: "memory", Title: "第一天", Text: "讨论原评论", Sources: []string{model.ID()}}
	_, err = s.Note(ctx, &note)
	wantError(t, err, "SOURCE")
	note.Sources = []string{comments.EventID}
	if _, err = s.Note(ctx, &note); err != nil {
		t.Fatal(err)
	}
	restored := core.NewService(s.Store.Root, nil)
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := restored.Search(ctx, "原评论")
	if err != nil || len(result.Events) != 1 || len(result.Notes) != 1 {
		t.Fatal(result, err)
	}
	if restored.Store.Database == nil {
		t.Fatal("restart did not reopen SQLite")
	}
}
func TestLatestEventsAvoidOldFilesAndPreserveOrder(t *testing.T) {
	s, _ := setup(t)
	for i := 0; i < 160; i++ {
		event := model.Event{ID: model.ID(), At: time.Date(2099, 1, 2, 0, 0, i, 0, time.UTC).Format(time.RFC3339Nano), Kind: "test", Summary: strings.Repeat("a", i+1), Data: map[string]any{}}
		if err := s.Store.Database.AppendEvent(ctx, &event); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Store.WriteText(ctx, "data/events/2099-01-01.jsonl", "损坏\n"); err != nil {
		t.Fatal(err)
	}
	c, err := s.Context(ctx)
	if err != nil || len(c.RecentEvents) != 8 || len(c.RecentEvents[0].Summary) != 153 {
		t.Fatal(c, err)
	}
	o, err := s.Overview(ctx)
	if err != nil || len(o.Events) != 150 || len(o.Events[0].Summary) != 160 {
		t.Fatal(o, err)
	}
	all, err := s.Store.Events(ctx, 0)
	if err != nil || len(all) != 160 {
		t.Fatal("legacy file was reread", len(all), err)
	}
}
func TestSlowPlatformDoesNotBlockLocalReads(t *testing.T) {
	s, f := setup(t)
	f.block = make(chan struct{})
	f.entered = make(chan struct{})
	done := make(chan struct{})
	go func() { defer close(done); s.Account(ctx) }()
	<-f.entered
	defer func() { close(f.block); <-done }()
	localDone := make(chan error, 1)
	go func() { _, err := s.Context(ctx); localDone <- err }()
	select {
	case err := <-localDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("local context blocked by network")
	}
}
func TestStrictPermissionFileAndPrivateFileModes(t *testing.T) {
	s, _ := setup(t)
	s.Store.WriteText(ctx, "config/permissions.json", `{"enabled":true}`)
	_, err := s.Permissions(ctx)
	wantError(t, err, "DATA")
	info, err := os.Stat(filepath.Join(s.Store.Root, "config/permissions.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	if err = s.Store.WriteText(ctx, "data/events/drafts.json", ""); err == nil {
		t.Fatal("invalid draft JSON accepted")
	}
	if _, err = s.Drafts(ctx); err != nil {
		t.Fatal("failed write damaged drafts", err)
	}
}

func TestRestrictedAndReviewStateRemainIdempotent(t *testing.T) {
	for _, value := range []int{17, 11, 0} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			s, f := setup(t)
			allow(t, s)
			f.state = &value
			d := prepare(t, s, "状态验收")
			result, err := s.Send(ctx, d.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := "visible"
			if value == 17 {
				want = "restricted"
			}
			if value == 11 {
				want = "pending_review"
			}
			if result.Status != want || result.PlatformState == nil || *result.PlatformState != value {
				t.Fatal(result)
			}
			duplicate := prepare(t, s, "状态验收")
			if duplicate.ID != d.ID {
				t.Fatal("restricted duplicate bypass")
			}
			_, err = s.Send(ctx, d.ID)
			wantError(t, err, "STATE")
			if f.counts()["send"] != 1 {
				t.Fatal(f.counts())
			}
			zero := 0
			f.state = &zero
			result, err = s.Recheck(ctx, d.ID)
			if err != nil || result.Status != "visible" || result.VisibilityCheckedAt == "" {
				t.Fatal(result, err)
			}
			hidden := 17
			f.state = &hidden
			result, err = s.Recheck(ctx, d.ID)
			if err != nil || result.Status != "restricted" {
				t.Fatal(result, err)
			}
		})
	}
}

func TestRestrictedReceiptSkipsRedundantVisibilityQueryAndPersistsMessage(t *testing.T) {
	s, f := setup(t)
	allow(t, s)
	state := 17
	f.state = &state
	d := prepare(t, s, "回执检查")
	result, err := s.Send(ctx, d.ID)
	if err != nil || result.Status != "restricted" || result.PlatformMessage != "发送成功" {
		t.Fatal(result, err)
	}
	if f.counts()["visible"] != 0 || f.counts()["send"] != 1 {
		t.Fatal(f.counts())
	}
	ds, err := s.Drafts(ctx)
	if err != nil || ds[0].PlatformState == nil || *ds[0].PlatformState != 17 || ds[0].PlatformMessage != "发送成功" {
		t.Fatal(ds, err)
	}
}
func TestInvalidImportedUserAgentRejectedBeforeNetwork(t *testing.T) {
	s, f := setup(t)
	input := model.ImportSessionInput{Cookie: "SESSDATA=test;bili_jct=test", ExpectedUID: "123", UserAgent: "bad\r\nHeader: value"}
	_, err := s.ImportSession(ctx, &input)
	wantError(t, err, "INPUT")
	if len(f.counts()) != 0 {
		t.Fatal(f.counts())
	}
}

func (f *fakeBili) SearchVideos(context.Context, string) ([]model.Video, error) {
	return nil, model.Fail("UNAVAILABLE", "此替身未配置搜索")
}

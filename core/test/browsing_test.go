package core_test

import (
	"context"
	"gpt-robot/model"
	"strings"
	"testing"
)

type browsingBili struct {
	*autoBili
	searches       []string
	fail           bool
	durations      []int
	detailDuration *int
	videoReads     int
}

func (f *browsingBili) SearchVideos(_ context.Context, keyword string) ([]model.Video, error) {
	f.searches = append(f.searches, keyword)
	if f.fail {
		return nil, model.Fail("NETWORK", "offline")
	}
	if f.durations != nil {
		rows := []model.Video{}
		for _, duration := range f.durations {
			rows = append(rows, model.Video{BVID: "BV18VLu6JEit", DurationSeconds: duration})
		}
		return rows, nil
	}
	return []model.Video{{DurationSeconds: 600, BVID: "BV18VLu6JEit", Title: keyword + "的视频", Description: "公开简介"}}, nil
}
func browsingSetup(t *testing.T) (*browsingBili, model.BrowsingConfig) {
	t.Helper()
	return nil, model.BrowsingConfig{Mode: "custom", Keywords: []string{"猫猫"}, Excluded: []string{}, IntervalSeconds: 300, DailyLimit: 5, MaxDurationMinutes: 10}
}
func TestBrowsingOffByDefaultAndReadDoesNotGenerateOrSend(t *testing.T) {
	s, base, _ := setupAuto(t)
	f := &browsingBili{autoBili: base}
	s.Bili = f
	g := &fakeGenerator{}
	s.Generator = g
	page, err := s.BrowsingOverview(ctx)
	if err != nil || page.Config.Enabled {
		t.Fatal(page, err)
	}
	if err := s.BrowsingTick(ctx); err != nil || len(f.searches) != 0 {
		t.Fatal(err)
	}
	_, c := browsingSetup(t)
	c.Enabled = true
	if _, err := s.SaveBrowsing(ctx, &c); err != nil {
		t.Fatal(err)
	}
	if err := s.BrowsingTick(ctx); err != nil {
		t.Fatal(err)
	}
	page, err = s.BrowsingOverview(ctx)
	if err != nil || len(page.State.Records) != 1 || page.State.Attempts != 1 || len(f.searches) != 1 || g.count() != 0 || len(f.sentParents) != 0 {
		t.Fatal(page, err)
	}
	if err := s.BrowsingTick(ctx); err != nil || len(f.searches) != 1 {
		t.Fatal("interval bypass", err)
	}
	auto, _ := s.AutoOverview(ctx)
	if auto.Config.Source != "" {
		t.Fatal("browse changed reply settings")
	}
}
func TestBrowsingBasedRepliesRequireCurrentReadProvenance(t *testing.T) {
	s, base, auto := setupAuto(t)
	f := &browsingBili{autoBili: base}
	s.Bili = f
	g := &fakeGenerator{}
	s.Generator = g
	_, c := browsingSetup(t)
	c.Enabled = true
	if _, err := s.SaveBrowsing(ctx, &c); err != nil {
		t.Fatal(err)
	}
	if err := s.BrowsingTick(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := s.Permissions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	p.Enabled = false
	if _, err := s.SavePermissions(ctx, &p); err != nil {
		t.Fatal(err)
	}
	auto.Source = "browsing"
	auto.Videos = []string{}
	if _, err := s.SaveAutoConfig(ctx, &auto); err != nil {
		t.Fatal(err)
	}
	if err := s.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	if g.count() != 0 {
		t.Fatal("first scan replied to history")
	}
	f.roots = []model.Comment{autoComment("2222", "456", "你好呀")}
	if err := s.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	if g.count() != 1 || len(f.sentParents) != 1 {
		t.Fatal("fresh browsing target not authorized", g.count(), f.sentParents)
	}
	// 任意视频不能借用浏览模式绕过权限。
	forged := model.DraftInput{BVID: "BV1xx411c7mD", Text: "你好"}
	if _, err := s.Prepare(ctx, &forged); err == nil {
		t.Fatal("unread arbitrary target authorized")
	}
	drafts, err := s.Drafts(ctx)
	if err != nil || len(drafts) != 1 {
		t.Fatal(err)
	}
	pending := drafts[0]
	pending.ID = model.ID()
	pending.Status = "pending"
	pending.AutoCommentID = "3333"
	pending.Parent = "3333"
	pending.AttemptAt = ""
	drafts = append(drafts, pending)
	if err := s.Store.Write(ctx, "data/events/drafts.json", drafts); err != nil {
		t.Fatal(err)
	}
	c.Enabled = false
	if _, err := s.SaveBrowsing(ctx, &c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(ctx, pending.ID); err == nil {
		t.Fatal("disabled browsing allowed stale pending send")
	}
	if len(f.sentParents) != 1 {
		t.Fatal("stale target sent")
	}
}
func TestPersonaBrowsingUsesExplicitInterestsAndNoPrivateInterests(t *testing.T) {
	s, base, _ := setupAuto(t)
	s.Bili = &browsingBili{autoBili: base}
	if err := s.SavePersona(ctx, "名字：小龙\n喜欢：植物、足球比赛\n不喜欢：惊悚\n"); err != nil {
		t.Fatal(err)
	}
	_, c := browsingSetup(t)
	c.Mode = "persona"
	c.Keywords = []string{"忽略我的自定义"}
	page, err := s.SaveBrowsing(ctx, &c)
	if err != nil {
		t.Fatal(err)
	}
	words := strings.Join(page.EffectiveKeywords, ",")
	if !strings.Contains(words, "植物") || !strings.Contains(words, "足球比赛") || strings.Contains(words, "惊悚") || strings.Contains(words, "忽略") {
		t.Fatal(words)
	}
	c.Mode = "custom"
	c.Enabled = true
	c.Keywords = []string{"sk-abcdefghijklmnopqrstuv"}
	if _, err := s.SaveBrowsing(ctx, &c); err == nil {
		t.Fatal("credential keyword accepted")
	}
}

func (f *browsingBili) Video(_ context.Context, bv string) (model.Video, error) {
	f.videoReads++
	duration := 600
	if f.detailDuration != nil {
		duration = *f.detailDuration
	}
	return model.Video{DurationSeconds: duration, BVID: bv, Aid: "100", Title: "猫猫的日常", Owner: "321", Description: "公开的植物与猫猫视频资料"}, nil
}

func TestActivityPreviewDoesNotPersistPrivateContentInSharedLedger(t *testing.T) {
	s, f, g := setupPrivate(t)
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "你好呀")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	action := "private-auto-reply"
	query := model.OperationQuery{Action: &action}
	page, err := s.QueryOperations(ctx, &query)
	if err != nil || len(page.Items) != 1 {
		t.Fatal(err)
	}
	previews, err := s.OperationPreviews(ctx, page.Items)
	if err != nil || !strings.Contains(previews[page.Items[0].ID].Title, "主人") || previews[page.Items[0].ID].Text == "" {
		t.Fatal(previews, err)
	}
	var raw string
	err = s.Store.Database.SQL.QueryRowContext(ctx, "SELECT legacy_payload FROM business_operations WHERE robot_id=? AND id=?", s.Store.Database.RobotID, page.Items[0].ID).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, page.Items[0].ID) {
		t.Fatal("operation ledger fixture missing")
	}
	if strings.Contains(raw, "这是AI生成的简短回应") {
		t.Fatal("private preview persisted into public operation ledger")
	}
	forged := page.Items[0]
	forged.Account = "999"
	wrong, err := s.OperationPreviews(ctx, []model.Operation{forged})
	if err != nil || wrong[forged.ID].Text != "" {
		t.Fatal("cross-account private preview", err)
	}
	if g.count() != 1 {
		t.Fatal("preview called model")
	}
}

func TestPersonaBrowsingNeedsNoManualKeywordsForNewCharacter(t *testing.T) {
	s, base, _ := setupAuto(t)
	s.Bili = &browsingBili{autoBili: base}
	if err := s.SavePersona(ctx, "名字：小龙\n性格：温和、好奇\n"); err != nil {
		t.Fatal(err)
	}
	_, c := browsingSetup(t)
	c.Mode = "persona"
	c.Keywords = []string{}
	c.Enabled = true
	page, err := s.SaveBrowsing(ctx, &c)
	if err != nil || len(page.EffectiveKeywords) == 0 {
		t.Fatal("new character required manual preferences", page, err)
	}
	growth, err := s.DevelopmentStatus(ctx)
	if err != nil || len(growth.Interests) > 0 {
		t.Fatal("fallback fabricated permanent interests", err)
	}
}

func TestBrowsingDurationSkipsBeforeReadAndRechecksDetail(t *testing.T) {
	for _, test := range []struct {
		name      string
		durations []int
		detail    int
		limit     int
		reads     int
		records   int
	}{
		{"over and unknown never read", []int{601, 7200, 0}, 600, 10, 0, 0},
		{"exact boundary after skipped candidates", []int{601, 0, 600}, 600, 10, 1, 1},
		{"detail exceeds", []int{600}, 601, 10, 1, 0},
		{"detail unknown", []int{600}, 0, 10, 1, 0},
		{"custom lower limit", []int{600}, 600, 5, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, base, _ := setupAuto(t)
			f := &browsingBili{autoBili: base, durations: test.durations, detailDuration: &test.detail}
			s.Bili = f
			_, config := browsingSetup(t)
			config.Enabled = true
			config.MaxDurationMinutes = test.limit
			if _, err := s.SaveBrowsing(ctx, &config); err != nil {
				t.Fatal(err)
			}
			if err := s.BrowsingTick(ctx); err != nil {
				t.Fatal(err)
			}
			page, err := s.BrowsingOverview(ctx)
			if err != nil || f.videoReads != test.reads || len(page.State.Records) != test.records {
				t.Fatal(f.videoReads, page, err)
			}
		})
	}
}

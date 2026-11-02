package core_test

import (
	"context"
	"encoding/json"
	"gpt-robot/core"
	"gpt-robot/model"
	"strings"
	"testing"
	"time"
)

func TestDevelopmentSlowDailyCappedDedupAndRestart(t *testing.T) {
	s, f, _ := setupDiary(t)
	base := time.Now().UTC().Add(time.Second)
	for day := 0; day < 10; day++ {
		now := base.AddDate(0, 0, day)
		s.DevelopmentNow = func() time.Time { return now }
		for i := 0; i < 8; i++ {
			signal := model.InterestSignal{Topic: "狗", Delta: 1}
			input := model.DevelopmentInput{Platform: "bilibili", Account: "123", Peer: "789", Visibility: "public", Source: model.Number(day*10 + i), At: now.Format(time.RFC3339Nano), Text: "一起聊狗的照料", Interaction: true, Interests: []model.InterestSignal{signal}}
			if err := s.RecordDevelopment(ctx, &input); err != nil {
				t.Fatal(err)
			}
			if err := s.RecordDevelopment(ctx, &input); err != nil {
				t.Fatal(err)
			}
		}
	}
	settled := base.AddDate(0, 0, 9).Add(6 * time.Minute)
	s.DevelopmentNow = func() time.Time { return settled }
	page, err := s.DevelopmentStatus(ctx)
	if err != nil || len(page.Interests) != 1 || page.Interests[0].FamiliarityUnits < 500 || page.Interests[0].FamiliarityUnits > 650 || page.Interests[0].Stage != "like_1" || len(page.Relationships) != 1 || page.Relationships[0].Score != 10 || page.Relationships[0].Days != 10 {
		t.Fatal(page, err)
	}
	// 同一天连续八条只形成一段经历，十天仍未达到朋友门槛。
	if page.Relationships[0].Label == "朋友（角色模拟）" {
		t.Fatal(page.Relationships)
	}
	restored := core.NewService(s.Store.Root, f)
	restored.DevelopmentNow = s.DevelopmentNow
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	previous, _ := json.Marshal(page)
	loaded, err := restored.DevelopmentStatus(ctx)
	after, _ := json.Marshal(loaded)
	if err != nil || string(previous) != string(after) {
		t.Fatal("development did not persist", err)
	}
	now := base.AddDate(0, 0, 9)
	duplicate := model.DevelopmentInput{Platform: "bilibili", Account: "123", Peer: "789", Visibility: "public", Source: "90", At: now.Format(time.RFC3339Nano), Text: "一起聊狗的照料", Interaction: true}
	if err := restored.RecordDevelopment(ctx, &duplicate); err != nil {
		t.Fatal(err)
	}
	loaded, _ = restored.DevelopmentStatus(ctx)
	if loaded.Relationships[0].Score != 10 {
		t.Fatal("restart duplicated source")
	}
	data, err := s.Store.Text(ctx, "data/private/development.json")
	if err != nil || strings.Contains(string(data), "一起聊狗的照料") {
		t.Fatal("raw chat stored in growth file", err)
	}
}

func TestPrivateDevelopmentCannotEnterPublicReplyOrAnotherConversation(t *testing.T) {
	s, f, _ := setupAuto(t)
	now := time.Now().UTC().Add(time.Second)
	s.DevelopmentNow = func() time.Time { return now }
	private := model.DevelopmentInput{Platform: "bilibili", Account: "123", Peer: "789", Visibility: "private", Source: "private-1", At: now.Format(time.RFC3339Nano), Text: "我喜欢狗，还有私密的花园", Interaction: true}
	if err := s.RecordDevelopment(ctx, &private); err != nil {
		t.Fatal(err)
	}
	if err := s.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	f.roots = []model.Comment{autoComment("8801", "789", "狗今天好可爱")}
	if err := s.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	g := s.Generator.(*fakeGenerator)
	if len(g.prompts) != 1 || strings.Contains(g.prompts[0].Development, "熟悉度") || strings.Contains(g.prompts[0].Development, "花园") {
		t.Fatal("private growth entered public model", g.prompts)
	}
	public, err := s.Context(ctx)
	raw, _ := json.Marshal(public)
	if err != nil || strings.Contains(string(raw), "私密的花园") || strings.Contains(string(raw), "relationships") {
		t.Fatal("private growth entered public context", err)
	}
	// 不同平台/账号/隐私范围不会因昵称或同一个数字ID合并。
	other := private
	other.Platform = "qq"
	other.Source = "qq-1"
	if err := s.RecordDevelopment(ctx, &other); err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Minute)
	page, _ := s.DevelopmentStatus(ctx)
	if len(page.Relationships) != 2 {
		t.Fatal("identities merged", page.Relationships)
	}
}

func TestActualPrivateReplyRecordsGrowthAndNextPromptUsesOnlyBoundScope(t *testing.T) {
	s, f, g := setupPrivate(t)
	enablePrivateMemory(t, s, true)
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	signal := model.InterestSignal{Topic: "植物", Delta: 1}
	g.signal = &model.CharacterSignal{Mood: "curious", Intensity: 30, Interests: []model.InterestSignal{signal}}
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "植物今天长得很好")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	settled := time.Now().Add(6 * time.Minute)
	s.DevelopmentNow = func() time.Time { return settled }
	page, err := s.DevelopmentStatus(ctx)
	if err != nil || len(page.Interests) != 1 || page.Interests[0].Visibility != "abstract" || len(page.Relationships) != 1 {
		t.Fatal(page, err)
	}
	// 第二条不实际发私信，替身调用保持真实业务路径并越过临时发送冷却。
	state := model.PrivateState{}
	if _, err := s.Store.Read(ctx, "data/private/state.json", &state); err != nil {
		t.Fatal(err)
	}
	for i := range state.Outbox {
		state.Outbox[i].At = time.Now().Add(-11 * time.Minute).UTC().Format(time.RFC3339Nano)
	}
	if err := s.Store.Write(ctx, "data/private/state.json", &state); err != nil {
		t.Fatal(err)
	}
	restored := core.NewService(s.Store.Root, f)
	restored.Generator = g
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	f.messages = append(f.messages, privateMessage(2, "789", "123", "植物又长新叶了"))
	if err := restored.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if g.count() != 2 || !strings.Contains(g.prompts[1].Development, "植物") {
		t.Fatal("private scoped growth missing from next reply", g.prompts)
	}
}

type growthVideoBili struct{ *fakeBili }

func (f *growthVideoBili) Video(_ context.Context, id string) (model.Video, error) {
	result := model.Video{Aid: "100", BVID: id, Title: "植物的日常照料", Owner: "123"}
	return result, nil
}
func TestRealCommentsReadGrowsFamiliarityWithoutModelOrRepeat(t *testing.T) {
	s, f, _ := setupDiary(t)
	s.Bili = &growthVideoBili{fakeBili: f}
	credentials := model.Credentials{UID: "123", Cookies: map[string]string{}}
	if err := s.Store.Write(ctx, ".private/bilibili.json", &credentials); err != nil {
		t.Fatal(err)
	}
	input := model.CommentsInput{BVID: bvid}
	if _, err := s.Comments(ctx, &input); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Comments(ctx, &input); err != nil {
		t.Fatal(err)
	}
	page, err := s.DevelopmentStatus(ctx)
	if err != nil || len(page.Interests) != 1 || page.Interests[0].FamiliarityUnits != 25 || page.Interests[0].PreferenceUnits != 0 || len(page.Relationships) != 0 {
		t.Fatal(page, err)
	}
}

func TestDevelopmentNegativeSignalAndOldSourceCannotRewriteToday(t *testing.T) {
	s, _, _ := setupDiary(t)
	now := time.Now().UTC().Add(time.Second)
	signal := model.InterestSignal{Topic: "猫", Delta: -1}
	for day := 0; day < 3; day++ {
		at := now.AddDate(0, 0, day)
		s.DevelopmentNow = func() time.Time { return at }
		for i := 0; i < 2; i++ {
			input := model.DevelopmentInput{Platform: "qq", Account: "a", Peer: "b", Visibility: "private", Source: model.Number(day*2 + i), At: at.Format(time.RFC3339Nano), Text: "认真讨论猫的习性", Interaction: true, Interests: []model.InterestSignal{signal}}
			if err := s.RecordDevelopment(ctx, &input); err != nil {
				t.Fatal(err)
			}
		}
	}
	settled := now.AddDate(0, 0, 2).Add(6 * time.Minute)
	s.DevelopmentNow = func() time.Time { return settled }
	page, _ := s.DevelopmentStatus(ctx)
	if len(page.Interests) != 1 || page.Interests[0].PreferenceUnits != -150 {
		t.Fatal("negative evidence did not change preference slowly", page)
	}
	old := model.DevelopmentInput{Platform: "qq", Account: "a", Peer: "b", Visibility: "private", Source: "late-old", At: now.Format(time.RFC3339Nano), Text: "认真讨论猫的习性", Interaction: true}
	if err := s.RecordDevelopment(ctx, &old); err != nil {
		t.Fatal(err)
	}
	at := now.AddDate(0, 0, 3)
	s.DevelopmentNow = func() time.Time { return at }
	negative := old
	negative.Source = "negative"
	negative.At = at.Format(time.RFC3339Nano)
	negative.RelationDelta = -1
	if err := s.RecordDevelopment(ctx, &negative); err != nil {
		t.Fatal(err)
	}
	at = at.Add(6 * time.Minute)
	changed, _ := s.DevelopmentStatus(ctx)
	if changed.Relationships[0].Score != page.Relationships[0].Score+1 || changed.Relationships[0].AffinityUnits != page.Relationships[0].AffinityUnits-50 {
		t.Fatal("relationship did not cool down or old record replayed", changed)
	}
}

func TestInterestQuestionRecallsPublicGrowthWithoutPrivateTopics(t *testing.T) {
	s, f, _ := setupAuto(t)
	now := time.Now().UTC().Add(time.Second)
	s.DevelopmentNow = func() time.Time { return now }
	public := model.DevelopmentInput{Platform: "bilibili", Account: "123", Visibility: "public", Source: "video:new", At: now.Format(time.RFC3339Nano), Text: "狗在玩耍"}
	private := model.DevelopmentInput{Platform: "bilibili", Account: "123", Peer: "789", Visibility: "private", Source: "private:new", At: now.Format(time.RFC3339Nano), Text: "我喜欢植物"}
	if err := s.RecordDevelopment(ctx, &public); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordDevelopment(ctx, &private); err != nil {
		t.Fatal(err)
	}
	if err := s.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	f.roots = []model.Comment{autoComment("8891", "789", "你有什么爱好呀")}
	if err := s.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	g := s.Generator.(*fakeGenerator)
	if len(g.prompts) != 1 || !strings.Contains(g.prompts[0].Development, "狗") || !strings.Contains(g.prompts[0].Development, "植物") || strings.Contains(g.prompts[0].Development, "private:new") {
		t.Fatal("interest recall missing or crossed privacy scope", g.prompts)
	}
}

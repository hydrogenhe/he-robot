package core_test

import (
	"encoding/json"
	"gpt-robot/core"
	"gpt-robot/model"
	"strings"
	"testing"
	"time"
)

func TestPrivateRecentWindowBoundedAcrossGrowingHistoryAndRestart(t *testing.T) {
	for _, size := range []int{10, 2000} {
		t.Run(model.Number(size), func(t *testing.T) {
			s, f, g := setupPrivate(t)
			if err := s.PrivateTick(ctx); err != nil {
				t.Fatal(err)
			}
			var state model.PrivateState
			if _, err := s.Store.Read(ctx, "data/private/state.json", &state); err != nil {
				t.Fatal(err)
			}
			for i := 1; i <= size; i++ {
				row := privateMessage(int64(i), "789", "123", strings.Repeat("近期内容", 60))
				state.Inbox = append(state.Inbox, row)
			}
			old := privateMessage(int64(size+1), "789", "123", "过期的私聊内容")
			old.Timestamp = time.Now().Add(-2 * time.Hour).Unix()
			foreign := privateMessage(int64(size+2), "456", "123", "陌生会话内容")
			withdrawn := privateMessage(int64(size+3), "789", "123", "已撤回内容")
			withdrawn.Withdrawn = true
			state.Inbox = append(state.Inbox, old, foreign, withdrawn)
			if err := s.Store.Write(ctx, "data/private/state.json", &state); err != nil {
				t.Fatal(err)
			}
			restored := core.NewService(s.Store.Root, f)
			restored.Generator = g
			if err := restored.Initialize(ctx); err != nil {
				t.Fatal(err)
			}
			f.messages = []model.PrivateMessage{privateMessage(int64(size+4), "789", "123", "今天过得怎么样呀")}
			if err := restored.PrivateTick(ctx); err != nil {
				t.Fatal(err)
			}
			if g.count() != 1 {
				t.Fatal("expected one model turn", g.count())
			}
			prompt := g.prompts[0]
			chars := 0
			for _, row := range prompt.Recent {
				chars += len([]rune(row))
			}
			raw, _ := json.Marshal(prompt)
			if len(prompt.Recent) > model.ReplyRecentLimit || chars > model.ReplyRecentRunes || len(raw) > model.ReplyContextMaxBytes || len(prompt.Recent) == 0 {
				t.Fatal("unbounded or missing recent context", len(raw), chars)
			}
			for _, body := range []string{"过期的私聊内容", "陌生会话内容", "已撤回内容"} {
				if strings.Contains(string(raw), body) {
					t.Fatal("out-of-scope recent context", body)
				}
			}
			t.Logf("history=%d promptBytes=%d recentChars=%d", size, len(raw), chars)
		})
	}
}

func TestPublicReplyRetrievesOnlyRelevantPublicNotesAndBoundsTools(t *testing.T) {
	s, f, _ := setupAuto(t)
	if err := s.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	source, err := s.Store.Event(ctx, "read", "读取过公开的龟背竹材料", nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		input := model.NoteInput{Kind: "memory", Title: "龟背竹", Text: strings.Repeat("公开植物资料", 600), Sources: []string{source.ID}}
		if _, err := s.Note(ctx, &input); err != nil {
			t.Fatal(err)
		}
	}
	irrelevant := model.NoteInput{Kind: "memory", Title: "无关的游戏", Text: "游戏笔记不应注入植物回复", Sources: []string{source.ID}}
	if _, err := s.Note(ctx, &irrelevant); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.WriteText(ctx, "data/private/memory/bilibili-123-789.json", "损坏的私聊库，不允许公开读取"); err == nil {
		t.Fatal("invalid private JSON accepted")
	}
	privateDiary := model.DiaryEntry{Date: time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02"), Status: "completed", Title: "私密日记", Body: "只在私聊和日记里的紫花暗号"}
	if err := s.Store.Write(ctx, "data/private/diary/"+privateDiary.Date+".json", &privateDiary); err != nil {
		t.Fatal(err)
	}
	f.roots = []model.Comment{autoComment("3001", "789", "你还记得龟背竹吗")}
	if err := s.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	g := s.Generator.(*fakeGenerator)
	if g.count() != 1 || len(g.prompts[0].PublicMemories) != 3 || len(g.prompts[0].PrivateMemories) != 0 || len(g.prompts[0].Recent) != 0 {
		t.Fatal(g.prompts)
	}
	rawPublic, _ := json.Marshal(g.prompts[0])
	if strings.Contains(string(rawPublic), "紫花暗号") {
		t.Fatal("private diary leaked into public AI prompt")
	}
	for _, memory := range g.prompts[0].PublicMemories {
		if !strings.Contains(memory.Summary, "龟背竹") || strings.Contains(memory.Summary, "游戏") || len([]rune(memory.Summary)) > 160 || memory.SourceID == "" {
			t.Fatal(memory)
		}
	}

	// 无关问候不回填最新记忆，也不调用额外检索模型。
	drafts, err := s.Drafts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range drafts {
		drafts[i].AttemptAt = time.Now().Add(-3 * time.Minute).UTC().Format(time.RFC3339Nano)
	}
	if err := s.Store.Write(ctx, "data/events/drafts.json", drafts); err != nil {
		t.Fatal(err)
	}
	f.roots = append(f.roots, autoComment("3002", "789", "晚上好呀"))
	if err := s.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	if g.count() != 2 || len(g.prompts[1].PublicMemories) != 0 {
		t.Fatal("unrelated memory or extra model calls", g.prompts)
	}
	search, err := s.Search(ctx, "龟背竹")
	if err != nil || len(search.Notes) != 3 {
		t.Fatal(search, err)
	}
	for _, note := range search.Notes {
		if len([]rune(note.Text)) > 160 {
			t.Fatal("long tool response")
		}
	}
	for _, event := range search.Events {
		if event.Data != nil {
			t.Fatal("raw event data returned")
		}
	}
	context, err := s.Context(ctx)
	if err != nil || len([]rune(context.Persona)) > 800 {
		t.Fatal(context, err)
	}
	for _, note := range context.Memories {
		if len([]rune(note.Text)) > 160 {
			t.Fatal("long context note")
		}
	}
}

func TestPrivateDateRecallRejectsUnrelatedTopic(t *testing.T) {
	s, f, _ := setupPrivate(t)
	enablePrivateMemory(t, s, false)
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "我养了一盆龟背竹")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if got := memoryPage(t, s, "今天香蕉是什么"); got.Total != 0 {
		t.Fatal("unrelated dated memory injected", got)
	}
	if got := memoryPage(t, s, "今天龟背竹"); got.Total != 1 {
		t.Fatal("matching dated fact missing", got)
	}
	if got := memoryPage(t, s, "今天我说了什么"); got.Total != 1 {
		t.Fatal("date-only recall missing", got)
	}
}

func TestPrivateMemoryExcerptKeepsMatchedFactAtEndOfLongStatement(t *testing.T) {
	s, f, g := setupPrivate(t)
	empty := ""
	g.text = &empty
	enablePrivateMemory(t, s, true)
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	statement := "我养了一盆植物，" + strings.Repeat("平时认真照顾它，", 25) + "名字是龟背竹"
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", statement)}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if g.count() != 1 {
		t.Fatal("owner long statement should be accepted without losing local memory")
	}
	f.messages = append(f.messages, privateMessage(2, "789", "123", "我养的龟背竹还记得吗"))
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if g.count() != 2 || len(g.prompts[1].PrivateMemories) != 1 {
		t.Fatal(g.prompts)
	}
	quote := g.prompts[1].PrivateMemories[0].Quote
	if len([]rune(quote)) > 160 || !strings.Contains(quote, "名字是龟背竹") {
		t.Fatal("matched fact truncated away", quote)
	}
}

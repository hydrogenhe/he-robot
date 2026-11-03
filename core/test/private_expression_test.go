package core_test

import (
	"bytes"
	"context"
	"encoding/json"
	"gpt-robot/core"
	"gpt-robot/model"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type expressionGenerator struct {
	calls   int
	parts   []string
	learned bool
	save    bool
	actions []model.ContentAction
	t       *testing.T
}

func (g *expressionGenerator) Generate(_ context.Context, prompt *model.ReplyPrompt) (*model.GeneratedReply, error) {
	g.calls++
	result := &model.GeneratedReply{Text: strings.Join(g.parts, " "), Messages: g.parts, Usage: &model.TokenUsage{Provider: "codex", Model: "fake", RequestID: model.ID(), Input: 100, Output: 20}}
	if g.save {
		id := "missing-content"
		if len(prompt.Contents) > 0 {
			id = prompt.Contents[0].ID
		}
		result.Actions = []model.ContentAction{{Action: "save_content", ContentID: id, Purpose: "sticker", Label: "开心猫"}}
	}
	if len(g.actions) > 0 {
		result.Actions = g.actions
	}

	if g.learned && !prompt.SelectSticker {
		result.StickerIntent = &model.StickerIntent{Suitable: true, Tags: []string{"开心"}, Scene: "闲聊"}
	}
	if g.learned && prompt.SelectSticker {
		for _, choice := range prompt.Stickers {
			if choice.Label == "开心猫" {
				result.StickerID = choice.ID
				break
			}
		}
		if result.StickerID == "" {
			g.t.Fatal("learned sticker not supplied from local library")
		}
	}
	return result, nil
}

type expressionBili struct {
	*privateBili
	data      []byte
	downloads int
	images    int
	attempts  int
	failAt    int
}

func (f *expressionBili) DownloadPrivateImage(_ context.Context, input *model.ImageAttachment) (*model.ImageAttachment, error) {
	f.downloads++
	result := *input
	result.Data = append([]byte{}, f.data...)
	result.Format = "png"
	result.Width = 8
	result.Height = 8
	return &result, nil
}
func (f *expressionBili) SendPrivate(ctx context.Context, sender, peer, text, request string) (string, error) {
	f.attempts++
	if f.failAt == f.attempts {
		return "", model.Fail("SEND_UNKNOWN", "模拟消息结果未知")
	}
	return f.privateBili.SendPrivate(ctx, sender, peer, text, request)
}
func (f *expressionBili) SendPrivateImage(_ context.Context, sender, peer string, input *model.ImageAttachment, request string) (string, error) {
	if sender != "123" || peer != "789" || !bytes.Equal(input.Data, f.data) {
		return "", model.Fail("ACCOUNT", "image scope or content mismatch")
	}
	f.images++
	return "999999999999999998", nil
}

func expressionService(t *testing.T) (*core.Service, *expressionBili, *expressionGenerator) {
	s, b, _ := setupPrivate(t)
	buffer := &bytes.Buffer{}
	if err := png.Encode(buffer, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	f := &expressionBili{privateBili: b, data: buffer.Bytes()}
	g := &expressionGenerator{parts: []string{"哈哈～", "有点意思。", "我们继续聊呀"}, t: t}
	s.Bili = f
	s.Generator = g
	s.ExpressionDraw = func(n int) int {
		if n == 3 {
			return 2
		}
		return 0
	}
	s.ExpressionWait = func(context.Context, time.Duration) error { return nil }
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	return s, f, g
}

func clearExpressionCooldown(t *testing.T, s *core.Service, f *expressionBili, g *expressionGenerator) *core.Service {
	state, _ := s.PrivateOverview(ctx)
	for i := range state.State.Outbox {
		state.State.Outbox[i].At = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	}
	state.State.NextAt = ""
	if err := s.Store.Write(ctx, "data/private/state.json", &state.State); err != nil {
		t.Fatal(err)
	}
	restored := core.NewService(s.Store.Root, f)
	restored.Generator = g
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	restored.ExpressionDraw = func(n int) int {
		if n == 3 {
			return 2
		}
		return 0
	}
	restored.ExpressionWait = func(context.Context, time.Duration) error { return nil }
	return restored
}

func TestPrivateReplyThreePartsOneGenerationAndPerPartLedger(t *testing.T) {
	s, f, g := expressionService(t)
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "今天聊得很开心呀")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	state, _ := s.PrivateOverview(ctx)
	if g.calls != 1 || f.attempts != 3 || len(state.State.Outbox) != 3 {
		t.Fatal("wrong inference/send counts", g.calls, f.attempts, len(state.State.Outbox))
	}
	group := state.State.Outbox[0].GroupID
	for i, out := range state.State.Outbox {
		if group == "" || out.GroupID != group || out.Part != i || out.Status != "sent" {
			t.Fatal("per-part receipt missing", out)
		}
	}
	action := "private-auto-reply"
	query := model.OperationQuery{Action: &action}
	ops, err := s.QueryOperations(ctx, &query)
	if err != nil || len(ops.Items) != 1 || len(ops.Items[0].Usage) != 1 || ops.Items[0].Usage[0].Input != 100 {
		t.Fatal("usage counted per part", ops, err)
	}
}

func TestPrivateReplyStopsAfterUnknownPartWithoutRetry(t *testing.T) {
	s, f, g := expressionService(t)
	f.failAt = 2
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "今天聊得很开心呀")}
	if err := s.PrivateTick(ctx); err == nil {
		t.Fatal("expected unknown send")
	}
	state, _ := s.PrivateOverview(ctx)
	if g.calls != 1 || f.attempts != 2 || len(state.State.Outbox) != 2 || state.State.Outbox[0].Status != "sent" || state.State.Outbox[1].Status != "unknown" || !state.State.Paused {
		t.Fatal("partial failure lost", state.State.Outbox)
	}
	_ = s.PrivateTick(ctx)
	if g.calls != 1 || f.attempts != 2 {
		t.Fatal("batch or generation retried")
	}
}

func TestNewIncomingMessageStopsUnsentTail(t *testing.T) {
	s, f, g := expressionService(t)
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "今天聊得很开心呀")}
	s.ExpressionWait = func(context.Context, time.Duration) error {
		f.messages = append(f.messages, privateMessage(2, "789", "123", "等一下，我想换个话题"))
		return nil
	}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	state, _ := s.PrivateOverview(ctx)
	if g.calls != 1 || f.attempts != 1 || state.State.Cursor != 1 || state.State.Paused {
		t.Fatal("new input did not interrupt safely", g.calls, f.attempts, state.State.Cursor)
	}
}

func TestOwnerLearnsFromSemanticActionAndReusesAfterModelSwitch(t *testing.T) {
	s, f, g := expressionService(t)
	enablePrivateMemory(t, s, true)
	g.parts = []string{"收到这张图了"}
	picture := privateMessage(1, "789", "123", "")
	picture.Image = &model.ImageAttachment{URL: "https://message.biliimg.com/bfs/im_new/source.png"}
	f.messages = []model.PrivateMessage{picture}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if f.downloads != 0 || g.calls != 1 {
		t.Fatal("merely showing an image saved it")
	}
	s = clearExpressionCooldown(t, s, f, g)
	g.save = true
	g.parts = []string{"好，这张我留着下次用～"}
	f.messages = append(f.messages, privateMessage(2, "789", "123", "这张收着呗，以后聊天拿出来用，名字你就叫开心猫"))
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(s.Store.Root, "data/private/stickers/*/*.image"))
	if len(files) != 1 || g.calls != 2 || f.downloads != 1 {
		t.Fatal("semantic action not saved once", files, g.calls, f.downloads)
	}
	raw, _ := os.ReadFile(files[0])
	if !bytes.Equal(raw, f.data) {
		t.Fatal("original changed")
	}
	s = clearExpressionCooldown(t, s, f, g)
	config := model.ModelProviderInput{Mode: "local", Provider: "deepseek", DeepSeekModel: "deepseek-flash", HarnessEffort: "low"}
	if _, err := s.SaveModelProviderConfig(ctx, &config); err != nil {
		t.Fatal(err)
	}
	g.save = false
	g.learned = true
	g.parts = []string{"这张我也会用啦"}
	f.messages = append(f.messages, privateMessage(3, "789", "123", "来个开心猫表情包"))
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if g.calls != 4 || f.images != 1 {
		t.Fatal("re-use added model calls or lost image", g.calls, f.images)
	}
	public, err := s.Context(ctx)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(public)
	if strings.Contains(string(body), "开心猫") || strings.Contains(string(body), "im_new") {
		t.Fatal("private image leaked")
	}
}

func TestPreviousNaturalRequestCanBeAppliedWhenImageArrives(t *testing.T) {
	s, f, g := expressionService(t)
	enablePrivateMemory(t, s, true)
	g.parts = []string{"把图发来吧"}
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "下回聊天你也用用我接下来发的这张图")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	s = clearExpressionCooldown(t, s, f, g)
	g.save = true
	g.parts = []string{"好，这张收好了"}
	picture := privateMessage(2, "789", "123", "")
	picture.Image = &model.ImageAttachment{URL: "https://message.biliimg.com/bfs/im_new/source.png"}
	f.messages = append(f.messages, picture)
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if g.calls != 2 || f.downloads != 1 {
		t.Fatal("one model decision per incoming message not preserved")
	}
}

func TestManualCallerCannotForgeBatchCooldownException(t *testing.T) {
	s, f, _ := expressionService(t)
	input := model.PrivateSendInput{RequestID: model.ID(), Text: "伪造分条", GroupID: model.ID(), AutoKey: "123", Part: 1}
	if _, err := s.SendPrivate(ctx, &input); err == nil || f.attempts != 0 {
		t.Fatal("forged batch reached platform")
	}
}

func TestContentActionsValidateActualSourceAndUnsupportedKinds(t *testing.T) {
	s, f, g := expressionService(t)
	enablePrivateMemory(t, s, true)
	g.parts = []string{"已经存好了"}
	g.save = true
	stranger := privateMessage(1, "456", "123", "")
	stranger.Image = &model.ImageAttachment{URL: "https://message.biliimg.com/bfs/im_new/other.png"}
	f.messages = []model.PrivateMessage{stranger, privateMessage(2, "789", "123", "帮我收着这张图")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	state, _ := s.PrivateOverview(ctx)
	if f.downloads != 0 || g.calls != 1 || !strings.Contains(state.State.Outbox[0].Text, "没有找到") {
		t.Fatal("source spoof or false success")
	}
	s = clearExpressionCooldown(t, s, f, g)
	g.save = false
	g.actions = []model.ContentAction{{Action: "read_content", ContentID: "file:unknown", Purpose: "reference"}}
	f.messages = append(f.messages, privateMessage(3, "789", "123", "帮我看一下那个文件"))
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	state, _ = s.PrivateOverview(ctx)
	if f.downloads != 0 || g.calls != 2 || !strings.Contains(state.State.Outbox[len(state.State.Outbox)-1].Text, "没有接入") {
		t.Fatal("unsupported operation pretended to execute")
	}
}

func TestLearningWordsAloneDoNotTriggerStorageWithoutModelAction(t *testing.T) {
	s, f, g := expressionService(t)
	enablePrivateMemory(t, s, true)
	g.parts = []string{"我们可以聊聊这张图"}
	picture := privateMessage(1, "789", "123", "")
	picture.Image = &model.ImageAttachment{URL: "https://message.biliimg.com/bfs/im_new/source.png"}
	f.messages = []model.PrivateMessage{picture}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	s = clearExpressionCooldown(t, s, f, g)
	g.parts = []string{"你希望我怎么处理它呢"}
	f.messages = append(f.messages, privateMessage(2, "789", "123", "学习一下"))
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if f.downloads != 0 || g.calls != 2 {
		t.Fatal("fixed words bypassed model decision")
	}
}

func TestSemanticSaveRespectsMemorySwitchAndDownloadFailure(t *testing.T) {
	s, f, g := expressionService(t)
	g.save = true
	g.parts = []string{"收好了"}
	picture := privateMessage(1, "789", "123", "")
	picture.Image = &model.ImageAttachment{URL: "https://message.biliimg.com/bfs/im_new/source.png"}
	f.messages = []model.PrivateMessage{picture}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	if f.downloads != 0 || g.calls != 1 {
		t.Fatal("disabled memory saved image")
	}
	enablePrivateMemory(t, s, true)
	s = clearExpressionCooldown(t, s, f, g)
	f.data = nil
	f.messages = append(f.messages, privateMessage(2, "789", "123", "把刚才那张留作表情吧"))
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	state, _ := s.PrivateOverview(ctx)
	files, _ := filepath.Glob(filepath.Join(s.Store.Root, "data/private/stickers/*/*.image"))
	if len(files) != 0 || g.calls != 2 || !strings.Contains(state.State.Outbox[len(state.State.Outbox)-1].Text, "没有保存成功") {
		t.Fatal("failed save falsely acknowledged")
	}
}

func TestNewBatchMayReuseShortPhraseWithoutReplayingWholeReply(t *testing.T) {
	s, f, g := expressionService(t)
	f.messages = []model.PrivateMessage{privateMessage(1, "789", "123", "今天聊得很开心呀")}
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal(err)
	}
	s = clearExpressionCooldown(t, s, f, g)
	g.parts = []string{"哈哈～", "换个话题吧"}
	f.messages = append(f.messages, privateMessage(2, "789", "123", "那我们接着聊"))
	if err := s.PrivateTick(ctx); err != nil {
		t.Fatal("short phrase blocked despite different complete reply", err)
	}
	if g.calls != 2 || f.attempts != 5 {
		t.Fatal("wrong batch counts", g.calls, f.attempts)
	}
}

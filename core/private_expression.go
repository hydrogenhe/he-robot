package core

import (
	"context"
	"crypto/rand"
	"gpt-robot/model"
	"math/big"
	"regexp"
	"strings"
	"time"
)

// privateReplyMaxParts 每次正常私聊回复最多发送三条，包含表情。
const privateReplyMaxParts = 3

// privateStickerChance 非严肃话题可提供表情候选的随机百分比。
const privateStickerChance = 18

// privatePartDelayMin 分条之间最短等待，单位毫秒；平台限速仍独立生效。
const privatePartDelayMin = 600

// privatePartDelayRange 为分条等待增加的随机范围，单位毫秒。
const privatePartDelayRange = 900

var seriousExpression = regexp.MustCompile(`去世|葬礼|自杀|自伤|不想活|重病|住院|分手|很难过|别发.*表情|不要.*表情`)
var requestedSticker = regexp.MustCompile(`来个.*表情|发个.*表情|发.*表情包`)

func randomExpressionDraw(n int) int {
	if n <= 1 {
		return 0
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(value.Int64())
}
func waitExpression(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) prepareExpression(ctx context.Context, round *conversationRound, prompt *model.ReplyPrompt) error {
	round.maxParts = 1 + s.ExpressionDraw(privateReplyMaxParts)
	prompt.ReplyParts = round.maxParts
	if seriousExpression.MatchString(prompt.Text) {
		return nil
	}
	items := []model.LearnedSticker{}
	if round.config.MemoryEnabled {
		var err error
		items, err = s.loadStickers(ctx, &round.scope)
		if err != nil {
			return err
		}
	}
	mentioned := false
	for _, item := range items {
		if len([]rune(item.Label)) >= 2 && item.Label != "你教我的表情" && strings.Contains(prompt.Text, item.Label) {
			mentioned = true
			break
		}
	}
	if !mentioned && !requestedSticker.MatchString(prompt.Text) && s.ExpressionDraw(100) >= privateStickerChance {
		return nil
	}
	native := s.Bili.NativeStickers()
	s.privateMu.Lock()
	recentStickers := map[string]bool{}
	for _, out := range s.privateState.Outbox {
		at, _ := time.Parse(time.RFC3339Nano, out.At)
		if out.StickerID != "" && time.Since(at) < 10*time.Minute {
			recentStickers[out.StickerID] = true
		}
	}
	s.privateMu.Unlock()
	for _, item := range native {
		if !recentStickers[item.Choice.ID] {
			round.native = append(round.native, item)
			prompt.Stickers = append(prompt.Stickers, item.Choice)
		}
	}
	if !round.config.MemoryEnabled {
		return nil
	}
	round.learned = map[string]model.LearnedSticker{}
	// 显式点名的旧表情优先，不能只因不在最近五张中就找不到。
	for i := len(items) - 1; i >= 0 && len(prompt.Stickers) < 8; i-- {
		item := items[i]
		if item.Label != "你教我的表情" && strings.Contains(prompt.Text, item.Label) && !recentStickers[item.ID] {
			round.learned[item.ID] = item
			prompt.Stickers = append(prompt.Stickers, model.StickerChoice{ID: item.ID, Label: item.Label})
		}
	}
	for i := len(items) - 1; i >= 0 && len(prompt.Stickers) < 8; i-- {
		item := items[i]
		if _, selected := round.learned[item.ID]; selected || recentStickers[item.ID] {
			continue
		}
		round.learned[item.ID] = item
		prompt.Stickers = append(prompt.Stickers, model.StickerChoice{ID: item.ID, Label: item.Label})
	}
	return nil
}

type privateReplyPart struct {
	Text      string
	StickerID string
	Image     *model.ImageAttachment
}
type privateReplyBatch struct {
	ID    string
	Hash  string
	Parts []privateReplyPart
	Next  int
}

func (s *Service) privateReplyParts(ctx context.Context, round *conversationRound, text string) ([]privateReplyPart, error) {
	parts := []privateReplyPart{}
	for _, message := range round.messages {
		parts = append(parts, privateReplyPart{Text: message})
	}
	if len(parts) == 0 && text != "" {
		parts = append(parts, privateReplyPart{Text: text})
	}
	if round.stickerID != "" && len(parts) < round.maxParts {
		for _, item := range round.native {
			if item.Choice.ID == round.stickerID {
				parts = append(parts, privateReplyPart{Text: item.TextCode, StickerID: item.Choice.ID})
				return parts, nil
			}
		}
		if item, ok := round.learned[round.stickerID]; ok {
			image, err := s.stickerImage(ctx, &round.scope, &item)
			if err != nil {
				return nil, err
			}
			parts = append(parts, privateReplyPart{Text: "[表情包：" + item.Label + "]", StickerID: item.ID, Image: image})
		}
	}
	if len(parts) > privateReplyMaxParts || len(parts) > round.maxParts {
		return nil, model.Fail("AI_REPLY", "整组回复超过系统上限")
	}
	return parts, nil
}

// sendPrivateReplyBatch 逐条调用共享发送入口，账本不把整组当作一次不可区分的发送。
func (s *Service) sendPrivateReplyBatch(ctx context.Context, round *conversationRound, text string, timings *model.OperationTimings) (*model.PrivateOutbox, string, bool, error) {
	parts, err := s.privateReplyParts(ctx, round, text)
	if err != nil {
		return nil, "", false, err
	}
	round.batch = &privateReplyBatch{ID: model.ID(), Hash: conversationDigest(parts), Parts: parts}
	var last *model.PrivateOutbox
	sent := []string{}
	for index, part := range parts {
		if index > 0 {
			delay := time.Duration(privatePartDelayMin+s.ExpressionDraw(privatePartDelayRange)) * time.Millisecond
			if err := s.ExpressionWait(ctx, delay); err != nil {
				return last, strings.Join(sent, " "), false, err
			}
			// 只读核对新来信；不向模型提交第二次请求，也不提前移动消费水位。
			page, err := s.Bili.PrivateMessages(ctx, round.config.SenderUID, round.config.PeerUID)
			if err != nil {
				return last, strings.Join(sent, " "), false, err
			}
			for _, message := range page.Messages {
				if message.Sender == round.config.PeerUID && message.Receiver == round.config.SenderUID && message.Seq > round.message.Seq && !message.Withdrawn {
					return last, strings.Join(sent, " "), false, nil
				}
			}
		}
		round.batch.Next = index
		input := model.PrivateSendInput{RequestID: model.ID(), Text: part.Text, AutoKey: round.message.Key, GroupID: round.batch.ID, Part: index, StickerID: part.StickerID}
		out, err := s.sendPrivate(ctx, &input, timings)
		if out != nil {
			last = out
			if out.Status == "sent" {
				sent = append(sent, out.Text)
			}
		}
		if err != nil {
			return last, strings.Join(sent, " "), false, err
		}
	}
	return last, strings.Join(sent, " "), true, nil
}

func validReplyBatch(ctx context.Context, input *model.PrivateSendInput) *privateReplyPart {
	round, _ := ctx.Value(conversationRoundKey{}).(*conversationRound)
	if round == nil || round.batch == nil || input.GroupID != round.batch.ID || input.AutoKey != round.message.Key || input.Part != round.batch.Next || input.Part < 0 || input.Part >= len(round.batch.Parts) {
		return nil
	}
	part := &round.batch.Parts[input.Part]
	if part.Text != input.Text || part.StickerID != input.StickerID {
		return nil
	}
	return part
}

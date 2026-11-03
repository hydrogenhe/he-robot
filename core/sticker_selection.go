package core

import (
	"context"
	"gpt-robot/model"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// selectReplySticker 在文字草稿完成后才抽取发送方式；可选模型调用失败保留文字。
func (s *Service) selectReplySticker(ctx context.Context, operationID string, round *conversationRound, prompt *model.ReplyPrompt, draft *model.GeneratedReply) error {
	intent := model.BoundStickerIntent(draft.StickerIntent)
	if intent == nil || !intent.Suitable || seriousExpression.MatchString(prompt.Text) || draft.Text == "" {
		return nil
	}
	// 沿用回忆的两轮用量边界；未知用量不追加模型调用。
	if draft.Usage == nil || draft.Usage.Input+draft.Usage.Output > 9904 {
		return nil
	}
	if s.modelRouter.Config().Provider != round.stickerProvider {
		return model.Fail("MODEL_CHANGED", "生成期间供应商已变化")
	}
	if s.ExpressionDraw(100) >= s.characterStickerChance(ctx) && !requestedSticker.MatchString(prompt.Text) {
		return nil
	}
	only := intent.ImageOnly && s.ExpressionDraw(2) == 0
	if !only && round.maxParts < 2 {
		round.maxParts = 2
	}
	candidates := s.stickerCandidates(ctx, round, intent, prompt.Text)
	if len(candidates) == 0 {
		return nil
	}
	selection := model.ReplyPrompt{
		SelectSticker: true,
		Channel:       "private-message",
		Text:          prompt.Text,
		Recent:        prompt.Recent,
		Draft:         draft.Text,
		StickerIntent: intent,
		Stickers:      candidates,
		Owner:         prompt.Owner,
	}
	bounded, err := model.BoundReplyPrompt(&selection)
	if err != nil || len(bounded.Stickers) == 0 {
		return nil
	}
	selected, err := s.generateReplyRound(ctx, operationID, prompt.Owner, bounded)
	if err != nil || selected == nil || selected.StickerID == "" {
		return nil
	}
	if s.modelRouter.Config().Provider != round.stickerProvider {
		return model.Fail("MODEL_CHANGED", "选图期间供应商已变化")
	}
	if selected.Usage != nil && draft.Usage.Input+draft.Usage.Output+selected.Usage.Input+selected.Usage.Output > 24000 {
		return nil
	}
	for _, candidate := range bounded.Stickers {
		if candidate.ID != selected.StickerID {
			continue
		}
		var image *model.ImageAttachment
		if item, ok := round.providerStickers[candidate.ID]; ok {
			image, err = readStickerFile(ctx, filepath.Join(s.providerStickerRoot(), providerStickerDirectory(round.stickerProvider)), &item)
		} else if item, ok := round.learned[candidate.ID]; ok {
			image, err = s.stickerImage(ctx, &round.scope, &item)
		}
		if err != nil {
			return nil
		}
		round.selectedImage = image
		round.stickerID = candidate.ID
		round.stickerOnly = only
		return nil
	}
	return nil
}

type scoredSticker struct {
	choice model.StickerChoice
	score  int
}

// stickerCandidates 仅按首轮语义标签召回，最多五张；场景和文字方向交由第二轮复核。
func (s *Service) stickerCandidates(ctx context.Context, round *conversationRound, intent *model.StickerIntent, text string) []model.StickerChoice {
	items, err := s.loadProviderStickers(ctx, round.stickerProvider)
	if err != nil {
		return nil
	}
	round.providerStickers = map[string]model.LearnedSticker{}
	for _, item := range items {
		round.providerStickers[item.ID] = item
	}
	round.learned = map[string]model.LearnedSticker{}
	if round.config.MemoryEnabled {
		learned, err := s.loadStickers(ctx, &round.scope)
		if err == nil {
			for _, item := range learned {
				if _, curated := round.providerStickers[item.ID]; !curated {
					round.learned[item.ID] = item
					items = append(items, item)
				}
			}
		}
	}
	choices := []model.StickerChoice{}
	for _, item := range items {
		choice := model.StickerChoice{ID: item.ID, Label: item.Label, StickerMeaning: item.StickerMeaning}
		choices = append(choices, choice)
	}
	round.native = s.Bili.NativeStickers()
	for _, item := range round.native {
		choices = append(choices, item.Choice)
	}
	s.privateMu.Lock()
	recent := map[string]bool{}
	for _, out := range s.privateState.Outbox {
		at, _ := time.Parse(time.RFC3339Nano, out.At)
		if out.StickerID != "" && time.Since(at) < 10*time.Minute {
			recent[out.StickerID] = true
		}
	}
	s.privateMu.Unlock()
	scored := []scoredSticker{}
	seen := map[string]bool{}
	for _, choice := range choices {
		if seen[choice.ID] {
			continue
		}
		seen[choice.ID] = true
		score := 0
		named := len([]rune(choice.Label)) >= 2 && choice.Label != "你教我的表情" && strings.Contains(text, choice.Label)
		if recent[choice.ID] && !named {
			continue
		}
		if named {
			score += 8
		}
		for _, tag := range intent.Tags {
			if strings.Contains(choice.Label, tag) {
				score += 2
			}
			for _, label := range choice.Tags {
				if label == tag {
					score += 3
					break
				}
			}
		}
		for _, scene := range choice.Scenes {
			if intent.Scene != "" && scene == intent.Scene {
				score += 2
				break
			}
		}
		if score > 0 {
			scored = append(scored, scoredSticker{choice: choice, score: score})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	result := []model.StickerChoice{}
	for _, item := range scored[:min(5, len(scored))] {
		result = append(result, item.choice)
	}
	return result
}

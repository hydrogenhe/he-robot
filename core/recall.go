package core

import (
	"context"
	"gpt-robot/model"
	"gpt-robot/storage"
	"time"
)

// completeRecall 最多一次模型提出的检索和一次追加生成，不执行模型提供的代码或路径。
func (s *Service) completeRecall(ctx context.Context, operationID string, prompt *model.ReplyPrompt, first *model.GeneratedReply, experience *model.DevelopmentInput) (*model.GeneratedReply, error) {
	// 输入正文与指令硬上限10000字节，另预留4096输出；首轮未知时不追加。
	if first.Usage == nil || first.Usage.Input+first.Usage.Output > 9904 {
		first.RecallQuery = ""
		return first, nil
	}
	next := *prompt
	next.AllowRecall = false
	next.RecallComplete = true
	next.Conversation = nil
	if prompt.Channel == "private-message" {
		if experience == nil || experience.BeforeSequence <= 0 {
			return first, nil
		}
		query := model.PrivateMemoryQuery{Query: &first.RecallQuery, BeforeSequence: experience.BeforeSequence, PageSize: model.ReplyMemoryLimit}
		page, err := s.PrivateMemories(ctx, &query)
		if err != nil {
			return nil, err
		}
		if !page.Enabled || page.Account != experience.Account || page.Peer != experience.Peer {
			return nil, model.Fail("PRIVATE_SCOPE", "回忆期间私聊授权已变化")
		}
		next.PrivateMemories = nil
		for _, record := range page.Items {
			if len(record.Sources) == 0 {
				continue
			}
			source := record.Sources[len(record.Sources)-1]
			item := model.RecalledMemory{Quote: memoryExcerpt(record.Quote, first.RecallQuery, 160), OccurredAt: source.OccurredAt, MessageKey: source.MessageKey}
			next.PrivateMemories = append(next.PrivateMemories, item)
		}
	} else {
		memories, err := s.publicReplyMemories(ctx, first.RecallQuery)
		if err != nil {
			return nil, err
		}
		next.PublicMemories = memories
	}
	episodes, err := s.recallEpisodes(ctx, first.RecallQuery, prompt.Channel, experience)
	if err != nil {
		return nil, err
	}
	next.Episodes = episodes
	// 检索请求不是已发送回复，释放供应商窗口，下一条使用已核验的本地交接。
	s.modelRouter.CloseConversations()
	s.sanitizeReplyPrompt(ctx, &next)
	bounded, err := model.BoundReplyPrompt(&next)
	if err != nil {
		return nil, err
	}
	result, err := s.generateReplyRound(ctx, operationID, prompt.Owner, bounded)
	if err != nil {
		return nil, err
	}
	result.RecallQuery = ""
	if result.Usage != nil && first.Usage.Input+first.Usage.Output+result.Usage.Input+result.Usage.Output > 24000 {
		return nil, model.Fail("CONTEXT_BUDGET", "本次回忆已知用量超过上限，未继续生成")
	}
	return result, nil
}

func (s *Service) recallEpisodes(ctx context.Context, text, channel string, experience *model.DevelopmentInput) ([]model.RecalledEpisode, error) {
	if s.Store.Database == nil {
		return nil, nil
	}
	input := storage.MemoryQuery{Terms: memoryTokens(text), Limit: 3}
	if channel == "private-message" {
		if experience == nil {
			return nil, nil
		}
		input.Platform = experience.Platform
		input.Account = experience.Account
		input.Peer = experience.Peer
	}
	if date := memoryQueryDate(text, time.Now()); date != "" {
		if at, err := time.ParseInLocation("2006-01-02", date, time.FixedZone("Asia/Shanghai", 8*60*60)); err == nil {
			input.Since = at.UnixMilli()
			input.Until = at.AddDate(0, 0, 1).UnixMilli()
		}
	}
	return s.Store.Database.RecallEpisodes(ctx, &input)
}

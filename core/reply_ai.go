package core

import (
	"context"
	"gpt-robot/model"
	"strings"
	"time"
)

func (s *Service) generatePrivateReply(ctx context.Context, operationID string, message *model.PrivateMessage, recent []string) (string, error) {
	s.privateMu.Lock()
	config := s.privateConfig
	processing := s.privateState.Processing
	s.privateMu.Unlock()
	if message == nil || message.Sender != config.PeerUID || message.Receiver != config.SenderUID || processing != message.Key || !config.Enabled {
		return "", model.Fail("PRIVATE_SCOPE", "私信生成范围已变化")
	}
	memories := []model.RecalledMemory{}
	if config.MemoryEnabled {
		queryText := model.Clip(message.Text, 100)
		query := model.PrivateMemoryQuery{Query: &queryText, BeforeSequence: message.Seq, PageSize: model.ReplyMemoryLimit}
		page, err := s.PrivateMemories(ctx, &query)
		if err != nil {
			return "", err
		}
		if page.Enabled && page.Account == config.SenderUID && page.Peer == config.PeerUID {
			for _, record := range page.Items {
				source := record.Sources[len(record.Sources)-1]
				memory := model.RecalledMemory{Quote: memoryExcerpt(record.Quote, message.Text, 160), OccurredAt: source.OccurredAt, MessageKey: source.MessageKey}
				memories = append(memories, memory)
			}
		}
	}
	s.privateMu.Lock()
	current := s.privateConfig
	valid := current == config && s.privateState.Processing == message.Key
	s.privateMu.Unlock()
	if !valid {
		return "", model.Fail("PRIVATE_SCOPE", "私聊授权在生成前已变化")
	}
	var experience *model.DevelopmentInput
	if config.MemoryEnabled {
		experience = &model.DevelopmentInput{BeforeSequence: message.Seq, Platform: "bilibili", Account: config.SenderUID, Peer: config.PeerUID, Visibility: "private", Source: "message:" + message.Key, At: time.Unix(message.Timestamp, 0).UTC().Format(time.RFC3339Nano), Text: message.Text, Interaction: true}
	}
	return s.generateReplyWithMemories(ctx, operationID, "private-message", message.Text, recent, memories, experience)
}
func (s *Service) generateReplyWithMemories(ctx context.Context, operationID, channel, text string, recent []string, memories []model.RecalledMemory, experience *model.DevelopmentInput) (string, error) {
	if channel != "private-message" && len(memories) > 0 {
		return "", model.Fail("PRIVATE_SCOPE", "私聊记忆不可用于公开生成")
	}

	if !s.robotRunning() {
		return "", model.Fail("ROBOT_PAUSED", "机器人已暂停")
	}
	var memoryEpoch int64
	if s.Store.Database != nil {
		var err error
		memoryEpoch, err = s.Store.Database.AuthEpoch(ctx)
		if err != nil {
			return "", err
		}
	}
	persona, err := s.Store.Text(ctx, "data/character/persona.md")
	if err != nil {
		return "", err
	}
	state, err := s.CharacterStatus(ctx)
	if err != nil {
		return "", err
	}
	// 私聊入口已核验唯一绑定主人；不能把是否开启记忆当成主人身份条件。
	owner := channel == "private-message" || s.isOwnerExperience(experience)
	prompt := model.ReplyPrompt{AllowRecall: s.Store.Database != nil && (channel != "private-message" || experience != nil), Owner: owner, PrivateMemories: memories, State: state.Description, Persona: model.Clip(persona, 800), Channel: channel, Text: text, Recent: recent}
	if err := s.flushGrowth(ctx); err != nil {
		return "", err
	}
	prompt.Episodes, err = s.recallEpisodes(ctx, text, channel, experience)
	if err != nil {
		return "", err
	}
	prompt.Development = s.developmentContext(experience)
	prompt.InterestCandidates = s.developmentCandidates(text, persona)
	if channel == "video-comment" {
		prompt.PublicMemories, err = s.publicReplyMemories(ctx, text)
		if err != nil {
			return "", err
		}
	}
	s.sanitizeReplyPrompt(ctx, &prompt)
	round, _ := ctx.Value(conversationRoundKey{}).(*conversationRound)
	if round != nil && channel == "private-message" {
		if err := s.prepareConversation(ctx, round, &prompt); err != nil {
			return "", err
		}
		s.preparePrivateContents(round, &prompt)
		if s.selfQueryAuthorized(ctx, round) {
			prompt.SelfCapabilities = []string{"diary", "memories", "experiences", "growth", "activities", "status", "write_diary"}
			prompt.SelfToday = diaryDay(s.DiaryNow())
			prompt.CompanionContext, err = s.companionContext(ctx, &round.config)
			if err != nil {
				return "", err
			}
		}
		if err := s.prepareExpression(ctx, round, &prompt); err != nil {
			return "", err
		}
		s.sanitizeReplyPrompt(ctx, &prompt)
	}
	bounded, err := model.BoundReplyPrompt(&prompt)
	if err != nil {
		return "", err
	}
	result, err := s.generateReplyRound(ctx, operationID, owner, bounded)
	if err != nil {
		return "", err
	}
	cue := result.Companion
	if result.SelfAction != nil {
		result, err = s.completeSelfAction(ctx, operationID, round, bounded, result)
		if err != nil {
			return "", err
		}
	} else if result.RecallQuery != "" && bounded.AllowRecall {
		result, err = s.completeRecall(ctx, operationID, bounded, result, experience)
		if err != nil {
			return "", err
		}
	}
	if s.Store.Database != nil {
		epoch, epochErr := s.Store.Database.AuthEpoch(ctx)
		if epochErr != nil {
			return "", epochErr
		}
		if epoch != memoryEpoch {
			return "", model.Fail("MEMORY_CHANGED", "记忆在生成期间已变化，未发送旧上下文回复")
		}
	}
	if round != nil {
		round.remember = result.Remember
		round.messages = result.Messages
		round.stickerID = result.StickerID
		if len(round.messages) > round.maxParts {
			return "", model.Fail("AI_REPLY", "回复分条超过本轮上限")
		}
		for _, part := range round.messages {
			if !model.Text(part, 60) || model.ContainsSensitiveText(part, s.generationSecrets(ctx)...) {
				return "", model.Fail("SENSITIVE_OUTPUT", "回复分条含疑似凭证或内容超限，未发送")
			}
		}
		if len(round.messages) > 0 {
			result.Text = strings.Join(round.messages, " ")
			if !model.Text(result.Text, 60) {
				return "", model.Fail("AI_REPLY", "整组回复超过60字")
			}
		}
	}
	if model.ContainsSensitiveText(result.Text, s.generationSecrets(ctx)...) {
		return "", model.Fail("SENSITIVE_OUTPUT", "回复含疑似凭证，已拦截，未发送")
	}
	if round != nil {
		if err := s.applyCompanionCue(ctx, round, cue); err != nil {
			return "", err
		}
		s.contentActionFeedback(ctx, round, result)
		round.messages = result.Messages
		round.stickerID = result.StickerID
	}
	if err := s.applyCharacterSignal(ctx, operationID, channel, result.Signal); err != nil {
		return "", err
	}
	if experience != nil {
		record := *experience
		record.Reaction = model.Clip(result.Text, 60)
		if result.Signal != nil {
			record.Recommended = result.Signal.Recommended
			record.Interests = result.Signal.Interests
			record.RelationDelta = result.Signal.Warmth
			record.Novelty = result.Signal.Novelty
			record.Resonance = result.Signal.Resonance
			record.Effort = result.Signal.Effort
		}
		if err := s.RecordDevelopment(ctx, &record); err != nil {
			return "", err
		}
	}
	if result.Text != "" && !model.Text(result.Text, 60) {
		return "", model.Fail("AI_REPLY", "模型回复超过60字")
	}
	if !s.robotRunning() || ctx.Err() != nil {
		return "", model.Fail("ROBOT_PAUSED", "机器人已暂停，未发送生成内容")
	}
	return result.Text, nil
}

func (s *Service) generateReplyRound(ctx context.Context, operationID string, owner bool, prompt *model.ReplyPrompt) (*model.GeneratedReply, error) {
	day, err := s.reserveGeneration(ctx, owner)
	if err != nil {
		return nil, err
	}
	result, runErr := s.Generator.Generate(ctx, prompt)
	var actual *model.TokenUsage
	if result != nil {
		actual = result.Usage
	}
	if err := s.settleGeneration(context.WithoutCancel(ctx), day, owner, actual); err != nil {
		runErr = err
	}
	// 即使生成失败或随后暂停，也保留宿主已经返回的真实用量；缺失保持未知。
	if result != nil && result.Usage != nil {
		report := model.UsageReport{OperationID: operationID, Usage: *result.Usage}
		if err := s.reportUsage(context.WithoutCancel(ctx), &report, generatedUsageSource(result.Usage)); err != nil {
			return nil, err
		}
	}
	if runErr != nil {
		return nil, runErr
	}
	if result == nil {
		return nil, model.Fail("AI_REPLY", "模型没有返回结果")
	}

	return result, nil
}

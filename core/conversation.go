package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"gpt-robot/model"
	"strings"
	"time"
)

type conversationRoundKey struct{}
type conversationRound struct {
	maxParts  int
	messages  []string
	stickerID string
	native    []model.NativeSticker
	learned   map[string]model.LearnedSticker
	batch     *privateReplyBatch
	contents  map[string]model.PrivateMessage
	scope     model.ConversationScope
	config    model.PrivateConfig
	message   model.PrivateMessage
	handoff   model.ConversationHandoff
	remember  []string
	user      string
	epoch     string
}

func conversationDigest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func handoffFile(scope *model.ConversationScope) string {
	identity := []string{scope.Platform, scope.Account, scope.Peer, scope.Channel}
	return "data/private/conversations/" + conversationDigest(identity) + ".json"
}

type conversationHandoffState struct {
	Epoch      string                    `json:"epoch,omitempty"`
	Generation string                    `json:"generation"`
	Handoff    model.ConversationHandoff `json:"handoff"`
}

func (s *Service) initializeConversations(ctx context.Context) error {
	config := model.DefaultConversationConfig()
	if _, err := s.Store.Read(ctx, "config/conversation.json", &config); err != nil {
		return err
	}
	return s.modelRouter.ConfigureConversations(&config)
}

func (s *Service) ConversationStatus(ctx context.Context) (*model.ConversationStatus, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	status := s.modelRouter.ConversationStatus()
	return &status, nil
}

func (s *Service) SaveConversationConfig(ctx context.Context, input *model.ConversationConfig) (*model.ConversationStatus, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	s.conversationMu.Lock()
	defer s.conversationMu.Unlock()
	if err := s.Store.Write(ctx, "config/conversation.json", input); err != nil {
		return nil, err
	}
	if err := s.modelRouter.ConfigureConversations(input); err != nil {
		return nil, err
	}
	return s.ConversationStatus(ctx)
}

func (s *Service) prepareConversation(ctx context.Context, round *conversationRound, prompt *model.ReplyPrompt) error {
	baseRevision := conversationDigest([]any{"conversation-policy-v1", round.config, prompt.Persona, prompt.Owner})
	round.scope.Revision = conversationDigest([]string{baseRevision, ""})
	prompt.Conversation = &round.scope
	round.user = prompt.Text
	s.privateMu.Lock()
	withdrawn := false
	for _, m := range s.privateState.Inbox {
		if m.Withdrawn {
			withdrawn = true
			break
		}
	}
	s.privateMu.Unlock()
	if withdrawn {
		s.modelRouter.CloseConversations()
	}
	if !round.config.MemoryEnabled {
		return nil
	}
	s.conversationMu.Lock()
	defer s.conversationMu.Unlock()
	stored := conversationHandoffState{}
	if _, err := s.Store.Read(ctx, handoffFile(&round.scope), &stored); err != nil {
		return err
	}
	if stored.Generation != round.config.MemoryGeneration {
		return nil
	}
	// 平台撤回立即使旧短期窗口和交接失效，不把已撤回原文留在模型会话里。
	if withdrawn {
		stored = conversationHandoffState{Generation: round.config.MemoryGeneration, Epoch: model.ID()}
		if err := s.Store.Write(ctx, handoffFile(&round.scope), &stored); err != nil {
			return err
		}
	}
	round.epoch = stored.Epoch
	round.scope.Revision = conversationDigest([]string{baseRevision, stored.Epoch})
	round.handoff = stored.Handoff
	for _, fact := range stored.Handoff.Facts[max(0, len(stored.Handoff.Facts)-2):] {
		if model.ValidNumber(fact.MessageKey) && fact.Quote != "" {
			prompt.Handoff = append(prompt.Handoff, "用户陈述 "+fact.OccurredAt+" #"+fact.MessageKey+"："+model.Clip(fact.Quote, 120))
		}
	}
	for _, text := range stored.Handoff.Recent[max(0, len(stored.Handoff.Recent)-2):] {
		prompt.Handoff = append(prompt.Handoff, model.Clip(text, 120))
	}
	return nil
}

// finishConversation 仅在平台明确sent后保存有限交接，空回复、失败、取消和未知结果不进入下一轮。
func (s *Service) finishConversation(ctx context.Context, round *conversationRound, reply string, sent bool) (resultErr error) {
	defer func() { s.modelRouter.FinishConversation(&round.scope, sent && resultErr == nil) }()
	if !sent || !round.config.MemoryEnabled {
		return nil
	}
	s.privateMu.Lock()
	same := s.privateConfig == round.config
	s.privateMu.Unlock()
	if !same {
		return nil
	}
	s.conversationMu.Lock()
	defer s.conversationMu.Unlock()
	current := conversationHandoffState{}
	if _, err := s.Store.Read(ctx, handoffFile(&round.scope), &current); err != nil {
		return err
	}
	if current.Epoch != round.epoch {
		return nil
	}
	state := round.handoff
	for _, quote := range round.remember {
		if !worthPrivateMemory(quote) || !strings.Contains(round.user, quote) || model.ContainsSensitiveText(quote, s.generationSecrets(ctx)...) {
			continue
		}
		fact := model.RecalledMemory{Quote: quote, OccurredAt: time.Unix(round.message.Timestamp, 0).UTC().Format(time.RFC3339), MessageKey: round.message.Key}
		state.Facts = append(state.Facts, fact)
	}
	if len(state.Facts) > 8 {
		state.Facts = state.Facts[len(state.Facts)-8:]
	}
	state.Recent = append(state.Recent, "对方："+model.Clip(round.user, 120), "已发送回复："+model.Clip(reply, 120))
	if len(state.Recent) > 4 {
		state.Recent = state.Recent[len(state.Recent)-4:]
	}
	stored := conversationHandoffState{Epoch: round.epoch, Generation: round.config.MemoryGeneration, Handoff: state}
	return s.Store.Write(ctx, handoffFile(&round.scope), &stored)
}

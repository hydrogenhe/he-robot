package core

import (
	"context"
	"gpt-robot/model"
	"time"
)

func defaultGenerationSafety() model.GenerationSafetyConfig {
	return model.GenerationSafetyConfig{OwnerDailyCalls: 1000, OtherDailyCalls: 100, OwnerDailyTokens: 2000000, OtherDailyTokens: 200000}
}
func validateGenerationSafety(c *model.GenerationSafetyConfig) error {
	if c == nil || c.OwnerDailyCalls < 0 || c.OwnerDailyCalls > 10000 || c.OtherDailyCalls < 1 || c.OtherDailyCalls > 1000 || c.OwnerDailyTokens < 0 || c.OwnerDailyTokens > 100000000 || c.OtherDailyTokens < 1000 || c.OtherDailyTokens > 10000000 {
		return model.Fail("INPUT", "主人调用上限0–10000、Token上限0–1亿（0关闭该项）；其他用户调用1–1000、Token上限1000–1000万")
	}
	return nil
}
func (s *Service) initializeGenerationSafety(ctx context.Context) error {
	config := defaultGenerationSafety()
	if _, err := s.Store.Read(ctx, "config/generation-safety.json", &config); err != nil {
		return err
	}
	if err := validateGenerationSafety(&config); err != nil {
		return err
	}
	s.generationSafety = config
	if _, err := s.Store.Read(ctx, "data/usage/generation-safety.json", &s.generationState); err != nil {
		return err
	}
	// 中断的调用保留次数；未知Token不能伪装为0。
	s.generationState.Owner.Unknown += s.generationState.Owner.Pending
	s.generationState.Other.Unknown += s.generationState.Other.Pending
	s.generationState.Owner.Pending = 0
	s.generationState.Other.Pending = 0
	return s.Store.Write(ctx, "data/usage/generation-safety.json", &s.generationState)
}
func (s *Service) generationDay() string { return diaryDay(time.Now()) }
func (s *Service) GenerationSafety(ctx context.Context) (*model.GenerationSafetyStatus, error) {
	s.generationMu.Lock()
	defer s.generationMu.Unlock()
	state := s.generationState
	if state.Day != s.generationDay() {
		state = model.GenerationSafetyState{Day: s.generationDay()}
	}
	return &model.GenerationSafetyStatus{Config: s.generationSafety, State: state}, ctx.Err()
}
func (s *Service) SaveGenerationSafety(ctx context.Context, c *model.GenerationSafetyConfig) (*model.GenerationSafetyStatus, error) {
	if err := validateGenerationSafety(c); err != nil {
		return nil, err
	}
	s.generationMu.Lock()
	err := s.Store.Write(ctx, "config/generation-safety.json", c)
	if err == nil {
		s.generationSafety = *c
	}
	s.generationMu.Unlock()
	if err != nil {
		return nil, err
	}
	s.modelRouter.CloseConversations()
	return s.GenerationSafety(ctx)
}
func (s *Service) reserveGeneration(ctx context.Context, owner bool) (string, error) {
	s.generationMu.Lock()
	defer s.generationMu.Unlock()
	state := s.generationState
	day := s.generationDay()
	if state.Day != day {
		if state.Owner.Pending+state.Other.Pending > 0 {
			return "", model.Fail("BUSY", "上一日生成尚未结算")
		}
		state = model.GenerationSafetyState{Day: day}
	}
	usage := &state.Other
	calls, tokens := s.generationSafety.OtherDailyCalls, s.generationSafety.OtherDailyTokens
	if owner {
		usage = &state.Owner
		calls, tokens = s.generationSafety.OwnerDailyCalls, s.generationSafety.OwnerDailyTokens
	}
	if (calls > 0 && usage.Calls >= calls) || (tokens > 0 && usage.Tokens >= tokens) {
		return "", model.Fail("GENERATION_LIMIT", "今日生成预算已达上限，请在大模型配置中调整；未调用模型")
	}
	// 同一身份有未结算调用时拒绝并发，避免并发穿透预算。
	if usage.Pending > 0 {
		return "", model.Fail("BUSY", "当前生成尚未结算")
	}
	usage.Calls++
	usage.Pending++
	if err := s.Store.Write(ctx, "data/usage/generation-safety.json", &state); err != nil {
		return "", err
	}
	s.generationState = state
	return day, nil
}
func (s *Service) settleGeneration(ctx context.Context, day string, owner bool, usage *model.TokenUsage) error {
	s.generationMu.Lock()
	defer s.generationMu.Unlock()
	// 跨午夜完成仍归预留调用日；下一次预留才重置日期。
	if s.generationState.Day != day {
		return model.Fail("GENERATION_STATE", "生成用量账本日期已变化")
	}
	state := s.generationState
	row := &state.Other
	if owner {
		row = &state.Owner
	}
	if row.Pending > 0 {
		row.Pending--
	}
	if usage == nil || usage.Input < 0 || usage.Output < 0 {
		row.Unknown++
	} else {
		row.Tokens += usage.Input + usage.Output
	}
	if err := s.Store.Write(ctx, "data/usage/generation-safety.json", &state); err != nil {
		return err
	}
	s.generationState = state
	return nil
}
func (s *Service) isOwnerExperience(e *model.DevelopmentInput) bool {
	if e == nil || e.Platform != "bilibili" {
		return false
	}
	if e.Visibility == "private" {
		s.privateMu.Lock()
		defer s.privateMu.Unlock()
		return e.Account == s.privateConfig.SenderUID && e.Peer == s.privateConfig.PeerUID && e.Peer != ""
	}
	s.autoMu.Lock()
	defer s.autoMu.Unlock()
	return e.Account == s.autoConfig.AccountUID && e.Peer == s.autoConfig.PriorityUID && e.Peer != ""
}

// 平台凭证仅用于逐值脱敏，不拼入提示词或错误。
func (s *Service) generationSecrets(ctx context.Context) []string {
	values := []string{}
	if c, err := s.Credentials(ctx); err == nil && c != nil {
		for _, v := range c.Cookies {
			values = append(values, v)
		}
	}
	return values
}
func (s *Service) sanitizeReplyPrompt(ctx context.Context, p *model.ReplyPrompt) {
	secrets := s.generationSecrets(ctx)
	if p.SelfResult != nil {
		for i := range p.SelfResult.Records {
			p.SelfResult.Records[i].Title = model.RedactSensitiveText(p.SelfResult.Records[i].Title, secrets...)
			p.SelfResult.Records[i].Text = model.RedactSensitiveText(p.SelfResult.Records[i].Text, secrets...)
		}
	}
	p.CompanionContext = model.RedactSensitiveText(p.CompanionContext, secrets...)
	p.Text = model.RedactSensitiveText(p.Text, secrets...)
	p.Persona = model.RedactSensitiveText(p.Persona, secrets...)
	p.State = model.RedactSensitiveText(p.State, secrets...)
	p.Development = model.RedactSensitiveText(p.Development, secrets...)
	for i := range p.Recent {
		p.Recent[i] = model.RedactSensitiveText(p.Recent[i], secrets...)
	}
	for i := range p.Handoff {
		p.Handoff[i] = model.RedactSensitiveText(p.Handoff[i], secrets...)
	}
	for i := range p.Stickers {
		p.Stickers[i].Label = model.RedactSensitiveText(p.Stickers[i].Label, secrets...)
	}
	for i := range p.PrivateMemories {
		p.PrivateMemories[i].Quote = model.RedactSensitiveText(p.PrivateMemories[i].Quote, secrets...)
	}
	for i := range p.Episodes {
		p.Episodes[i].Summary = model.RedactSensitiveText(p.Episodes[i].Summary, secrets...)
		p.Episodes[i].Reaction = model.RedactSensitiveText(p.Episodes[i].Reaction, secrets...)
	}
	for i := range p.PublicMemories {
		p.PublicMemories[i].Summary = model.RedactSensitiveText(p.PublicMemories[i].Summary, secrets...)
	}
}

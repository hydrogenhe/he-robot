package core

import (
	"context"
	"gpt-robot/model"
	"maps"
	"slices"
	"time"
)

func defaultAutoConfig() model.AutoReplyConfig {
	c := model.AutoReplyConfig{Videos: []string{}, IntervalSeconds: 300, OtherDailyLimit: 30, PerUserDailyLimit: 5}
	return c
}
func (s *Service) initializeAuto(ctx context.Context) error {
	s.autoMu.Lock()
	defer s.autoMu.Unlock()
	config := defaultAutoConfig()
	if _, err := s.Store.Read(ctx, "config/auto-reply.json", &config); err != nil {
		return err
	}
	if err := validateAutoConfig(&config); err != nil {
		return err
	}
	state := model.AutoReplyState{}
	if _, err := s.Store.Read(ctx, "data/automation/state.json", &state); err != nil {
		return err
	}
	if state.Videos == nil {
		state.Videos = map[string]model.AutoVideoState{}
	}
	if state.Entries == nil {
		state.Entries = map[string]model.AutoEntry{}
	}
	changed := false
	for key, entry := range state.Entries {
		if entry.Status == model.AutoProcessing {
			entry.Status = model.AutoInterrupted
			entry.Reason = "上次执行中断，不自动重试"
			state.Entries[key] = entry
			state.Paused = true
			state.Reason = entry.Reason
			changed = true
		}
	}
	s.autoConfig = config
	s.autoState = state
	if changed {
		return s.Store.Write(ctx, "data/automation/state.json", state)
	}
	return nil
}
func validateAutoConfig(c *model.AutoReplyConfig) error {
	if c == nil {
		return model.Fail("INPUT", "缺少自动回复设置")
	}
	if c.Source != "" && c.Source != "browsing" {
		return model.Fail("INPUT", "不支持的回复来源")
	}
	if c.IntervalSeconds < 120 || c.IntervalSeconds > 3600 || c.OtherDailyLimit < 1 || c.OtherDailyLimit > 100 || c.PerUserDailyLimit < 1 || c.PerUserDailyLimit > 20 || len(c.Videos) > 20 {
		return model.Fail("INPUT", "检查间隔120–3600秒，他人日限额1–100次、单人日限额1–20次，最多20个视频")
	}
	if c.Enabled && ((c.Source != "browsing" && len(c.Videos) == 0) || !model.ValidNumber(c.AccountUID) || !model.ValidNumber(c.PriorityUID) || c.AccountUID == "0" || c.PriorityUID == "0" || c.AccountUID == c.PriorityUID) {
		return model.Fail("INPUT", "启用前必须绑定不同的机器人账号、优先账号以及视频")
	}
	for _, bv := range c.Videos {
		if !model.ValidBV(bv) {
			return model.Fail("INPUT", "自动回复视频BV号不正确")
		}
	}
	return nil
}
func (s *Service) SaveAutoConfig(ctx context.Context, input *model.AutoReplyConfig) (*model.AutoReplyOverview, error) {
	if err := validateAutoConfig(input); err != nil {
		return nil, err
	}
	config := *input
	config.Videos = slices.Clone(input.Videos)
	slices.Sort(config.Videos)
	config.Videos = slices.Compact(config.Videos)
	if config.Enabled {
		permissions, err := s.Permissions(ctx)
		if err != nil {
			return nil, err
		}
		if !permissions.Enabled && config.Source != "browsing" {
			return nil, model.Fail("DENIED", "请先开启平台互动发送权限")
		}
		for _, bv := range config.Videos {
			if config.Source != "browsing" && !slices.Contains(permissions.AllowedVideos, bv) {
				return nil, model.Fail("DENIED", "监听视频必须在互动权限允许列表中")
			}
		}
		account, err := s.Bili.Status(ctx)
		if err != nil {
			return nil, err
		}
		if account.UID != config.AccountUID {
			return nil, model.Fail("ACCOUNT", "当前登录账号与机器人绑定UID不一致")
		}
	}
	s.autoMu.Lock()
	old := s.autoConfig
	state := s.autoState
	state.Entries = maps.Clone(s.autoState.Entries)
	resetScope := !config.Enabled || !old.Enabled || old.AccountUID != config.AccountUID || old.PriorityUID != config.PriorityUID || old.Source != config.Source || !slices.Equal(old.Videos, config.Videos)
	if resetScope {
		for key, entry := range state.Entries {
			if entry.Status == model.AutoQueued {
				entry.Status = model.AutoSkipped
				entry.Reason = "自动回复关闭或范围改变，旧队列不再发送"
				state.Entries[key] = entry
			}
		}
	}
	if !old.Enabled || old.AccountUID != config.AccountUID || old.PriorityUID != config.PriorityUID || old.Source != config.Source || !slices.Equal(old.Videos, config.Videos) {
		state.Videos = map[string]model.AutoVideoState{}
	}
	state.Revision++
	state.Paused = false
	state.Reason = ""
	state.NextAt = ""
	// 范围配置最后提交，配置保存失败不会在重启后悄悄启用新的发送范围。
	if err := s.Store.Write(ctx, "data/automation/state.json", &state); err != nil {
		s.autoMu.Unlock()
		return nil, err
	}
	if err := s.Store.Write(ctx, "config/auto-reply.json", &config); err != nil {
		s.autoMu.Unlock()
		return nil, err
	}
	s.autoConfig = config
	s.autoState = state
	s.autoMu.Unlock()
	select {
	case s.autoWake <- struct{}{}:
	default:
	}
	return s.AutoOverview(ctx)
}
func autoUsage(drafts []model.Draft, config *model.AutoReplyConfig, now time.Time) (int, map[string]int) {
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	day := now.In(zone).Format("2006-01-02")
	other := 0
	users := map[string]int{}
	for _, d := range drafts {
		if d.AutoCommentID == "" || d.Account != config.AccountUID || d.TargetUser == config.PriorityUID || d.AttemptAt == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, d.AttemptAt)
		if err == nil && at.In(zone).Format("2006-01-02") == day {
			other++
			users[d.TargetUser]++
		}
	}
	return other, users
}
func (s *Service) autoSendAllowed(d *model.Draft, drafts []model.Draft, now time.Time) error {
	s.autoMu.Lock()
	config := s.autoConfig
	paused := s.autoState.Paused
	s.autoMu.Unlock()
	if !config.Enabled || paused || !s.robotRunning() {
		return model.Fail("AUTO_DISABLED", "自动回复已关闭或暂停")
	}
	videoAllowed := slices.Contains(config.Videos, d.BVID)
	if config.Source == "browsing" {
		videoAllowed = slices.Contains(s.browsingTargets(config.AccountUID), d.BVID)
	}
	if config.AccountUID != d.Account || !videoAllowed {
		return model.Fail("ACCOUNT", "自动回复账号或视频已不在授权范围")
	}
	decision := evaluateConversation(d.TargetText, d.TargetUser == config.PriorityUID)
	if !decision.Allowed {
		return model.Fail("TARGET", "目标内容或自动回复规则已变化")
	}
	if d.TargetUser == config.AccountUID {
		return model.Fail("DENIED", "不自动回复自己")
	}
	if d.TargetUser == config.PriorityUID {
		return nil
	}
	other, users := autoUsage(drafts, &config, now)
	if other >= config.OtherDailyLimit {
		return model.Fail("AUTO_LIMIT", "今天对其他用户的自动回复次数已满")
	}
	if users[d.TargetUser] >= config.PerUserDailyLimit {
		return model.Fail("AUTO_USER_LIMIT", "今天对该用户的自动回复已达设置上限")
	}
	return nil
}
func (s *Service) AutoOverview(ctx context.Context) (*model.AutoReplyOverview, error) {
	drafts, err := s.Drafts(ctx)
	if err != nil {
		return nil, err
	}
	s.autoMu.Lock()
	defer s.autoMu.Unlock()
	config := s.autoConfig
	config.Videos = slices.Clone(config.Videos)
	other, users := autoUsage(drafts, &config, time.Now())
	entries := []model.AutoEntry{}
	for _, entry := range s.autoState.Entries {
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, compareAutoEntries)
	if len(entries) > 100 {
		entries = entries[:100]
	}
	result := &model.AutoReplyOverview{SourceReady: config.Source != "browsing" || len(s.browsingTargets(config.AccountUID)) > 0, Config: config, Paused: s.autoState.Paused, Reason: s.autoState.Reason, LastAt: s.autoState.LastAt, NextAt: s.autoState.NextAt, Entries: entries, OtherUsed: other, UserUsed: users}
	return result, nil
}
func compareAutoEntries(a, b model.AutoEntry) int {
	if a.At > b.At {
		return -1
	}
	if a.At < b.At {
		return 1
	}
	if a.Key > b.Key {
		return -1
	}
	if a.Key < b.Key {
		return 1
	}
	return 0
}

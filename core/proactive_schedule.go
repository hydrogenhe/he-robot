package core

import (
	"context"
	"fmt"
	"gpt-robot/model"
	"slices"
	"time"
)

// proactiveMinimumGap 主动消息间隔下限，单位分钟；避免集中打扰。
const proactiveMinimumGap = 30

func proactiveQuiet(now time.Time) bool {
	hour := now.In(time.FixedZone("Asia/Shanghai", 8*3600)).Hour()
	return hour < 9
}
func proactiveNextMorning(now time.Time) time.Time {
	local := now.In(time.FixedZone("Asia/Shanghai", 8*3600))
	day := time.Date(local.Year(), local.Month(), local.Day(), 9, 0, 0, 0, local.Location())
	if !day.After(now) {
		day = day.AddDate(0, 0, 1)
	}
	return day
}
func (s *Service) initializeProactive(ctx context.Context) error {
	c := model.ProactiveConfig{RandomMin: 3, RandomMax: 5, VideoDailyLimit: 10}
	if _, err := s.Store.Read(ctx, "config/proactive.json", &c); err != nil {
		return err
	}
	if c.RandomMin < 0 || c.RandomMax < c.RandomMin || c.RandomMax > 5 || c.VideoDailyLimit < 1 || c.VideoDailyLimit > 10 {
		return model.Fail("INPUT", "主动聊天每天0–5次，视频每天1–10个")
	}
	s.proactiveConfig = c
	if _, err := s.Store.Read(ctx, "data/private/proactive.json", &s.proactiveState); err != nil {
		return err
	}
	for i := range s.proactiveState.Jobs {
		j := &s.proactiveState.Jobs[i]
		if j.Status == "generating" || j.Status == "sending" {
			j.Status = "unknown"
			j.ErrorCode = "INTERRUPTED"
		}
	}
	return s.saveProactive(ctx)
}
func (s *Service) saveProactive(ctx context.Context) error {
	return s.Store.Write(context.WithoutCancel(ctx), "data/private/proactive.json", &s.proactiveState)
}
func (s *Service) ProactiveStatus(ctx context.Context) (*model.ProactiveOverview, error) {
	s.proactiveMu.Lock()
	defer s.proactiveMu.Unlock()
	state := s.proactiveState
	state.Jobs = slices.Clone(state.Jobs)
	for i := range state.Jobs {
		state.Jobs[i].Parts = slices.Clone(state.Jobs[i].Parts)
		state.Jobs[i].RequestIDs = slices.Clone(state.Jobs[i].RequestIDs)
	}
	return &model.ProactiveOverview{Config: s.proactiveConfig, State: state}, ctx.Err()
}
func (s *Service) SaveProactiveConfig(ctx context.Context, input *model.ProactiveConfig) (*model.ProactiveOverview, error) {
	if input == nil || input.RandomMin < 0 || input.RandomMax < input.RandomMin || input.RandomMax > 5 || input.VideoDailyLimit < 1 || input.VideoDailyLimit > 10 {
		return nil, model.Fail("INPUT", "主动聊天每天0–5次，视频每天1–10个")
	}
	c := *input
	s.privateMu.Lock()
	binding := s.privateConfig
	s.privateMu.Unlock()
	if c.Enabled {
		if !binding.Enabled || !binding.AllowSend || binding.SenderUID == "" || binding.PeerUID == "" {
			return nil, model.Fail("PRIVATE_DISABLED", "请先启用已绑定主人的私信发送")
		}
		c.SenderUID = binding.SenderUID
		c.PeerUID = binding.PeerUID
	}
	s.proactiveMu.Lock()
	defer s.proactiveMu.Unlock()
	if err := s.Store.Write(ctx, "config/proactive.json", &c); err != nil {
		return nil, err
	}
	for i := range s.proactiveState.Jobs {
		job := &s.proactiveState.Jobs[i]
		if (job.Status == "pending" || job.Status == "ready") && (!proactiveKindEnabled(c, job.Kind) || job.SenderUID != c.SenderUID || job.PeerUID != c.PeerUID) {
			job.Status = "cancelled"
		}
	}
	s.proactiveConfig = c
	if err := s.saveProactive(ctx); err != nil {
		return nil, err
	}
	s.wakeRobot()
	return &model.ProactiveOverview{Config: c, State: s.proactiveState}, nil
}
func (s *Service) planProactive(ctx context.Context, now time.Time) error {
	day := diaryDay(now)
	if s.proactiveState.Day == day {
		return nil
	}
	c := s.proactiveConfig
	local := now.In(time.FixedZone("Asia/Shanghai", 8*3600))
	base := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	// 旧问候全部过期；资料通知保留。最多保留最近7天任务，不回放停机期间问候。
	kept := []model.ProactiveJob{}
	for _, j := range s.proactiveState.Jobs {
		due, _ := time.Parse(time.RFC3339Nano, j.DueAt)
		if due.Before(now.AddDate(0, 0, -7)) {
			continue
		}
		if j.Kind == "morning" || j.Kind == "night" || j.Kind == "random" {
			if j.Status == "pending" || j.Status == "ready" {
				j.Status = "expired"
			}
		}
		kept = append(kept, j)
	}
	s.proactiveState.Jobs = kept
	s.proactiveState.Day = day

	if c.Greetings {
		s.addProactiveSlot(now, base, c, "morning", 9*60+s.ExpressionDraw(60), 10*60)
		s.addProactiveSlot(now, base, c, "night", 21*60+s.ExpressionDraw(110), 23*60)
	}
	n := c.RandomMin
	if c.RandomMax > n {
		n += s.ExpressionDraw(c.RandomMax - n + 1)
	}
	if n > 0 {
		width := 11 * 60 / n
		for i := 0; i < n; i++ {
			minute := 10*60 + i*width + s.ExpressionDraw(max(1, width-30))
			s.addProactiveSlot(now, base, c, "random", minute, min(21*60, minute+60))
		}
	}
	return s.saveProactive(ctx)
}
func (s *Service) queueProactive(ctx context.Context, kind, key, text, parent string) error {
	s.proactiveMu.Lock()
	defer s.proactiveMu.Unlock()
	c := s.proactiveConfig
	if !c.Enabled || kind == "video" && !c.VideoReflections || kind == "diary" && !c.DiaryDelivery {
		return nil
	}
	for _, j := range s.proactiveState.Jobs {
		if j.Key == key {
			return nil
		}
	}
	if len(s.proactiveState.Jobs) >= 400 {
		return model.Fail("LIMIT", "主动任务队列已满")
	}
	now := s.ProactiveNow()
	due := now
	if proactiveQuiet(now) {
		due = proactiveNextMorning(now)
	}
	chars := []rune(text)
	parts := []string{}
	ids := []string{}
	for len(chars) > 0 {
		n := min(450, len(chars))
		parts = append(parts, string(chars[:n]))
		ids = append(ids, model.ID())
		chars = chars[n:]
	}
	if len(parts) == 0 || len(parts) > 3 || model.ContainsSensitiveText(text, s.generationSecrets(ctx)...) {
		return model.Fail("INPUT", "主动通知内容超限或含敏感信息")
	}
	j := model.ProactiveJob{OperationID: parent, ParentOperation: parent != "", Key: key, Kind: kind, DueAt: due.UTC().Format(time.RFC3339Nano), ExpiresAt: now.Add(36 * time.Hour).UTC().Format(time.RFC3339Nano), Status: "ready", SenderUID: c.SenderUID, PeerUID: c.PeerUID, Parts: parts, RequestIDs: ids}
	if parent != "" {
		if err := s.recordNotification(ctx, parent, "queued", nil); err != nil {
			return err
		}
	}
	s.proactiveState.Jobs = append(s.proactiveState.Jobs, j)
	s.wakeRobot()
	return s.saveProactive(ctx)
}

func (s *Service) addProactiveSlot(now, base time.Time, c model.ProactiveConfig, kind string, minute, expires int) {
	due := base.Add(time.Duration(minute) * time.Minute)
	if !due.After(now) {
		return
	}
	job := model.ProactiveJob{Key: fmt.Sprintf("%s:%s:%d", diaryDay(now), kind, minute), Kind: kind, DueAt: due.UTC().Format(time.RFC3339Nano), ExpiresAt: base.Add(time.Duration(expires) * time.Minute).UTC().Format(time.RFC3339Nano), Status: "pending", SenderUID: c.SenderUID, PeerUID: c.PeerUID}
	s.proactiveState.Jobs = append(s.proactiveState.Jobs, job)
}

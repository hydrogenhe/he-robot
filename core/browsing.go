package core

import (
	"context"
	"gpt-robot/model"
	"slices"
	"strings"
	"time"
)

func defaultBrowsing() model.BrowsingConfig {
	return model.BrowsingConfig{Mode: "persona", Keywords: []string{}, Excluded: []string{}, IntervalSeconds: 900, DailyLimit: 5, MaxDurationMinutes: 10}
}
func validateBrowsing(c *model.BrowsingConfig) error {
	if c == nil || c.MaxDurationMinutes < 1 || c.MaxDurationMinutes > 180 || (c.Mode != "persona" && c.Mode != "custom") || c.IntervalSeconds < 300 || c.IntervalSeconds > 86400 || c.DailyLimit < 1 || c.DailyLimit > 50 || len(c.Keywords) > 10 || len(c.Excluded) > 10 {
		return model.Fail("INPUT", "视频最长时长1–180分钟；请选择角色兴趣或自定义模式；间隔300–86400秒，每天1–50条，关键词各最多10个")
	}
	for _, word := range append(slices.Clone(c.Keywords), c.Excluded...) {
		if !model.Text(strings.TrimSpace(word), 20) || model.ContainsSensitiveText(word) || strings.ContainsAny(word, "\r\n") {
			return model.Fail("INPUT", "关键词应为1–20字，不能包含凭证")
		}
	}
	if c.Enabled && c.Mode == "custom" && len(c.Keywords) == 0 {
		return model.Fail("INPUT", "自定义模式请先填写兴趣关键词")
	}
	return nil
}
func (s *Service) initializeBrowsing(ctx context.Context) error {
	c := defaultBrowsing()
	if _, err := s.Store.Read(ctx, "config/browsing.json", &c); err != nil {
		return err
	}
	if err := validateBrowsing(&c); err != nil {
		return err
	}
	s.browsingConfig = c
	if _, err := s.Store.Read(ctx, "data/browsing/state.json", &s.browsingState); err != nil {
		return err
	}
	if s.browsingState.Records == nil {
		s.browsingState.Records = []model.BrowsedVideo{}
	}
	return nil
}
func (s *Service) browsingTopics(ctx context.Context, c *model.BrowsingConfig) []string {
	candidates := slices.Clone(c.Keywords)
	if c.Mode == "persona" {
		persona, err := s.Store.Text(ctx, "data/character/persona.md")
		if err != nil {
			return nil
		}
		candidates = diaryInterestTopics(persona, nil)
		s.developmentMu.Lock()
		for _, i := range s.development.Interests {
			if i.Platform == "bilibili" && i.Account == c.AccountUID && i.Visibility == "public" && (i.Preference > 0 || (i.Preference == 0 && i.Familiarity >= 3)) {
				candidates = append(candidates, i.Topic)
			}
		}
		s.developmentMu.Unlock()
		if len(candidates) == 0 {
			candidates = []string{"日常科普", "自然生活"}
		}
	}
	topics := []string{}
	secrets := s.generationSecrets(ctx)
	for _, word := range candidates {
		word = strings.TrimSpace(word)
		if !model.Text(word, 20) || model.ContainsSensitiveText(word, secrets...) || slices.Contains(topics, word) {
			continue
		}
		excluded := false
		for _, x := range c.Excluded {
			if strings.Contains(word, x) {
				excluded = true
			}
		}
		if !excluded {
			topics = append(topics, word)
		}
		if len(topics) == 10 {
			break
		}
	}
	return topics
}
func (s *Service) BrowsingOverview(ctx context.Context) (*model.BrowsingOverview, error) {
	s.browsingMu.Lock()
	config := s.browsingConfig
	config.Keywords = slices.Clone(config.Keywords)
	config.Excluded = slices.Clone(config.Excluded)
	state := s.browsingState
	state.Records = slices.Clone(state.Records)
	s.browsingMu.Unlock()
	if state.Day != diaryDay(time.Now()) {
		state.Attempts = 0
	}
	return &model.BrowsingOverview{Config: config, State: state, EffectiveKeywords: s.browsingTopics(ctx, &config)}, ctx.Err()
}
func (s *Service) SaveBrowsing(ctx context.Context, input *model.BrowsingConfig) (*model.BrowsingOverview, error) {
	if err := validateBrowsing(input); err != nil {
		return nil, err
	}
	for _, word := range append(slices.Clone(input.Keywords), input.Excluded...) {
		if model.ContainsSensitiveText(word, s.generationSecrets(ctx)...) {
			return nil, model.Fail("INPUT", "关键词不能包含凭证")
		}
	}
	config := *input
	config.Keywords = slices.Compact(slices.Clone(input.Keywords))
	config.Excluded = slices.Compact(slices.Clone(input.Excluded))
	if config.Enabled {
		account, err := s.Bili.Status(ctx)
		if err != nil {
			return nil, err
		}
		config.AccountUID = account.UID
		if len(s.browsingTopics(ctx, &config)) == 0 {
			return nil, model.Fail("BROWSING_TOPICS", "没有找到明确兴趣，请补充角色人设或改用自定义关键词")
		}
	}
	s.browsingMu.Lock()
	state := s.browsingState
	state.Revision++
	state.Paused = false
	state.Reason = ""
	state.NextAt = ""
	// 配置与版本先落盘；不改变任何回复开关、账号权限或已有暂停。
	err := s.Store.Write(ctx, "data/browsing/state.json", &state)
	if err == nil {
		err = s.Store.Write(ctx, "config/browsing.json", &config)
	}
	if err == nil {
		s.browsingConfig = config
		s.browsingState = state
	}
	s.browsingMu.Unlock()
	if err != nil {
		return nil, err
	}
	s.wakeRobot()
	return s.BrowsingOverview(ctx)
}
func (s *Service) browsingTargets(account string) []string {
	s.browsingMu.Lock()
	defer s.browsingMu.Unlock()
	targets := []string{}
	if !s.browsingConfig.Enabled || s.browsingState.Paused || account != s.browsingConfig.AccountUID {
		return targets
	}
	for i := len(s.browsingState.Records) - 1; i >= 0 && len(targets) < 20; i-- {
		r := s.browsingState.Records[i]
		at, err := time.Parse(time.RFC3339Nano, r.At)
		if err == nil && time.Since(at) < 24*time.Hour && r.AccountUID == account && r.Revision == s.browsingState.Revision && !slices.Contains(targets, r.Video.BVID) {
			targets = append(targets, r.Video.BVID)
		}
	}
	return targets
}
func (s *Service) autoBrowsingTarget(d *model.Draft) bool {
	s.autoMu.Lock()
	config := s.autoConfig
	paused := s.autoState.Paused
	s.autoMu.Unlock()
	return config.Enabled && !paused && s.robotRunning() && config.Source == "browsing" && d.Kind != "dynamic" && d.AutoCommentID != "" && d.Account == config.AccountUID && slices.Contains(s.browsingTargets(d.Account), d.BVID)
}
func (s *Service) BrowsingTick(ctx context.Context) error { return s.browsingTick(ctx, false) }
func (s *Service) BrowseNow(ctx context.Context) error    { return s.browsingTick(ctx, true) }
func (s *Service) browsingTick(ctx context.Context, manual bool) error {
	if !s.autoRunMu.TryLock() {
		return model.Fail("BUSY", "机器人正在处理其他互动")
	}
	defer s.autoRunMu.Unlock()
	run, cancel := s.beginAutoRun(ctx)
	defer s.endAutoRun(cancel)
	ctx = run
	ctx, timeout := context.WithTimeout(ctx, 60*time.Second)
	defer timeout()
	if !s.robotRunning() {
		return nil
	}
	s.browsingMu.Lock()
	c := s.browsingConfig
	state := s.browsingState
	state.Records = slices.Clone(state.Records)
	if !c.Enabled || state.Paused || (!manual && untilRun(state.NextAt) > 0) {
		s.browsingMu.Unlock()
		return nil
	}
	day := diaryDay(time.Now())
	if state.Day != day {
		state.Day = day
		state.Attempts = 0
	}
	if state.Attempts >= c.DailyLimit {
		state.NextAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
		s.browsingState = state
		err := s.Store.Write(ctx, "data/browsing/state.json", &state)
		s.browsingMu.Unlock()
		return err
	}
	topics := s.browsingTopics(ctx, &c)
	if len(topics) == 0 {
		s.browsingMu.Unlock()
		return model.Fail("BROWSING_TOPICS", "当前没有明确兴趣")
	}
	keyword := topics[state.KeywordIndex%len(topics)]
	state.KeywordIndex++
	state.Attempts++
	state.NextAt = time.Now().Add(time.Duration(c.IntervalSeconds) * time.Second).UTC().Format(time.RFC3339Nano)
	if err := s.Store.Write(ctx, "data/browsing/state.json", &state); err != nil {
		s.browsingMu.Unlock()
		return err
	}
	s.browsingState = state
	s.browsingMu.Unlock()
	operation := model.OperationStart{Platform: "bilibili", Action: "browsing-read", Source: "automation"}
	id, startErr := s.StartOperation(ctx, &operation)
	if startErr != nil {
		return startErr
	}
	usage := model.UsageReport{OperationID: id, Usage: model.TokenUsage{Provider: "local", Model: "none", RequestID: id, Input: 0, Output: 0}}
	if err := s.reportUsage(ctx, &usage, "local_rule"); err != nil {
		return err
	}
	previousEvent := ""
	if len(state.Records) > 0 {
		previousEvent = state.Records[len(state.Records)-1].EventID
	}
	err := s.readInterestVideo(ctx, &c, &state, keyword)
	finish := model.OperationFinish{Status: "skipped", Account: c.AccountUID}
	if len(state.Records) > 0 && state.Records[len(state.Records)-1].EventID != previousEvent {
		finish.Status = "succeeded"
		finish.Reference = state.Records[len(state.Records)-1].EventID
	}
	if err != nil {
		finish.Status = "failed"
		finish.ErrorCode = OperationErrorCode(err)
	}
	if finishErr := s.FinishOperation(context.WithoutCancel(ctx), id, &finish); finishErr != nil {
		return finishErr
	}
	s.browsingMu.Lock()
	defer s.browsingMu.Unlock()
	if s.browsingState.Revision != state.Revision {
		return nil
	}
	if err != nil {
		state.Paused = true
		state.Reason = "浏览读取失败（" + OperationErrorCode(err) + "），请检查登录或平台限制后重新保存"
	}
	s.browsingState = state
	if saveErr := s.Store.Write(context.WithoutCancel(ctx), "data/browsing/state.json", &state); saveErr != nil {
		return saveErr
	}
	return err
}
func (s *Service) readInterestVideo(ctx context.Context, c *model.BrowsingConfig, state *model.BrowsingState, keyword string) error {
	discovery, ok := s.Bili.(model.VideoDiscovery)
	if !ok {
		return model.Fail("UNAVAILABLE", "当前平台没有视频发现适配器")
	}
	account, err := s.Bili.Status(ctx)
	if err != nil {
		return err
	}
	if account.UID != c.AccountUID {
		return model.Fail("ACCOUNT", "浏览绑定账号已变化")
	}
	rows, err := discovery.SearchVideos(ctx, keyword)
	if err != nil {
		return err
	}
	for _, candidate := range rows {
		if !model.ValidBV(candidate.BVID) || candidate.DurationSeconds <= 0 || candidate.DurationSeconds > c.MaxDurationMinutes*60 {
			continue
		}
		seen := false
		for _, old := range state.Records {
			if old.Video.BVID == candidate.BVID && old.AccountUID == c.AccountUID {
				seen = true
				break
			}
		}
		if seen {
			continue
		}
		excluded := false
		for _, x := range c.Excluded {
			if strings.Contains(strings.ToLower(candidate.Title+candidate.Description), strings.ToLower(x)) {
				excluded = true
			}
		}
		if excluded {
			continue
		}
		video, err := s.Bili.Video(ctx, candidate.BVID)
		if err != nil {
			return err
		}
		if video.BVID != candidate.BVID || !model.ValidNumber(video.Aid) {
			return model.Fail("TARGET", "视频资料不匹配")
		}
		if video.DurationSeconds <= 0 || video.DurationSeconds > c.MaxDurationMinutes*60 {
			continue
		}
		for _, x := range c.Excluded {
			if strings.Contains(strings.ToLower(video.Title+video.Description), strings.ToLower(x)) {
				excluded = true
			}
		}
		if excluded {
			continue
		}
		page, err := s.Bili.Comments(ctx, video.Aid, 1, "0")
		if err != nil {
			return err
		}
		s.browsingMu.Lock()
		valid := s.browsingConfig.Enabled && s.browsingState.Revision == state.Revision
		s.browsingMu.Unlock()
		if !valid || !s.robotRunning() || ctx.Err() != nil {
			return model.Fail("CANCELLED", "浏览授权已改变")
		}
		secrets := s.generationSecrets(ctx)
		video.Title = model.RedactSensitiveText(video.Title, secrets...)
		video.Description = model.RedactSensitiveText(model.Clip(video.Description, 500), secrets...)
		preview := ""
		for _, comment := range page.Comments[:min(3, len(page.Comments))] {
			preview += model.Clip(model.RedactSensitiveText(comment.Text, secrets...), 80) + "；"
		}
		event, err := s.Store.Event(ctx, "video-browsed", "读取视频资料《"+video.Title+"》", map[string]any{"bvid": video.BVID, "keyword": keyword, "description": video.Description, "commentCount": len(page.Comments), "readScope": "标题、简介和评论；未读取画面/音轨/字幕"})
		if err != nil {
			return err
		}
		record := model.BrowsedVideo{Video: video, Keyword: keyword, At: event.At, AccountUID: c.AccountUID, Revision: state.Revision, EventID: event.ID, CommentCount: len(page.Comments), Preview: preview}
		state.Records = append(state.Records, record)
		if len(state.Records) > 100 {
			state.Records = state.Records[len(state.Records)-100:]
		}
		experience := model.DevelopmentInput{EvidenceKind: 4, MaterialKey: video.BVID, Platform: "bilibili", Account: c.AccountUID, Visibility: "public", Source: event.ID, At: event.At, Text: model.Clip(video.Title+" "+video.Description, 500)}
		return s.RecordDevelopment(ctx, &experience)
	}
	return nil
}

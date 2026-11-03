package core

import (
	"context"
	"gpt-robot/model"
	"log/slog"
	"time"
)

func diaryDay(now time.Time) string {
	return now.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
}
func diaryFile(day string) string { return "data/private/diary/" + day + ".json" }
func (s *Service) initializeDiary(ctx context.Context) error {
	config := model.DiaryConfig{Topics: []string{}, OwnerWeight: 4, InterestWeight: 2}
	if _, err := s.Store.Read(ctx, "config/diary.json", &config); err != nil {
		return err
	}
	if err := validateDiaryConfig(&config); err != nil {
		return err
	}
	s.diaryConfig = config
	entries, err := s.Store.Diaries(ctx, 1)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Status == "generating" {
			entry.Status, entry.ErrorCode = "interrupted", "INTERRUPTED"
			if err := s.Store.Write(ctx, diaryFile(entry.Date), &entry); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Service) DiaryOverview(ctx context.Context) (*model.DiaryOverview, error) {
	s.diaryMu.Lock()
	enabled := s.diaryConfig.Enabled
	testing := s.diaryConfig.Testing
	config := s.diaryConfig
	s.diaryMu.Unlock()
	entries, err := s.Store.Diaries(ctx, 30)
	if err != nil {
		return nil, err
	}
	today := diaryDay(s.DiaryNow())
	result := &model.DiaryOverview{Topics: append([]string{}, config.Topics...), OwnerWeight: config.OwnerWeight, InterestWeight: config.InterestWeight, Testing: testing, Enabled: enabled, Today: today, Entries: entries}
	for _, entry := range entries {
		if entry.Date == today {
			result.TodayStatus = entry.Status
		}
	}
	return result, nil
}
func (s *Service) SaveDiaryConfig(ctx context.Context, input *model.DiarySettings) (*model.DiaryOverview, error) {
	if input == nil {
		return nil, model.Fail("INPUT", "缺少日记设置")
	}
	s.diaryMu.Lock()
	config := s.diaryConfig
	config.Enabled = input.Enabled
	if input.Testing != nil {
		config.Testing = *input.Testing
	}
	if input.Topics != nil {
		config.Topics = append([]string{}, (*input.Topics)...)
	}
	if input.OwnerWeight != nil {
		config.OwnerWeight = *input.OwnerWeight
	}
	if input.InterestWeight != nil {
		config.InterestWeight = *input.InterestWeight
	}
	if err := validateDiaryConfig(&config); err != nil {
		s.diaryMu.Unlock()
		return nil, err
	}
	if err := s.Store.Write(ctx, "config/diary.json", &config); err != nil {
		s.diaryMu.Unlock()
		return nil, err
	}
	s.diaryConfig = config
	s.diaryMu.Unlock()
	s.wakeRobot()
	return s.DiaryOverview(ctx)
}

// GenerateDiary 只写当天角色回顾，先保存生成占位防止并发、重启或自动任务重复调用模型。
func (s *Service) GenerateDiary(ctx context.Context, input *model.DiaryGenerateInput) (*model.DiaryEntry, error) {
	if !s.autoRunMu.TryLock() {
		return nil, model.Fail("BUSY", "机器人正在处理互动，请稍后写日记")
	}
	defer s.autoRunMu.Unlock()
	runCtx, cancel := s.beginAutoRun(ctx)
	defer s.endAutoRun(cancel)
	return s.generateDiary(runCtx, input)
}

// generateDiary 复用日记业务，调用方已持有互动串行锁及可取消运行上下文。
func (s *Service) generateDiary(ctx context.Context, input *model.DiaryGenerateInput) (*model.DiaryEntry, error) {
	if input == nil {
		return nil, model.Fail("INPUT", "缺少日记生成参数")
	}
	s.diaryMu.Lock()
	testing := s.diaryConfig.Testing
	s.diaryMu.Unlock()
	if s.DiaryGenerator == nil {
		return nil, model.Fail("UNAVAILABLE", "日记AI宿主未配置")
	}
	if input.Automatic {
		s.diaryMu.Lock()
		enabled := s.diaryConfig.Enabled
		s.diaryMu.Unlock()
		if !enabled {
			return nil, model.Fail("DIARY_DISABLED", "自动日记未开启")
		}
	}
	if !s.robotRunning() {
		return nil, model.Fail("ROBOT_PAUSED", "请先启动或恢复机器人")
	}
	day := diaryDay(s.DiaryNow())
	entry := &model.DiaryEntry{}
	found, err := s.Store.Read(ctx, diaryFile(day), entry)
	if err != nil {
		return nil, err
	}
	if found && entry.Date != day {
		return nil, model.Fail("DATA", "日记日期不一致，未覆盖原记录")
	}
	if found && entry.Status == "completed" && (!testing || input.Automatic) {
		return entry, nil
	}
	if found && entry.Status != "empty" && entry.Status != "completed" && (input.Automatic || !input.Retry) {
		return nil, model.Fail("DIARY_ATTEMPTED", "今天已尝试生成，请在日记页明确重试，不自动重复消耗")
	}
	prompt, err := s.diaryPrompt(ctx, day)
	if err != nil {
		return nil, err
	}
	previous := *entry
	if testing && len(prompt.Facts) == 0 {
		fact := model.DiaryFact{Source: "empty-day:" + day, Kind: "empty_day", Summary: "测试模式：今天暂无已记录的新经历。只可按人设写安静一天的主观感想，不得编造互动、视频、行动或收获。"}
		prompt.Facts = append(prompt.Facts, fact)
	}
	entry = &model.DiaryEntry{Testing: testing, Date: day, At: time.Now().UTC().Format(time.RFC3339Nano), Status: "empty", Sources: []string{}}
	if len(prompt.Facts) == 0 {
		if err := s.Store.Write(ctx, diaryFile(day), entry); err != nil {
			return nil, err
		}
		return entry, nil
	}
	source := "web"
	if input.DirectReply {
		source = "local"
	}
	if input.Automatic {
		source = "automation"
	}
	operation := model.OperationStart{Platform: "local", Action: "diary-generate", Source: source}
	id, err := s.StartOperation(ctx, &operation)
	if err != nil {
		return nil, err
	}
	if previous.Status == "completed" && previous.Body != "" {
		revision := previous.OperationID
		if !model.ValidID(revision) {
			revision = model.ID()
		}
		if err := s.Store.Write(ctx, "data/private/diary/revisions/"+day+"/"+revision+".json", &previous); err != nil {
			return nil, err
		}
	}
	entry.OperationID, entry.Status = id, "generating"
	for _, fact := range prompt.Facts {
		if fact.Kind != "empty_day" {
			entry.Sources = append(entry.Sources, fact.Source)
		}
	}
	if err := s.Store.Write(ctx, diaryFile(day), entry); err != nil {
		return nil, err
	}
	secrets := s.generationSecrets(ctx)
	prompt.Persona = model.RedactSensitiveText(prompt.Persona, secrets...)
	prompt.State = model.RedactSensitiveText(prompt.State, secrets...)
	for i := range prompt.Facts {
		prompt.Facts[i].Summary = model.RedactSensitiveText(prompt.Facts[i].Summary, secrets...)
	}
	dayBudget, budgetErr := s.reserveGeneration(ctx, true)
	var result *model.GeneratedDiary
	runErr := budgetErr
	if runErr == nil {
		result, runErr = s.DiaryGenerator.GenerateDiary(ctx, prompt)
		var actual *model.TokenUsage
		if result != nil {
			actual = result.Usage
		}
		if err := s.settleGeneration(context.WithoutCancel(ctx), dayBudget, true, actual); err != nil {
			runErr = err
		}
	}
	if runErr == nil && result != nil && (model.ContainsSensitiveText(result.Title, secrets...) || model.ContainsSensitiveText(result.Body, secrets...)) {
		runErr = model.Fail("SENSITIVE_OUTPUT", "日记含疑似凭证，未保存正文")
	}
	persist := context.WithoutCancel(ctx)
	if result != nil && result.Usage != nil {
		usage := model.UsageReport{OperationID: id, Usage: *result.Usage}
		if err := s.reportUsage(persist, &usage, generatedUsageSource(result.Usage)); err != nil {
			runErr = err
		}
	}
	if runErr == nil && (result == nil || !model.Text(result.Title, 40) || !model.Text(result.Body, 800)) {
		runErr = model.Fail("AI_DIARY", "模型没有返回合格日记")
	}
	if runErr == nil && (!s.robotRunning() || ctx.Err() != nil) {
		runErr = model.Fail("ROBOT_PAUSED", "生成期间机器人已暂停，日记未发布")
	}
	finish := model.OperationFinish{Status: "succeeded", Reference: day}
	if runErr != nil {
		entry.Status, entry.ErrorCode = "failed", OperationErrorCode(runErr)
		finish.Status, finish.ErrorCode = "failed", entry.ErrorCode
	} else {
		entry.Status, entry.Title, entry.Body = "completed", result.Title, result.Body
	}
	// 测试重写失败时保留上一版成功日记，失败用量仍记到本次操作。
	if runErr != nil && previous.Status == "completed" {
		entry = &previous
	}
	if err := s.Store.Write(persist, diaryFile(day), entry); err != nil {
		finish.Status = "failed"
		finish.ErrorCode = OperationErrorCode(err)
		if finishErr := s.FinishOperation(persist, id, &finish); finishErr != nil {
			return nil, finishErr
		}
		return nil, err
	}
	if err := s.FinishOperation(persist, id, &finish); err != nil {
		return entry, err
	}
	if runErr == nil && entry.Status == "completed" && !input.DirectReply {
		if notifyErr := s.queueProactive(ctx, "diary", "diary:"+entry.OperationID, "《"+entry.Title+"》\n"+entry.Body, entry.OperationID); notifyErr != nil {
			return entry, notifyErr
		}
	}
	return entry, runErr
}

// diaryWait 仅到北京时间23点才读当天占位，无事则记empty，不按私信频率重复收集素材。
func (s *Service) diaryWait(ctx context.Context, now time.Time) time.Duration {
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	local := now.In(zone)
	next := time.Date(local.Year(), local.Month(), local.Day(), 23, 0, 0, 0, zone)
	s.diaryMu.Lock()
	enabled := s.diaryConfig.Enabled
	checked := s.diaryChecked
	s.diaryMu.Unlock()
	if !enabled {
		return time.Hour
	}
	day := diaryDay(now)
	if local.Before(next) {
		return next.Sub(local)
	}
	if checked == day {
		return next.AddDate(0, 0, 1).Sub(local)
	}
	entry := model.DiaryEntry{}
	found, err := s.Store.Read(ctx, diaryFile(day), &entry)
	if err == nil && (!found || entry.Status == "empty") {
		input := model.DiaryGenerateInput{Automatic: true}
		_, err = s.GenerateDiary(ctx, &input)
	}
	if err != nil && OperationErrorCode(err) == "BUSY" {
		return 30 * time.Second
	}
	if err != nil {
		slog.Warn("角色日记生成停止", "code", OperationErrorCode(err))
	}
	s.diaryMu.Lock()
	s.diaryChecked = day
	s.diaryMu.Unlock()
	return next.AddDate(0, 0, 1).Sub(local)
}

package core

import (
	"context"
	"gpt-robot/model"
	"log/slog"
	"time"
)

func (s *Service) initializeRobot(ctx context.Context) error {
	state := model.RobotState{Enabled: true}
	if _, err := s.Store.Read(ctx, "config/robot-runtime.json", &state); err != nil {
		return err
	}
	if state.Status == "" {
		if state.Enabled {
			state.Status = "running"
		} else {
			state.Status = "paused"
		}
	}
	if state.Status != "running" && state.Status != "paused" && state.Status != "stopped" {
		return model.Fail("STATE", "机器人运行状态无效")
	}
	state.Enabled = state.Status == "running"
	s.robot = state
	config := model.PrivateConfig{SenderUID: s.autoConfig.AccountUID, PeerUID: s.autoConfig.PriorityUID, IntervalSeconds: 30}
	if _, err := s.Store.Read(ctx, "config/private-chat.json", &config); err != nil {
		return err
	}
	if err := validatePrivateConfig(&config); err != nil {
		return err
	}
	if _, err := s.Store.Read(ctx, "data/private/state.json", &s.privateState); err != nil {
		return err
	}
	if s.privateState.Inbox == nil {
		s.privateState.Inbox = []model.PrivateMessage{}
	}
	if s.privateState.Outbox == nil {
		s.privateState.Outbox = []model.PrivateOutbox{}
	}
	if s.privateState.Processing != "" {
		s.privateState.Paused = true
		s.privateState.Reason = "上次私信处理被中断，已暂停，不自动重试"
		s.privateState.Processing = ""
	}
	for i := range s.privateState.Outbox {
		if s.privateState.Outbox[i].Status == "sending" {
			s.privateState.Outbox[i].Status = "unknown"
			s.privateState.Paused = true
			s.privateState.Reason = "上次私信发送结果未知，请人工回查"
		}
	}
	s.privateConfig = config
	return s.Store.Write(ctx, "data/private/state.json", &s.privateState)
}
func (s *Service) robotRunning() bool {
	s.robotMu.Lock()
	defer s.robotMu.Unlock()
	return s.robot.Enabled
}
func (s *Service) RobotStatus(ctx context.Context) (*model.RobotState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.robotMu.Lock()
	defer s.robotMu.Unlock()
	state := s.robot
	return &state, nil
}

// SetRobot 总开关只控制自动任务，网页及手动查询继续可用。
func (s *Service) SetRobot(ctx context.Context, enabled bool) (*model.RobotState, error) {
	action := "pause"
	if enabled {
		action = "start"
	}
	input := model.RobotControlInput{Action: action}
	return s.ControlRobot(ctx, &input)
}

// ControlRobot 区分关闭和暂停：关闭后启动重建基线，暂停后恢复继续已有进度。
func (s *Service) ControlRobot(ctx context.Context, input *model.RobotControlInput) (*model.RobotState, error) {
	if input == nil {
		return nil, model.Fail("INPUT", "缺少机器人控制动作")
	}
	switch input.Action {
	case "start", "stop", "pause", "resume":
	default:
		return nil, model.Fail("INPUT", "控制动作必须为start/stop/pause/resume")
	}
	s.robotControlMu.Lock()
	defer s.robotControlMu.Unlock()
	old, err := s.RobotStatus(ctx)
	if err != nil {
		return nil, err
	}
	state := *old
	switch input.Action {
	case "start":
		if old.Status == "stopped" {
			if err := s.resetRobotBaselines(ctx); err != nil {
				return nil, err
			}
		}
		state.Status = "running"
	case "stop":
		state.Status = "stopped"
	case "pause":
		if old.Status != "stopped" {
			state.Status = "paused"
		}
	case "resume":
		if old.Status == "stopped" {
			return nil, model.Fail("STATE", "机器人已关闭，请先启动")
		}
		state.Status = "running"
	}
	state.Enabled = state.Status == "running"
	s.robotMu.Lock()
	if err := s.Store.Write(ctx, "config/robot-runtime.json", &state); err != nil {
		s.robotMu.Unlock()
		return nil, err
	}
	s.robot = state
	if !state.Enabled && s.videoCancel != nil {
		s.videoCancel()
	}
	if !state.Enabled && s.robotCancel != nil {
		s.robotCancel()
	}
	s.robotMu.Unlock()
	if !state.Enabled {
		s.modelRouter.CloseConversations()
	}
	s.wakeRobot()
	return &state, nil
}
func (s *Service) resetRobotBaselines(ctx context.Context) error {
	s.autoMu.Lock()
	s.autoState.Videos = map[string]model.AutoVideoState{}
	s.autoState.Revision++
	s.autoState.NextAt = ""
	for key, entry := range s.autoState.Entries {
		if entry.Status == model.AutoQueued {
			entry.Status = model.AutoSkipped
			entry.Reason = "机器人关闭后重新启动，不重放旧队列"
			s.autoState.Entries[key] = entry
		}
	}
	err := s.Store.Write(ctx, "data/automation/state.json", &s.autoState)
	s.autoMu.Unlock()
	if err != nil {
		return err
	}
	s.privateMu.Lock()
	defer s.privateMu.Unlock()
	s.privateState.Initialized = false
	s.privateState.Cursor = 0
	s.privateState.Since = time.Now().Unix()
	s.privateState.NextAt = ""
	s.privateState.Revision++
	if err := s.savePrivate(ctx); err != nil {
		return err
	}
	if s.privateConfig.MemoryEnabled {
		config := s.privateConfig
		config.MemoryGeneration = model.ID()
		if err := s.Store.Write(ctx, "config/private-chat.json", &config); err != nil {
			return err
		}
		s.privateConfig = config
	}
	return nil
}
func (s *Service) wakeRobot() {
	s.wakePrivatePush()
	select {
	case s.autoWake <- struct{}{}:
	default:
	}
}

// RunAutomation 单个后台调度器按各渠道到期时间检查；无新输入不调用模型。
func (s *Service) RunAutomation(ctx context.Context) {
	defer s.waitBrowsingWorker()
	defer s.modelRouter.CloseConversations()
	pushCtx, cancelPush := context.WithCancel(ctx)
	pushDone := make(chan struct{})
	go s.privatePushWorker(pushCtx, pushDone)
	defer stopPrivatePushWorker(cancelPush, pushDone)
	for {
		if ctx.Err() != nil {
			return
		}
		s.robotMu.Lock()
		enabled := s.robot.Enabled
		runCtx := ctx
		s.robotMu.Unlock()
		wait := time.Hour
		if enabled {
			if err := s.flushGrowth(ctx); err != nil {
				slog.Warn("成长封段保存失败", "error", err)
			}
			wait = min(wait, 5*time.Minute)
			s.privateMu.Lock()
			privateEnabled, privateNext, privateInterval := s.privateConfig.Enabled && !s.privateState.ReadPaused && ((s.privateConfig.AutoReply && !s.privateState.Paused) || s.privateConfig.MemoryEnabled), s.privateState.NextAt, s.privateConfig.IntervalSeconds
			s.privateMu.Unlock()
			if privateEnabled && runCtx.Err() == nil {
				due := untilRun(privateNext)
				s.privateMu.Lock()
				pending := s.privatePushPending && s.privateState.ReadFailures == 0
				pushDue := time.Until(s.privatePushNextCheck)
				s.privateMu.Unlock()
				if pending {
					due = min(due, max(0, pushDue))
				}
				if due <= 0 {
					s.privateMu.Lock()
					source := "poll"
					if s.privateDeferredSource != "" {
						source = s.privateDeferredSource
					}
					if pending && pushDue <= 0 && s.privatePushPendingSource != "" {
						source = s.privatePushPendingSource
					}
					previousRead := s.privateState.LastAt
					s.privateCheckSource = source
					s.privatePushPendingSource = ""
					s.privatePushPending = false
					delay := 2 * time.Second
					if source == "push-hint" {
						delay = 5 * time.Second
					}
					s.privatePushNextCheck = time.Now().Add(delay)
					s.privateMu.Unlock()
					if err := s.PrivateTick(runCtx); err != nil && runCtx.Err() == nil {
						if OperationErrorCode(err) == "BUSY" {
							s.privateMu.Lock()
							s.privateState.NextAt = time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano)
							s.privateMu.Unlock()
						} else {
							slog.Warn("私信自动任务停止", "code", OperationErrorCode(err))
						}
					}
					s.wakePrivatePush()
					s.privateMu.Lock()
					if s.privateState.LastAt == previousRead && source != "poll" && !s.privateState.ReadPaused {
						s.privateDeferredSource = source
					} else {
						s.privateDeferredSource = ""
					}
					next := s.privateState.NextAt
					s.privateMu.Unlock()
					due = untilRun(next)
					if due <= 0 {
						due = time.Duration(privateInterval) * time.Second
					}
				}
				wait = min(wait, due)
			}
			s.browsingMu.Lock()
			browseEnabled, browseNext, browseInterval := s.browsingConfig.Enabled && !s.browsingState.Paused, s.browsingState.NextAt, s.browsingConfig.IntervalSeconds
			s.browsingMu.Unlock()
			if browseEnabled {
				due := untilRun(browseNext)
				if due <= 0 {
					s.startBrowsingWorker(runCtx)
					due = time.Duration(browseInterval) * time.Second
				}
				wait = min(wait, due)
			}
			s.autoMu.Lock()
			autoEnabled, autoNext, autoInterval := s.autoConfig.Enabled && !s.autoState.Paused, s.autoState.NextAt, s.autoConfig.IntervalSeconds
			s.autoMu.Unlock()
			if autoEnabled {
				due := untilRun(autoNext)
				if due <= 0 {
					if err := s.AutoTick(runCtx); err != nil && runCtx.Err() == nil {
						slog.Warn("评论自动任务停止", "code", OperationErrorCode(err))
					}
					due = time.Duration(autoInterval) * time.Second
				}
				wait = min(wait, due)
			}
			wait = min(wait, s.diaryWait(runCtx, s.DiaryNow()))
			if err := s.ProactiveTick(runCtx); err != nil && runCtx.Err() == nil {
				slog.Warn("主动任务停止", "code", OperationErrorCode(err))
			}
			wait = min(wait, s.proactiveWait())
		}
		if wait < time.Second {
			wait = time.Second
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.autoWake:
			timer.Stop()
		case <-timer.C:
		}
	}
}
func untilRun(value string) time.Duration {
	at, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0
	}
	return time.Until(at)
}

func (s *Service) beginAutoRun(ctx context.Context) (context.Context, context.CancelFunc) {
	s.robotMu.Lock()
	defer s.robotMu.Unlock()
	runCtx, cancel := context.WithCancel(ctx)
	s.robotCancel = cancel
	if !s.robot.Enabled {
		cancel()
	}
	return runCtx, cancel
}
func (s *Service) endAutoRun(cancel context.CancelFunc) {
	cancel()
	s.robotMu.Lock()
	s.robotCancel = nil
	s.robotMu.Unlock()
}

func (s *Service) privatePushWorker(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	s.runPrivatePush(ctx)
}
func stopPrivatePushWorker(cancel context.CancelFunc, done <-chan struct{}) { cancel(); <-done }

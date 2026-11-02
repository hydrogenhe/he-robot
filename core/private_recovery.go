package core

import (
	"context"
	"errors"
	"fmt"
	"gpt-robot/model"
	"log/slog"
	"time"
)

// privateProcessingError 区分生成、发送前核验和实际发送，不携带私聊正文。
type privateProcessingError struct {
	err   error
	stage string
}

func (e *privateProcessingError) Error() string { return e.err.Error() }
func (e *privateProcessingError) Unwrap() error { return e.err }

// verifyPrivateSendAccount 仅重试发送前的只读网络核验，最多三次，等待1/2秒。
// 已生成回复保留在当前调用内，不重新调用模型；不重试发送、登录或风控错误。
func (s *Service) verifyPrivateSendAccount(ctx context.Context, config *model.PrivateConfig, automated bool, timings *model.OperationTimings) error {
	for attempt := 0; attempt < 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.privateMu.Lock()
		allowed := s.privateConfig == *config && !s.privateState.Paused
		s.privateMu.Unlock()
		if !allowed || (automated && !s.robotRunning()) {
			return model.Fail("PRIVATE_DISABLED", "私信核验期间权限已变化，未发送")
		}
		if timings != nil {
			timings.AccountAttempts++
		}
		account, err := s.Bili.Status(ctx)
		if err == nil {
			if account.UID != config.SenderUID {
				return model.Fail("ACCOUNT", "私信账号核验失败")
			}
			return nil
		}
		if OperationErrorCode(err) != "NETWORK" || attempt == 2 || ctx.Err() != nil {
			return err
		}
		slog.Warn("私信发送前只读账号核验退避", "attempt", attempt+1, "code", "NETWORK")
		timer := time.NewTimer(time.Duration(attempt+1) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return model.Fail("NETWORK", "私信发送前账号核验未完成")
}

// privateReadFailure 只读网络错误有界退避；绝不重试模型生成或发送。
func (s *Service) privateReadFailure(ctx context.Context, err error) error {
	code := OperationErrorCode(err)
	s.privateMu.Lock()
	defer s.privateMu.Unlock()
	state := &s.privateState
	state.LastErrorCode, state.LastErrorStage, state.LastErrorAt = code, "read", time.Now().UTC().Format(time.RFC3339Nano)
	if code == "NETWORK" {
		state.ReadFailures++
		if state.ReadFailures < 5 {
			delay := time.Duration(s.privateConfig.IntervalSeconds) * time.Second
			for i := 1; i < state.ReadFailures; i++ {
				delay *= 2
			}
			ceiling := max(time.Duration(s.privateConfig.IntervalSeconds)*time.Second, 15*time.Minute)
			delay = min(delay, ceiling)
			state.NextAt = time.Now().Add(delay).UTC().Format(time.RFC3339Nano)
			if !state.Paused {
				state.Reason = fmt.Sprintf("私信只读网络错误，等待退避重试（%d/5）；未调用AI或发送", state.ReadFailures)
			}
			return s.savePrivate(ctx)
		}
	}
	state.Paused, state.ReadPaused = true, true
	state.NextAt = ""
	state.Reason = "私信读取已暂停（" + code + "）；请核验后恢复，仅处理新消息"
	if saveErr := s.savePrivate(ctx); saveErr != nil {
		return saveErr
	}
	slog.Warn("私信读取安全暂停", "code", code, "failures", state.ReadFailures)
	return nil
}
func (s *Service) privateFailure(ctx context.Context, err error, stage string) error {
	var failure *privateProcessingError
	if errors.As(err, &failure) {
		stage = failure.stage
	}
	s.privateMu.Lock()
	defer s.privateMu.Unlock()
	s.privateState.Paused = true
	s.privateState.Reason = "私信处理失败（" + OperationErrorCode(err) + "），已暂停，不自动重试生成或发送"
	s.privateState.LastErrorCode = OperationErrorCode(err)
	s.privateState.LastErrorStage = stage
	s.privateState.LastErrorAt = time.Now().UTC().Format(time.RFC3339Nano)
	slog.Warn("私信处理安全暂停", "stage", stage, "code", s.privateState.LastErrorCode, "messageKey", s.privateState.LastTriggerKey)
	return s.savePrivate(ctx)
}

// ResumePrivateReplies 只核验和读取，先持久保存最新水位，再解除暂停；不回放旧消息。
func (s *Service) ResumePrivateReplies(ctx context.Context) (*model.PrivateOverview, error) {
	if !s.autoRunMu.TryLock() {
		return nil, model.Fail("BUSY", "自动任务正在检查，请稍后恢复")
	}
	defer s.autoRunMu.Unlock()
	s.privateMu.Lock()
	config := s.privateConfig
	revision := s.privateState.Revision
	initialErr := privateResumeAllowed(&s.privateState)
	s.privateMu.Unlock()
	if !config.Enabled || !config.AutoReply || !config.AllowSend {
		return nil, model.Fail("PRIVATE_DISABLED", "请先开启绑定大号的私信AI自动回复")
	}
	if initialErr != nil {
		return nil, initialErr
	}
	s.mu.Lock()
	if err := s.privateAccount(ctx, &config); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	account, err := s.Bili.Status(ctx)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if account.UID != config.SenderUID {
		return nil, model.Fail("ACCOUNT", "当前账号不符合私信绑定，未恢复")
	}
	page, err := s.readPrivate(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.privateAccount(ctx, &config); err != nil {
		return nil, err
	}
	s.privateMu.Lock()
	if s.privateState.Revision != revision || s.privateConfig.SenderUID != config.SenderUID || s.privateConfig.PeerUID != config.PeerUID {
		s.privateMu.Unlock()
		return nil, model.Fail("STATE", "核验期间配置改变，未恢复")
	}
	if err := privateResumeAllowed(&s.privateState); err != nil {
		s.privateMu.Unlock()
		return nil, err
	}
	state := s.privateState
	for _, message := range page.Messages {
		state.Cursor = max(state.Cursor, message.Seq)
	}
	state.Initialized = true
	state.Since = time.Now().Unix()
	state.Paused, state.ReadPaused = false, false
	state.ReadFailures = 0
	state.Reason = ""
	state.LastErrorCode, state.LastErrorStage, state.LastErrorAt = "", "", ""
	state.LastAt = time.Now().UTC().Format(time.RFC3339Nano)
	state.NextAt = time.Now().Add(time.Duration(config.IntervalSeconds) * time.Second).UTC().Format(time.RFC3339Nano)
	state.Revision++
	if err := s.Store.Write(ctx, "data/private/state.json", &state); err != nil {
		s.privateMu.Unlock()
		return nil, err
	}
	s.privateState = state
	s.privateMu.Unlock()
	s.wakeRobot()
	return s.PrivateOverview(ctx)
}

func privateResumeAllowed(state *model.PrivateState) error {
	if state == nil {
		return model.Fail("STATE", "缺少私信状态")
	}
	if state.Processing != "" {
		return model.Fail("BUSY", "存在未完成的私信处理，不能跳过核对恢复")
	}
	for _, row := range state.Outbox {
		if row.Status == "sending" || row.Status == "unknown" {
			return model.Fail("PRIVATE_UNKNOWN", "存在结果未知的发送，先人工核对，不恢复或重试")
		}
	}
	return nil
}

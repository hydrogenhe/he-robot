package core

import (
	"context"
	"gpt-robot/model"
	"time"
)

// PrivateTick 只对绑定大号的新文字私信生成AI回复，首次只建基线。
func (s *Service) PrivateTick(ctx context.Context) error {
	if !s.autoRunMu.TryLock() {
		return model.Fail("BUSY", "自动任务正在执行")
	}
	defer s.autoRunMu.Unlock()
	runCtx, runCancel := s.beginAutoRun(ctx)
	defer s.endAutoRun(runCancel)
	ctx = runCtx
	if !s.robotRunning() {
		return nil
	}
	s.privateMu.Lock()
	config := s.privateConfig
	paused := s.privateState.Paused
	readPaused := s.privateState.ReadPaused
	failures := s.privateState.ReadFailures
	nextAt := s.privateState.NextAt
	s.privateMu.Unlock()
	if readPaused || (failures > 0 && untilRun(nextAt) > 0) {
		return nil
	}
	if !config.Enabled || (!config.AutoReply && !config.MemoryEnabled) || (paused && !config.MemoryEnabled) {
		return nil
	}
	// 已知冷却未到时只调整本地唤醒时间，不先查询平台再发现不能回复。
	if config.AutoReply && config.AllowSend && !paused {
		s.privateMu.Lock()
		ready := privateCooldownReady(s.privateState.Outbox, time.Now())
		if !ready.IsZero() {
			s.privateState.NextAt = ready.UTC().Format(time.RFC3339Nano)
			err := s.savePrivate(ctx)
			s.privateMu.Unlock()
			return err
		}
		s.privateMu.Unlock()
	}
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	err := s.privateTick(ctx, &config)
	if err != nil && ctx.Err() == nil && s.robotRunning() {
		_ = s.privateFailure(ctx, err, "processing")
	}
	return err
}
func (s *Service) privateTick(ctx context.Context, config *model.PrivateConfig) error {
	page, err := s.readPrivate(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return s.privateReadFailure(ctx, err)
	}
	s.privateMu.Lock()
	s.privateState.ReadFailures = 0
	s.privateState.ReadPaused = false
	if s.privateState.LastErrorStage == "read" {
		s.privateState.LastErrorCode, s.privateState.LastErrorStage, s.privateState.LastErrorAt = "", "", ""
		if !s.privateState.Paused {
			s.privateState.Reason = ""
		}
	}

	s.privateState.LastAt = time.Now().UTC().Format(time.RFC3339Nano)
	interval := config.IntervalSeconds
	if s.privatePushStatus.Connected && s.privatePushStatus.Verified {
		interval = max(120, interval)
	}
	s.privateState.NextAt = time.Now().Add(time.Duration(interval) * time.Second).UTC().Format(time.RFC3339Nano)
	if !s.privateState.Initialized {
		for _, message := range page.Messages {
			s.privateState.Cursor = max(s.privateState.Cursor, message.Seq)
		}
		s.privateState.Initialized, s.privateState.Since = true, time.Now().Unix()
		err := s.savePrivate(ctx)
		s.privateMu.Unlock()
		return err
	}
	// 历史消息页多于20条时，不跨过未读取的新消息。
	if page.HasMore && len(page.Messages) > 0 && s.privateState.Cursor > 0 && page.Messages[0].Seq > s.privateState.Cursor+1 {
		s.privateMu.Unlock()
		return s.pausePrivate(ctx, "新私信超出单次20条范围，已暂停，请人工查看，不补发历史回复")
	}
	if s.privateState.Paused || !config.AutoReply || !config.AllowSend {
		err := s.savePrivate(ctx)
		s.privateMu.Unlock()
		return err
	}
	// 取最近一次尝试的冷却截止时间，避免每次碰到冷却又多等一个完整检查周期。
	ready := privateCooldownReady(s.privateState.Outbox, time.Now())
	if !ready.IsZero() {
		s.privateState.NextAt = ready.UTC().Format(time.RFC3339Nano)
		err := s.savePrivate(ctx)
		s.privateMu.Unlock()
		return err
	}
	var selected *model.PrivateMessage
	for _, message := range page.Messages {
		if message.Seq <= s.privateState.Cursor {
			continue
		}
		s.privateState.Cursor = message.Seq
		if message.Image != nil && message.Text == "" {
			message.Text = "[收到主人发来的图片，用户未附文字；请结合此前对话理解意图]"
		}
		if message.Sender != config.PeerUID || message.Receiver != config.SenderUID || message.Withdrawn || message.Timestamp < s.privateState.Since || message.Timestamp < time.Now().Add(-30*time.Minute).Unix() || message.Timestamp > time.Now().Add(5*time.Minute).Unix() || !evaluateConversation(message.Text, true).Allowed {
			continue
		}
		copyMessage := message
		selected = &copyMessage
		break
	}
	if selected == nil {
		err := s.savePrivate(ctx)
		s.privateMu.Unlock()
		return err
	}
	s.privateState.Processing = selected.Key
	s.privateState.LastTriggerKey = selected.Key
	s.privateState.LastTriggerKind = s.privateCheckSource
	if s.privateState.LastTriggerKind == "" {
		s.privateState.LastTriggerKind = "poll"
	}
	s.privateState.LastTriggerAt = time.Now().UTC().Format(time.RFC3339Nano)
	recent := recentPrivateContext(s.privateState.Inbox, selected, config)
	if err := s.savePrivate(ctx); err != nil {
		s.privateMu.Unlock()
		return err
	}
	s.privateMu.Unlock()
	input := model.OperationStart{Platform: "bilibili", Action: "private-auto-reply", Source: "automation"}
	id, err := s.StartOperation(ctx, &input)
	if err != nil {
		return err
	}
	timings := model.OperationTimings{}
	round := &conversationRound{config: *config, message: *selected, scope: model.ConversationScope{Platform: "bilibili", Account: config.SenderUID, Peer: config.PeerUID, Channel: "private-message", RequestID: id}}
	ctx = context.WithValue(ctx, conversationRoundKey{}, round)
	confirmed := false
	defer func() {
		if !confirmed {
			s.modelRouter.FinishConversation(&round.scope, false)
		}
	}()
	generationStart := time.Now()
	reply, runErr := s.generatePrivateReply(ctx, id, selected, recent)
	generationMS := time.Since(generationStart).Milliseconds()
	timings.GenerationMS = &generationMS
	stage := "generation"
	result, reference := "skipped", ""
	complete := false
	if runErr == nil && (reply != "" || round.stickerID != "") {
		out, actual, allSent, sendErr := s.sendPrivateReplyBatch(ctx, round, reply, &timings)
		reply = actual
		complete = allSent
		runErr = sendErr
		stage = "send-preflight"
		if out != nil {
			stage = "send"
			result, reference = out.Status, out.RequestID
			if !allSent && sendErr == nil && out.Status == "sent" {
				result = "partial"
			}
		}
	}
	sent := runErr == nil && result == "sent" && complete
	if handoffErr := s.finishConversation(context.WithoutCancel(ctx), round, reply, sent); handoffErr != nil && runErr == nil {
		runErr = handoffErr
	}
	confirmed = true
	finish := model.OperationFinish{Status: result, Reference: reference, Account: config.SenderUID}
	finish.Timings = &timings
	if runErr != nil {
		finish.Status, finish.ErrorCode = "failed", OperationErrorCode(runErr)
	}
	if (!s.robotRunning() || ctx.Err() != nil) && reference == "" {
		finish.Status = "cancelled"
	}
	if err := s.FinishOperation(context.WithoutCancel(ctx), id, &finish); err != nil {
		return err
	}
	s.privateMu.Lock()
	s.privateState.Processing = ""
	err = s.savePrivate(ctx)
	s.privateMu.Unlock()
	if err != nil {
		return err
	}
	if (!s.robotRunning() || ctx.Err() != nil) && reference == "" {
		return nil
	}
	// 敏感输出只跳过本条，账本已有脱敏错误码；不让一次诱导锁住主人后续正常对话。
	if runErr != nil && OperationErrorCode(runErr) == "SENSITIVE_OUTPUT" {
		return nil
	}
	if runErr != nil {
		return &privateProcessingError{err: runErr, stage: stage}
	}
	return nil
}

// privateCooldownReady 返回所有已记录尝试中最晚的有效冷却截止点，零值表示可继续。
func privateCooldownReady(outbox []model.PrivateOutbox, now time.Time) time.Time {
	ready := time.Time{}
	for _, old := range outbox {
		at, err := time.Parse(time.RFC3339Nano, old.At)
		if err != nil {
			continue
		}
		deadline := at.Add(privateSendCooldown)
		if deadline.After(now) && deadline.After(ready) {
			ready = deadline
		}
	}
	return ready
}

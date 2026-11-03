package core

import (
	"context"
	"gpt-robot/model"
	"time"
)

type proactiveSendContextKey struct{}
type proactiveSendScope struct {
	Sender, Peer, Kind string
	Binding            model.PrivateConfig
}

func (s *Service) proactiveSendAllowed(ctx context.Context) bool {
	scope, _ := ctx.Value(proactiveSendContextKey{}).(*proactiveSendScope)
	if scope == nil {
		return true
	}
	s.proactiveMu.Lock()
	c := s.proactiveConfig
	s.proactiveMu.Unlock()
	if scope.Kind == "random" || scope.Kind == "morning" || scope.Kind == "night" {
		if s.companionBusy(ctx, &scope.Binding) {
			return false
		}
	}
	return proactiveKindEnabled(c, scope.Kind) && c.SenderUID == scope.Sender && c.PeerUID == scope.Peer && s.robotRunning() && !proactiveQuiet(s.ProactiveNow())
}

// ProactiveTick 每次只处理一个到期任务；生成前、发送前、慢账号核验后均检查授权。
func (s *Service) ProactiveTick(ctx context.Context) error {
	if !s.autoRunMu.TryLock() {
		return nil
	}
	defer s.autoRunMu.Unlock()
	ctx, cancel := s.beginAutoRun(ctx)
	defer s.endAutoRun(cancel)
	if !s.robotRunning() {
		return nil
	}
	now := s.ProactiveNow()
	s.proactiveMu.Lock()
	if !s.proactiveConfig.Enabled {
		s.proactiveMu.Unlock()
		return nil
	}
	if err := s.planProactive(ctx, now); err != nil {
		s.proactiveMu.Unlock()
		return err
	}
	if proactiveQuiet(now) {
		s.proactiveMu.Unlock()
		return nil
	}
	changed := false
	index := -1
	for i, j := range s.proactiveState.Jobs {
		if j.Status != "pending" && j.Status != "ready" {
			continue
		}
		due, _ := time.Parse(time.RFC3339Nano, j.DueAt)
		expires, _ := time.Parse(time.RFC3339Nano, j.ExpiresAt)
		if !now.Before(expires) {
			s.proactiveState.Jobs[i].Status = "expired"
			changed = true
			continue
		}
		if !due.After(now) && (index < 0 || j.DueAt < s.proactiveState.Jobs[index].DueAt) {
			index = i
		}
	}
	if index < 0 {
		var err error
		if changed {
			err = s.saveProactive(ctx)
		}
		s.proactiveMu.Unlock()
		return err
	}
	job := s.proactiveState.Jobs[index]
	config := s.proactiveConfig
	if !proactiveKindEnabled(config, job.Kind) || job.SenderUID != config.SenderUID || job.PeerUID != config.PeerUID {
		job.Status = "cancelled"
		s.proactiveState.Jobs[index] = job
		err := s.saveProactive(ctx)
		s.proactiveMu.Unlock()
		return err
	}
	if job.Kind == "morning" || job.Kind == "night" || job.Kind == "random" {
		for _, old := range s.proactiveState.Jobs {
			if old.Status == "sent" && old.Key != job.Key {
				at, _ := time.Parse(time.RFC3339Nano, old.SentAt)
				if now.Sub(at) < proactiveMinimumGap*time.Minute {
					s.proactiveMu.Unlock()
					return nil
				}
			}
		}
	}
	s.proactiveMu.Unlock()
	s.privateMu.Lock()
	private := s.privateConfig
	paused := s.privateState.Paused
	// 正在对话时不强插问候；通知正文在队列中保留到安静时再发。
	cooldown := false
	for _, out := range s.privateState.Outbox {
		at, _ := time.Parse(time.RFC3339Nano, out.At)
		if time.Since(at) < privateSendCooldown {
			cooldown = true
		}
	}
	active := false
	for _, m := range s.privateState.Inbox {
		if m.Sender == private.PeerUID && now.Unix()-m.Timestamp >= 0 && now.Unix()-m.Timestamp < 10*60 {
			active = true
		}
	}
	s.privateMu.Unlock()
	if paused || !private.Enabled || !private.AllowSend || private.SenderUID != job.SenderUID || private.PeerUID != job.PeerUID || cooldown || (active && (job.Kind == "morning" || job.Kind == "night" || job.Kind == "random")) {
		return nil
	}
	if (job.Kind == "random" || job.Kind == "morning" || job.Kind == "night") && s.companionBusy(ctx, &private) {
		return nil
	}
	scope := &proactiveSendScope{Sender: job.SenderUID, Peer: job.PeerUID, Kind: job.Kind, Binding: private}
	ctx = context.WithValue(ctx, proactiveSendContextKey{}, scope)
	if !s.proactiveSendAllowed(ctx) {
		return nil
	}
	if job.OperationID == "" {
		start := model.OperationStart{Platform: "bilibili", Action: "proactive-" + job.Kind, Source: "automation"}
		id, err := s.StartOperation(ctx, &start)
		if err != nil {
			return err
		}
		job.OperationID = id
	}
	if job.Status == "pending" {
		job.Status = "generating"
		if err := s.persistProactiveJob(ctx, index, &job); err != nil {
			return err
		}
		persona, err := s.Store.Text(ctx, "data/character/persona.md")
		if err != nil {
			return s.failProactiveJob(ctx, index, &job, err)
		}
		state, err := s.CharacterStatus(ctx)
		if err != nil {
			return s.failProactiveJob(ctx, index, &job, err)
		}
		task := "现在是北京时间" + now.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("15:04") + "。这是角色主动找已绑定主人聊天的时机，不是主人刚发来的消息。"
		switch job.Kind {
		case "morning":
			task += "自然道一声早安。"
		case "night":
			task += "自然说晚安，不声称知道主人即将睡觉。"
		default:
			task += "自然开一个简短话题，可以问主人在做什么、分享想念或关心；不重复催回复，不用愧疚、占有或离开惩罚要求陪伴。"
		}
		contextText := ""
		if job.Kind == "random" {
			var topicErr error
			contextText, topicErr = s.companionTopic(ctx, &private)
			if topicErr != nil {
				return s.failProactiveJob(ctx, index, &job, topicErr)
			}
			task += contextText
		}
		task += "按人设表达，1–60字，不编造主人当前活动或双方未发生的经历。"
		prompt := model.ReplyPrompt{Persona: persona, State: state.Description, Owner: true, Channel: "private-message", Text: model.Clip(task, 500)}
		if job.Kind == "random" && private.MemoryEnabled {
			queryText := model.Clip(contextText, 80)
			query := model.PrivateMemoryQuery{Query: &queryText, PageSize: 2}
			memories, queryErr := s.PrivateMemories(ctx, &query)
			if queryErr != nil {
				return s.failProactiveJob(ctx, index, &job, queryErr)
			}
			for _, memory := range memories.Items {
				prompt.PrivateMemories = append(prompt.PrivateMemories, model.RecalledMemory{Quote: model.Clip(memory.Quote, 120)})
			}
		}
		s.sanitizeReplyPrompt(ctx, &prompt)
		bounded, err := model.BoundReplyPrompt(&prompt)
		if err != nil {
			return s.failProactiveJob(ctx, index, &job, err)
		}
		reply, err := s.generateReplyRound(ctx, job.OperationID, true, bounded)
		if err != nil {
			return s.failProactiveJob(ctx, index, &job, err)
		}
		if reply == nil || !model.Text(reply.Text, 60) || model.ContainsSensitiveText(reply.Text, s.generationSecrets(ctx)...) {
			return s.failProactiveJob(ctx, index, &job, model.Fail("AI_REPLY", "主动消息不合格"))
		}
		job.Parts = []string{reply.Text}
		job.RequestIDs = []string{model.ID()}
		job.Status = "ready"
		if err := s.persistProactiveJob(ctx, index, &job); err != nil {
			return err
		}
	}
	if !s.proactiveSendAllowed(ctx) {
		return nil
	}
	job.Status = "sending"
	if err := s.persistProactiveJob(ctx, index, &job); err != nil {
		return err
	}
	input := model.PrivateSendInput{RequestID: job.RequestIDs[job.Part], Text: job.Parts[job.Part]}
	timings := model.OperationTimings{}
	result, err := s.sendPrivate(ctx, &input, &timings)
	if err != nil {
		code := OperationErrorCode(err)
		if code == "COOLDOWN" || code == "PRIVATE_DISABLED" {
			job.Status = "ready"
			job.DueAt = now.Add(15 * time.Second).UTC().Format(time.RFC3339Nano)
			return s.persistProactiveJob(ctx, index, &job)
		}
		return s.failProactiveJob(ctx, index, &job, err)
	}
	if result == nil || result.Status != "sent" {
		return s.failProactiveJob(ctx, index, &job, model.Fail("SEND_UNKNOWN", "主动消息结果不明，不自动重发"))
	}
	job.Part++
	if job.Part < len(job.Parts) {
		job.Status = "ready"
		if job.ParentOperation {
			if err = s.recordNotification(ctx, job.OperationID, "partial", &timings); err != nil {
				return err
			}
		}
		job.DueAt = now.Add(15 * time.Second).UTC().Format(time.RFC3339Nano)
	} else {
		job.Status = "sent"
		job.SentAt = now.UTC().Format(time.RFC3339Nano)
		finish := model.OperationFinish{Status: "sent", Reference: result.RequestID, Account: job.SenderUID, Timings: &timings}
		if job.ParentOperation {
			err = s.recordNotification(ctx, job.OperationID, "sent", &timings)
		} else {
			err = s.FinishOperation(ctx, job.OperationID, &finish)
		}
		if err != nil {
			return err
		}
	}
	return s.persistProactiveJob(ctx, index, &job)
}
func (s *Service) persistProactiveJob(ctx context.Context, index int, job *model.ProactiveJob) error {
	s.proactiveMu.Lock()
	defer s.proactiveMu.Unlock()
	s.proactiveState.Jobs[index] = *job
	return s.saveProactive(ctx)
}
func (s *Service) failProactiveJob(ctx context.Context, index int, job *model.ProactiveJob, err error) error {
	job.Status = "failed"
	job.ErrorCode = OperationErrorCode(err)
	if job.ErrorCode == "SEND_UNKNOWN" {
		job.Status = "unknown"
	}
	saveErr := s.persistProactiveJob(ctx, index, job)
	finish := model.OperationFinish{Status: job.Status, ErrorCode: job.ErrorCode, Account: job.SenderUID}
	var finishErr error
	if job.ParentOperation {
		finishErr = s.recordNotification(context.WithoutCancel(ctx), job.OperationID, job.Status, nil)
	} else {
		finishErr = s.FinishOperation(context.WithoutCancel(ctx), job.OperationID, &finish)
	}
	if saveErr != nil {
		return saveErr
	}
	if finishErr != nil {
		return finishErr
	}
	return err
}
func (s *Service) proactiveWait() time.Duration {
	s.proactiveMu.Lock()
	defer s.proactiveMu.Unlock()
	if !s.proactiveConfig.Enabled {
		return time.Hour
	}
	now := s.ProactiveNow()
	if proactiveQuiet(now) {
		return max(time.Second, proactiveNextMorning(now).Sub(now))
	}
	wait := time.Hour
	for _, job := range s.proactiveState.Jobs {
		if job.Status == "pending" || job.Status == "ready" {
			due, _ := time.Parse(time.RFC3339Nano, job.DueAt)
			wait = min(wait, max(30*time.Second, due.Sub(now)))
		}
	}
	return wait
}

// recordNotification 更新原活动的通知阶段，视频和日记不另拆一条历史，也不重复计量。
func (s *Service) recordNotification(ctx context.Context, id, status string, timings *model.OperationTimings) error {
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	op, ok := s.operations[id]
	if !ok {
		return model.Fail("NOT_FOUND", "通知所属活动不存在")
	}
	op.NotificationStatus = status
	op.Timings = cloneOperationTimings(op.Timings)
	if timings != nil {
		if op.Timings == nil {
			op.Timings = &model.OperationTimings{}
		}
		if timings.AccountCheckMS != nil {
			value := *timings.AccountCheckMS
			op.DurationMS += value
			if op.Timings.AccountCheckMS != nil {
				value += *op.Timings.AccountCheckMS
			}
			op.Timings.AccountCheckMS = &value
		}
		op.Timings.AccountAttempts += timings.AccountAttempts
		if timings.SendMS != nil {
			op.DurationMS += *timings.SendMS
			value := *timings.SendMS
			if op.Timings.SendMS != nil {
				value += *op.Timings.SendMS
			}
			op.Timings.SendMS = &value
		}
	}
	if err := s.Store.AppendOperation(context.WithoutCancel(ctx), &op); err != nil {
		return err
	}
	s.operations[id] = op
	return nil
}

func proactiveKindEnabled(c model.ProactiveConfig, kind string) bool {
	if !c.Enabled {
		return false
	}
	switch kind {
	case "video":
		return c.VideoReflections
	case "diary":
		return c.DiaryDelivery
	case "morning", "night":
		return c.Greetings
	case "random":
		return c.RandomMax > 0
	}
	return false
}

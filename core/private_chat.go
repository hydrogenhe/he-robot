package core

import (
	"context"
	"gpt-robot/model"
	"log/slog"
	"slices"
	"strings"
	"time"
)

// privateSendCooldown 绑定大号私信两次发送尝试之间至少等待10秒，不影响公开评论冷却。
const privateSendCooldown = 10 * time.Second

func validatePrivateConfig(c *model.PrivateConfig) error {
	if c == nil || c.IntervalSeconds < 15 || c.IntervalSeconds > 3600 {
		return model.Fail("INPUT", "私信检查间隔须为15–3600秒")
	}
	if c.Enabled && (!model.ValidNumber(c.SenderUID) || !model.ValidNumber(c.PeerUID) || c.SenderUID == "0" || c.PeerUID == "0" || c.SenderUID == c.PeerUID) {
		return model.Fail("INPUT", "请先绑定机器人和大号UID")
	}
	if c.MemoryEnabled && !c.Enabled {
		return model.Fail("INPUT", "自动记忆需要开启私信功能")
	}
	if c.AutoReply && (!c.Enabled || !c.AllowSend) {
		return model.Fail("INPUT", "私信自动回复需要开启私信和发送权限")
	}
	return nil
}
func (s *Service) PrivateOverview(ctx context.Context) (*model.PrivateOverview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.privateMu.Lock()
	defer s.privateMu.Unlock()
	state := s.privateState
	state.Inbox = slices.Clone(state.Inbox)
	state.Outbox = slices.Clone(state.Outbox)
	result := &model.PrivateOverview{Push: s.privatePushStatus, Config: s.privateConfig, State: state}
	return result, nil
}
func (s *Service) SavePrivateConfig(ctx context.Context, input *model.PrivateSettings) (*model.PrivateOverview, error) {
	if input == nil {
		return nil, model.Fail("INPUT", "缺少私信设置")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.privateMu.Lock()
	config := s.privateConfig
	s.privateMu.Unlock()
	if config.SenderUID == "" && config.PeerUID == "" {
		s.autoMu.Lock()
		config.SenderUID, config.PeerUID = s.autoConfig.AccountUID, s.autoConfig.PriorityUID
		s.autoMu.Unlock()
	}
	memoryOnlyChange := input.MemoryEnabled != nil && *input.MemoryEnabled != config.MemoryEnabled && input.Enabled == config.Enabled && input.AllowSend == config.AllowSend && input.AutoReply == config.AutoReply && input.IntervalSeconds == config.IntervalSeconds
	intervalOnlyChange := input.IntervalSeconds != config.IntervalSeconds && input.Enabled == config.Enabled && input.AllowSend == config.AllowSend && input.AutoReply == config.AutoReply && (input.MemoryEnabled == nil || *input.MemoryEnabled == config.MemoryEnabled)
	passiveChange := memoryOnlyChange || intervalOnlyChange
	config.Enabled, config.AllowSend, config.AutoReply, config.IntervalSeconds = input.Enabled, input.AllowSend, input.AutoReply, input.IntervalSeconds
	if input.MemoryEnabled != nil {
		if *input.MemoryEnabled && !config.MemoryEnabled {
			config.MemoryGeneration = model.ID()
		}
		config.MemoryEnabled = *input.MemoryEnabled
	}
	if err := validatePrivateConfig(&config); err != nil {
		return nil, err
	}
	if config.Enabled {
		account, err := s.Bili.Status(ctx)
		if err != nil {
			return nil, err
		}
		if account.UID != config.SenderUID {
			return nil, model.Fail("ACCOUNT", "当前账号与私信绑定账号不一致")
		}
	}
	s.privateMu.Lock()
	state := s.privateState
	if !s.privateConfig.AutoReply && config.AutoReply {
		state.Initialized = false
		state.Cursor = 0
		state.Since = time.Now().Unix()
	}
	// 未知发送不得靠重新保存设置恢复自动发送。
	for _, out := range state.Outbox {
		if !passiveChange && config.AutoReply && (out.Status == "sending" || out.Status == "unknown") {
			s.privateMu.Unlock()
			return nil, model.Fail("PRIVATE_UNKNOWN", "有结果未知的私信，请先在B站人工核对；不可重新启用自动发送")
		}
	}
	if !passiveChange && config.AutoReply && state.Processing != "" {
		s.privateMu.Unlock()
		return nil, model.Fail("BUSY", "私信正在处理，请先暂停机器人")
	}
	if !passiveChange {
		if config.AutoReply && state.Paused {
			state.Initialized = false
			state.Cursor = 0
			state.Since = time.Now().Unix()
		}
		state.Paused, state.Reason = false, ""
		state.ReadPaused = false
		state.ReadFailures = 0
		state.LastErrorCode, state.LastErrorStage, state.LastErrorAt = "", "", ""
	}
	if !passiveChange || (!state.ReadPaused && state.ReadFailures == 0) {
		state.NextAt = ""
	}
	state.Revision++
	if err := s.Store.Write(ctx, "data/private/state.json", &state); err != nil {
		s.privateMu.Unlock()
		return nil, err
	}
	if err := s.Store.Write(ctx, "config/private-chat.json", &config); err != nil {
		s.privateMu.Unlock()
		return nil, err
	}
	s.privateConfig, s.privateState = config, state
	s.privateMu.Unlock()
	s.modelRouter.CloseConversations()
	s.wakeRobot()
	return s.PrivateOverview(ctx)
}
func (s *Service) privateAccount(ctx context.Context, config *model.PrivateConfig) error {
	credentials, err := s.Credentials(ctx)
	if err != nil {
		return err
	}
	if credentials == nil || credentials.UID != config.SenderUID {
		return model.Fail("ACCOUNT", "私信登录账号已变化，请重新核验")
	}
	return nil
}
func (s *Service) savePrivate(ctx context.Context) error {
	return s.Store.Write(context.WithoutCancel(ctx), "data/private/state.json", &s.privateState)
}
func (s *Service) pausePrivate(ctx context.Context, reason string) error {
	s.privateMu.Lock()
	defer s.privateMu.Unlock()
	s.privateState.Paused, s.privateState.Reason, s.privateState.NextAt = true, reason, ""
	return s.savePrivate(ctx)
}
func (s *Service) readPrivate(ctx context.Context) (*model.PrivatePage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.privateMu.Lock()
	config := s.privateConfig
	s.privateMu.Unlock()
	if !config.Enabled {
		return nil, model.Fail("PRIVATE_DISABLED", "私信功能已关闭")
	}
	if err := s.privateAccount(ctx, &config); err != nil {
		return nil, err
	}
	page, err := s.Bili.PrivateMessages(ctx, config.SenderUID, config.PeerUID)
	if err != nil {
		return nil, err
	}
	s.privateMu.Lock()
	defer s.privateMu.Unlock()
	// 白名单过滤在核心层再做一次，私信正文不写公共事件或记忆。
	messages := map[string]model.PrivateMessage{}
	for _, row := range s.privateState.Inbox {
		messages[row.Key] = row
	}
	clean := []model.PrivateMessage{}
	for _, row := range page.Messages {
		if !((row.Sender == config.SenderUID && row.Receiver == config.PeerUID) || (row.Sender == config.PeerUID && row.Receiver == config.SenderUID)) {
			continue
		}
		if old, ok := messages[row.Key]; ok && old.Withdrawn {
			row.Withdrawn = true
		}
		if row.Withdrawn {
			row.Text = ""
			row.Image = nil
		}
		messages[row.Key] = row
		clean = append(clean, row)
	}
	memoryPage := model.PrivatePage{Messages: clean, HasMore: page.HasMore}
	if err := s.syncCoreName(ctx, &config, &memoryPage); err != nil {
		return nil, err
	}
	if err := s.syncPrivateMemory(ctx, &config, &memoryPage); err != nil {
		return nil, err
	}
	s.privateState.Inbox = []model.PrivateMessage{}
	for _, row := range messages {
		s.privateState.Inbox = append(s.privateState.Inbox, row)
	}
	slices.SortFunc(s.privateState.Inbox, comparePrivateMessages)
	if len(s.privateState.Inbox) > 200 {
		s.privateState.Inbox = s.privateState.Inbox[len(s.privateState.Inbox)-200:]
	}
	if err := s.savePrivate(ctx); err != nil {
		return nil, err
	}
	result := &model.PrivatePage{Messages: clean, HasMore: page.HasMore}
	slices.SortFunc(result.Messages, comparePrivateMessages)
	return result, nil
}
func comparePrivateMessages(a, b model.PrivateMessage) int {
	if a.Seq < b.Seq {
		return -1
	}
	if a.Seq > b.Seq {
		return 1
	}
	return strings.Compare(a.Key, b.Key)
}
func (s *Service) RefreshPrivate(ctx context.Context) (*model.PrivateOverview, error) {
	if _, err := s.readPrivate(ctx); err != nil {
		return nil, err
	}
	return s.PrivateOverview(ctx)
}

// SendPrivate 收件人永远取已绑定大号，客户端没有任意UID发送入口。
func (s *Service) SendPrivate(ctx context.Context, input *model.PrivateSendInput) (*model.PrivateOutbox, error) {
	return s.sendPrivate(ctx, input, nil)
}

// sendPrivate 与公开入口共用发送校验和幂等，自动回复可附加分段计时。
func (s *Service) sendPrivate(ctx context.Context, input *model.PrivateSendInput, timings *model.OperationTimings) (*model.PrivateOutbox, error) {
	if input == nil || !model.ValidID(input.RequestID) || !model.Text(input.Text, 500) {
		return nil, model.Fail("INPUT", "私信正文1–500字，并需有效请求ID")
	}
	part := validReplyBatch(ctx, input)
	if (input.GroupID != "" || input.StickerID != "") && part == nil {
		return nil, model.Fail("PRIVATE_SCOPE", "分条回复计划未经服务端核验")
	}
	round, _ := ctx.Value(conversationRoundKey{}).(*conversationRound)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.privateMu.Lock()
	config := s.privateConfig
	for _, old := range s.privateState.Outbox {
		if old.RequestID == input.RequestID {
			s.privateMu.Unlock()
			if old.Text != input.Text || old.AutoKey != input.AutoKey || old.StickerID != input.StickerID || old.GroupID != input.GroupID || old.Part != input.Part {
				return nil, model.Fail("CONFLICT", "请求ID已对应其他私信")
			}
			return &old, nil
		}
		at, _ := time.Parse(time.RFC3339Nano, old.At)
		duplicate := old.Text == input.Text
		if part != nil {
			duplicate = old.GroupID != input.GroupID && (old.GroupHash == round.batch.Hash || (old.GroupHash == "" && len(round.batch.Parts) == 1 && old.Text == input.Text))
		}
		if duplicate && time.Since(at) < 10*time.Minute {
			s.privateMu.Unlock()
			return nil, model.Fail("DUPLICATE", "10分钟内已有相同私信，不重复发送")
		}
	}
	permitted := config.Enabled && config.AllowSend && !s.privateState.Paused
	if input.AutoKey != "" {
		permitted = permitted && config.AutoReply && s.privateState.Processing == input.AutoKey && s.robotRunning()
	}
	s.privateMu.Unlock()
	if !permitted {
		return nil, model.Fail("PRIVATE_DISABLED", "私信发送已关闭或机器人已暂停")
	}
	if err := s.privateAccount(ctx, &config); err != nil {
		return nil, err
	}
	accountStart := time.Now()
	accountErr := s.verifyPrivateSendAccount(ctx, &config, input.AutoKey != "", timings)
	if timings != nil {
		elapsed := time.Since(accountStart).Milliseconds()
		if timings.AccountCheckMS == nil {
			timings.AccountCheckMS = &elapsed
		} else {
			*timings.AccountCheckMS += elapsed
		}
	}
	if accountErr != nil {
		return nil, accountErr
	}
	s.privateMu.Lock()
	// 慢网络核验之后再次检查权限，暂停或修改开关可以阻止尚未提交的消息。
	if s.privateConfig != config || !s.privateConfig.Enabled || !s.privateConfig.AllowSend || s.privateState.Paused || (input.AutoKey != "" && (!s.privateConfig.AutoReply || !s.robotRunning() || ctx.Err() != nil)) {
		s.privateMu.Unlock()
		return nil, model.Fail("PRIVATE_DISABLED", "私信权限已关闭，未发送")
	}
	for _, old := range s.privateState.Outbox {
		if part != nil && old.GroupID == input.GroupID && old.Part < input.Part && old.Status == "sent" {
			continue
		}
		at, _ := time.Parse(time.RFC3339Nano, old.At)
		if time.Since(at) < privateSendCooldown {
			s.privateMu.Unlock()
			return nil, model.Fail("COOLDOWN", "私信发送冷却10秒")
		}
	}
	out := model.PrivateOutbox{RequestID: input.RequestID, At: time.Now().UTC().Format(time.RFC3339Nano), Text: input.Text, AutoKey: input.AutoKey, Status: "sending"}
	if part != nil {
		out.GroupID = input.GroupID
		out.Part = input.Part
		out.StickerID = input.StickerID
		out.GroupHash = round.batch.Hash
	}
	s.privateState.Outbox = append(s.privateState.Outbox, out)
	if err := s.savePrivate(ctx); err != nil {
		s.privateMu.Unlock()
		return nil, err
	}
	index := len(s.privateState.Outbox) - 1
	s.privateMu.Unlock()
	sendStart := time.Now()
	var key string
	var sendErr error
	if part != nil && part.Image != nil {
		key, sendErr = s.Bili.SendPrivateImage(ctx, config.SenderUID, config.PeerUID, part.Image, input.RequestID)
	} else {
		key, sendErr = s.Bili.SendPrivate(ctx, config.SenderUID, config.PeerUID, input.Text, input.RequestID)
	}
	if timings != nil {
		elapsed := time.Since(sendStart).Milliseconds()
		if timings.SendMS == nil {
			timings.SendMS = &elapsed
		} else {
			*timings.SendMS += elapsed
		}
	}
	out.MessageKey, out.Status = key, "sent"
	if sendErr != nil {
		out.Status, out.ErrorCode = "failed", OperationErrorCode(sendErr)
		if out.ErrorCode == "SEND_UNKNOWN" {
			out.Status = "unknown"
		}
		slog.Warn("私信发送未成功", "requestId", out.RequestID, "code", out.ErrorCode)
	}
	s.privateMu.Lock()
	defer s.privateMu.Unlock()
	s.privateState.Outbox[index] = out
	if sendErr != nil {
		s.privateState.Paused, s.privateState.Reason = true, "私信发送失败或结果未知，已暂停，不自动重试"
	}
	if err := s.savePrivate(ctx); err != nil {
		return &out, err
	}
	return &out, sendErr
}

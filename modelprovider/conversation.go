package modelprovider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"gpt-robot/businesshost"
	"gpt-robot/model"
	"sync"
	"time"
)

// conversationReserve 为下一轮预留的输出预算（Token）；原生用量迟到仍可能使单轮越线。
const conversationReserve int64 = 4096

// conversationLimit 限制同时驻留的会话数，避免未来多渠道接入无限启动本地进程。
const conversationLimit = 16

type conversationWindow struct {
	root            context.Context
	stop            context.CancelFunc
	mu              sync.Mutex
	id              string
	key             string
	revision        uint64
	session         businesshost.JSONSession
	cancel          context.CancelFunc
	timer           *time.Timer
	timerGeneration uint64
	born            time.Time
	last            time.Time
	turns           int
	tokens          int64
	contextEstimate int64
	pending         string
	closed          bool
}

// conversationKey 只使用已核验的范围和策略版本，文件或状态页不输出账号原文。
func conversationKey(scope *model.ConversationScope) string {
	raw, _ := json.Marshal([]string{scope.Platform, scope.Account, scope.Peer, scope.Channel, scope.Revision})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (r *Router) ConversationStatus() model.ConversationStatus {
	r.windowsMu.Lock()
	defer r.windowsMu.Unlock()
	status := model.ConversationStatus{Config: r.windowConfig, Windows: []model.ConversationWindowStatus{}}
	for key, w := range r.windows {
		// 当前有生成时不等待该会话锁；状态只报告已安全提交的快照。
		if !w.mu.TryLock() {
			status.Windows = append(status.Windows, model.ConversationWindowStatus{ID: key[:12], Pending: true})
			continue
		}
		if !w.closed {
			status.Windows = append(status.Windows, model.ConversationWindowStatus{ID: w.id, Turns: w.turns, Tokens: w.tokens, Pending: w.pending != ""})
		}
		w.mu.Unlock()
	}
	return status
}

func (r *Router) ConfigureConversations(config *model.ConversationConfig) error {
	if err := config.Validate(); err != nil {
		return err
	}
	r.CloseConversations()
	r.windowsMu.Lock()
	r.windowConfig = *config
	r.windowsMu.Unlock()
	return nil
}

func (r *Router) CloseConversations() {
	r.windowsMu.Lock()
	r.windowRevision++
	windows := r.windows
	r.windows = map[string]*conversationWindow{}
	r.windowsMu.Unlock()
	for _, w := range windows {
		w.stop()
		w.mu.Lock()
		closeConversation(w)
		w.mu.Unlock()
	}
}

func closeConversation(w *conversationWindow) {
	if w.closed {
		return
	}
	w.closed = true
	w.timerGeneration++
	if w.timer != nil {
		w.timer.Stop()
	}
	if w.session != nil {
		w.session.Close()
	}
	if w.cancel != nil {
		w.cancel()
	}
}

func (r *Router) GenerateConversationJSON(ctx context.Context, scope *model.ConversationScope, instruction string, body []byte) (*model.GeneratedReply, error) {
	if scope == nil || scope.Platform == "" || scope.Account == "" || scope.Peer == "" || scope.Channel != "private-message" || !model.ValidID(scope.RequestID) {
		return nil, model.Fail("PRIVATE_SCOPE", "会话范围未经核验")
	}
	key := conversationKey(scope)
	r.windowsMu.Lock()
	config, revision := r.windowConfig, r.windowRevision
	configSnapshot := r.Config()
	if !config.Enabled {
		r.windowsMu.Unlock()
		return r.GenerateJSON(ctx, instruction, body)
	}
	w := r.windows[key]
	if w == nil {
		// 清除已关闭索引，不驱逐有未确认发送的会话。
		for k, old := range r.windows {
			if old.mu.TryLock() {
				if old.closed {
					delete(r.windows, k)
				}
				old.mu.Unlock()
			}
		}
		if len(r.windows) >= conversationLimit {
			r.windowsMu.Unlock()
			return nil, model.Fail("CONVERSATION_BUSY", "驻留会话已达上限，请等待空闲会话结束")
		}
		root, stop := context.WithCancel(context.Background())
		w = &conversationWindow{key: key, revision: revision, root: root, stop: stop}
		r.windows[key] = w
	}
	r.windowsMu.Unlock()
	w.mu.Lock()
	defer w.mu.Unlock()
	r.windowsMu.Lock()
	current := r.windows[key] == w && r.windowRevision == revision
	r.windowsMu.Unlock()
	if !current {
		return nil, model.Fail("CONVERSATION_CHANGED", "会话配置已变化，未提交模型调用")
	}
	if w.pending != "" {
		return nil, model.Fail("CONVERSATION_BUSY", "上一轮结果尚未确认，未提交新的模型调用")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := r.ConversationNow()
	// 用字节数作保守估算，不把字符数伪装成分词结果；原生回执用于累计结算。
	estimate := int64(len(instruction)+len(body)) + conversationReserve
	if estimate > config.ContextTokens || estimate > config.TotalTokens {
		return nil, model.Fail("CONTEXT_BUDGET", "本轮输入及输出预留超过会话预算，请提高限额或缩短输入")
	}
	if w.closed || w.revision != revision || w.session == nil || w.turns >= config.MaxTurns || now.Sub(w.last) >= time.Duration(config.IdleSeconds)*time.Second || now.Sub(w.born) >= time.Duration(config.MaxAgeSeconds)*time.Second || w.contextEstimate+estimate > config.ContextTokens || w.tokens+w.contextEstimate+estimate > config.TotalTokens {
		closeConversation(w)
		w.closed = false
		w.id = model.ID()
		w.born = now
		w.last = now
		w.turns = 0
		w.tokens = 0
		w.contextEstimate = 0
		w.revision = revision
		life, cancel := context.WithCancel(w.root)
		w.cancel = cancel
		// 握手等待有界；会话生命不绑定单轮请求，过期由下方定时器回收。
		startupTimer := time.AfterFunc(15*time.Second, cancel)
		stopRequest := context.AfterFunc(ctx, cancel)
		var err error
		w.session, err = r.openConversation(life, &configSnapshot, instruction)
		startupTimer.Stop()
		stopRequest()
		if err != nil {
			closeConversation(w)
			return nil, err
		}
	}
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timerGeneration++
	w.pending = scope.RequestID
	// 已驻留窗口自带此前轮次，近期原文和交接只在新窗口首轮注入。
	if w.turns > 0 {
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(body, &payload); err != nil {
			w.pending = ""
			closeConversation(w)
			return nil, model.Fail("INPUT", "会话业务输入无法解析")
		}
		delete(payload, "recent")
		delete(payload, "handoff")
		body, _ = json.Marshal(payload)
	}
	result, err := w.session.GenerateJSON(ctx, instruction, body)
	if err != nil || result == nil {
		if err == nil {
			err = model.Fail("AI_REPLY", "模型没有返回结果，窗口已结束")
		}
		w.pending = ""
		closeConversation(w)
		return result, err
	}
	w.turns++
	w.contextEstimate += int64(len(body) + len(result.Text))
	if w.turns == 1 {
		w.contextEstimate += int64(len(instruction))
	}
	if result.Usage == nil || result.Usage.Input < 0 || result.Usage.Output < 0 {
		closeConversation(w)
	} else {
		w.tokens += result.Usage.Input + result.Usage.Output
		w.contextEstimate = max(w.contextEstimate, result.Usage.Input+result.Usage.Output)
	}
	// 尚未确认发送也设置回收上限；不发任何总结请求。
	r.armConversation(w, config)
	return result, nil
}

func (r *Router) openConversation(life context.Context, config *model.ModelProviderConfig, instruction string) (businesshost.JSONSession, error) {
	// 使用单独的启动生命周期：请求握手到期时主动终止该进程。
	var backend interface {
		OpenConversation(context.Context, string) (businesshost.JSONSession, error)
	}
	if config.Provider == "codex" {
		if config.CodexModel == "" {
			return nil, model.Fail("MODEL_UNAVAILABLE", "请先明确选择本机模型")
		}
		effort := config.CodexEffort
		if effort == "" {
			effort = "low"
		}
		backend = businesshost.NewCodexBackend(r.root, config.CodexModel, effort)
	} else {
		backend = &businesshost.DeepSeekHarnessBackend{Model: config.DeepSeekModel, Effort: config.HarnessEffort}
	}
	return backend.OpenConversation(life, instruction)
}

func (r *Router) armConversation(w *conversationWindow, config model.ConversationConfig) {
	delay := min(time.Duration(config.IdleSeconds)*time.Second, w.born.Add(time.Duration(config.MaxAgeSeconds)*time.Second).Sub(r.ConversationNow()))
	if delay < 0 {
		delay = 0
	}
	w.timerGeneration++
	generation := w.timerGeneration
	w.timer = time.AfterFunc(delay, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.timerGeneration == generation {
			closeConversation(w)
		}
	})
}

// FinishConversation 仅由业务发送结果确认；失败/未知/跳过都丢弃窗口，不重试模型或平台。
func (r *Router) FinishConversation(scope *model.ConversationScope, sent bool) {
	if scope == nil {
		return
	}
	r.windowsMu.Lock()
	w := r.windows[conversationKey(scope)]
	config := r.windowConfig
	r.windowsMu.Unlock()
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending != scope.RequestID {
		return
	}
	w.pending = ""
	if !sent {
		closeConversation(w)
		return
	}
	w.last = r.ConversationNow()
	if w.turns >= config.MaxTurns || w.tokens >= config.TotalTokens || w.contextEstimate+conversationReserve >= config.ContextTokens {
		closeConversation(w)
		return
	}
	if w.timer != nil {
		w.timer.Stop()
	}
	r.armConversation(w, config)
}

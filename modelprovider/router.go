package modelprovider

import (
	"context"
	"gpt-robot/businesshost"
	"gpt-robot/model"
	"strings"
	"sync"
	"time"
)

// Router 对单次生成快照供应商配置，回复与日记共用同一传输接口。
type Router struct {
	// ConversationNow 默认time.Now，仅作为会话策略时钟依赖，协议超时仍用真实时间。
	ConversationNow func() time.Time
	windowsMu       sync.Mutex
	windows         map[string]*conversationWindow
	windowConfig    model.ConversationConfig
	windowRevision  uint64
	root            string
	mu              sync.RWMutex
	config          model.ModelProviderConfig
}
type RouterInterface interface {
	GenerateConversationJSON(context.Context, *model.ConversationScope, string, []byte) (*model.GeneratedReply, error)
	ConversationStatus() model.ConversationStatus
	ConfigureConversations(*model.ConversationConfig) error
	FinishConversation(*model.ConversationScope, bool)
	CloseConversations()
	GenerateJSON(context.Context, string, []byte) (*model.GeneratedReply, error)
	Config() model.ModelProviderConfig
	Configure(*model.ModelProviderConfig) error
}

var _ RouterInterface = (*Router)(nil)
var _ businesshost.JSONBackend = (*Router)(nil)

func New(root string) *Router {
	router := &Router{
		ConversationNow: time.Now,
		windows:         map[string]*conversationWindow{},
		windowConfig:    model.DefaultConversationConfig(),
		root:            root,
		config:          model.ModelProviderConfig{Mode: "local", Provider: "codex", DeepSeekModel: "deepseek-flash", CodexEffort: "low", HarnessEffort: "low"},
	}
	return router
}

func Validate(config *model.ModelProviderConfig) error {
	if config == nil || config.Mode != "local" || (config.Provider != "codex" && config.Provider != "deepseek") {
		return model.Fail("INPUT", "只支持本机Codex或DeepSeek Harness")
	}
	if config.CodexEffort != "" && config.CodexEffort != "none" && config.CodexEffort != "low" && config.CodexEffort != "medium" && config.CodexEffort != "high" {
		return model.Fail("INPUT", "不支持的Codex思考档位")
	}
	if config.HarnessEffort != "" && config.HarnessEffort != "none" && config.HarnessEffort != "low" && config.HarnessEffort != "high" {
		return model.Fail("INPUT", "不支持的DeepSeek思考档位")
	}
	if config.CodexModel != "" && (!model.Text(config.CodexModel, 100) || strings.ContainsAny(config.CodexModel, " \r\n\t")) {
		return model.Fail("INPUT", "Codex模型ID不正确")
	}
	if config.DeepSeekModel != "deepseek-flash" && config.DeepSeekModel != "deepseek-v4-pro" {
		return model.Fail("INPUT", "请选择支持的DeepSeek模型")
	}
	return nil
}

func (r *Router) Config() model.ModelProviderConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config
}
func (r *Router) Configure(config *model.ModelProviderConfig) error {
	if err := Validate(config); err != nil {
		return err
	}
	r.windowsMu.Lock()
	r.mu.Lock()
	r.config = *config
	r.mu.Unlock()
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
	return nil
}
func (r *Router) GenerateJSON(ctx context.Context, instruction string, body []byte) (*model.GeneratedReply, error) {
	config := r.Config()
	if config.Mode == "local" && config.Provider == "codex" {
		if config.CodexModel == "" {
			return nil, model.Fail("MODEL_UNAVAILABLE", "请先明确选择并连接本机模型，不自动选择其他模型")
		}
		effort := config.CodexEffort
		if effort == "" {
			effort = "low"
		}
		return businesshost.NewCodexBackend(r.root, config.CodexModel, effort).GenerateJSON(ctx, instruction, body)
	}
	if config.Mode == "local" {
		backend := &businesshost.DeepSeekHarnessBackend{Model: config.DeepSeekModel, Effort: config.HarnessEffort}
		return backend.GenerateJSON(ctx, instruction, body)
	}
	return nil, model.Fail("MODEL_MODE", "请选择本机Harness，不支持其他接入方式")
}

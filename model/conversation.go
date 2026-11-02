package model

// ConversationConfig 跨供应商会话轮换规则；Token阈值为已知回执和预估的控制边界，不是精确费用承诺。
type ConversationConfig struct {
	Enabled       bool  `json:"enabled"`
	MaxTurns      int   `json:"maxTurns"`
	IdleSeconds   int   `json:"idleSeconds"`
	MaxAgeSeconds int   `json:"maxAgeSeconds"`
	ContextTokens int64 `json:"contextTokens"`
	TotalTokens   int64 `json:"totalTokens"`
}

func DefaultConversationConfig() ConversationConfig {
	config := ConversationConfig{Enabled: true, MaxTurns: 10, IdleSeconds: 600, MaxAgeSeconds: 1800, ContextTokens: 12000, TotalTokens: 60000}
	return config
}

func (c *ConversationConfig) Validate() error {
	if c == nil || c.MaxTurns < 1 || c.MaxTurns > 50 || c.IdleSeconds < 60 || c.IdleSeconds > 3600 || c.MaxAgeSeconds < 60 || c.MaxAgeSeconds > 14400 || c.ContextTokens < 6000 || c.ContextTokens > 64000 || c.TotalTokens < 12000 || c.TotalTokens > 1000000 {
		return Fail("INPUT", "会话轮数1–50、空闲60–3600秒、总寿命60–14400秒、上下文6000–64000 Token、累计12000–1000000 Token")
	}
	return nil
}

// ConversationScope 由已核验的业务入口构造，绝不从模型输出或聊天自称取得。
type ConversationScope struct {
	Platform  string
	Account   string
	Peer      string
	Channel   string
	Revision  string
	RequestID string
}

// ConversationHandoff 仅保存带来源的有限原文交接，不把推测升格为事实。
type ConversationHandoff struct {
	Facts  []RecalledMemory `json:"facts"`
	Recent []string         `json:"recent"`
}

type ConversationWindowStatus struct {
	ID      string
	Turns   int
	Tokens  int64
	Pending bool
}

type ConversationStatus struct {
	Config  ConversationConfig
	Windows []ConversationWindowStatus
}

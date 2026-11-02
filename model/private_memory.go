package model

// PrivateMemorySource 始终保留平台消息来源，不把用户陈述冒充亲眼观察。
type PrivateMemorySource struct {
	MessageKey string `json:"messageKey"`
	Sequence   int64  `json:"sequence"`
	OccurredAt string `json:"occurredAt"`
	RecordedAt string `json:"recordedAt"`
	Withdrawn  bool   `json:"withdrawn"`
}
type PrivateMemory struct {
	ID      string                `json:"id"`
	Quote   string                `json:"quote"`
	Kind    string                `json:"kind"`
	Sources []PrivateMemorySource `json:"sources"`
}
type PrivateMemoryState struct {
	Platform    string          `json:"platform"`
	Account     string          `json:"account"`
	Peer        string          `json:"peer"`
	Channel     string          `json:"channel"`
	Generation  string          `json:"generation"`
	Initialized bool            `json:"initialized"`
	Cursor      int64           `json:"cursor"`
	Since       int64           `json:"since"`
	Records     []PrivateMemory `json:"records"`
}
type PrivateMemoryQuery struct {
	Query          *string
	Page           int
	PageSize       int
	BeforeSequence int64
}
type PrivateMemoryPage struct {
	Enabled  bool
	Account  string
	Peer     string
	Items    []PrivateMemory
	Total    int
	Page     int
	PageSize int
}
type RecalledMemory struct {
	Quote      string `json:"userStatement"`
	OccurredAt string `json:"occurredAt"`
	MessageKey string `json:"sourceMessage"`
}

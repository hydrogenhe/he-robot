package model

// Operation 是跨平台业务流水，不保存请求正文或凭证。
type Operation struct {
	NotificationStatus string            `json:"notificationStatus,omitempty"`
	ID                 string            `json:"id"`
	At                 string            `json:"at"`
	Platform           string            `json:"platform"`
	Account            string            `json:"account"`
	Action             string            `json:"action"`
	Source             string            `json:"source"`
	Status             string            `json:"status"`
	DurationMS         int64             `json:"durationMs"`
	Timings            *OperationTimings `json:"timings,omitempty"`
	Reference          string            `json:"reference,omitempty"`
	ErrorCode          string            `json:"errorCode,omitempty"`
	UsageOwner         string            `json:"usageOwner,omitempty"`
	UsageThread        string            `json:"usageThread,omitempty"`
	UsageTurn          string            `json:"usageTurn,omitempty"`
	Usage              []TokenUsage      `json:"usage"`
}

// OperationTimings 保存私信回复各阶段的实测毫秒数；nil表示该阶段未执行。
// GenerationMS包含上下文准备、宿主生成及本地用量/成长落盘；AccountCheckMS包含只读重试等待。
type OperationTimings struct {
	GenerationMS    *int64 `json:"generationMs"`
	AccountCheckMS  *int64 `json:"accountCheckMs"`
	SendMS          *int64 `json:"sendMs"`
	AccountAttempts int    `json:"accountAttempts"`
}

// TokenUsage 只接受真实调用响应或宿主的上报；缓存输入是输入子集。
type TokenUsage struct {
	RequestID         string   `json:"requestId"`
	Provider          string   `json:"provider"`
	Model             string   `json:"model"`
	Input             int64    `json:"input"`
	CachedInput       *int64   `json:"cachedInput"`
	Output            int64    `json:"output"`
	ReasoningOutput   *int64   `json:"reasoningOutput"`
	Source            string   `json:"source"`
	ThreadID          string   `json:"threadId,omitempty"`
	TurnID            string   `json:"turnId,omitempty"`
	Scope             string   `json:"scope,omitempty"`
	RelatedOperations []string `json:"relatedOperations,omitempty"`
}
type OperationStart struct {
	Platform string
	Action   string
	Source   string
}
type UsageReport struct {
	OperationID string
	Usage       TokenUsage
}
type OperationQuery struct {
	ActivitiesOnly *bool
	Platform       *string
	Account        *string
	Action         *string
	Status         *string
	From           *string
	To             *string
	HasUsage       *bool
	Page           int
	PageSize       int
}
type OperationPage struct {
	CollectionStatus string
	Items            []Operation
	Total            int
	Reported         int
	Input            int64
	Output           int64
	CachedInput      int64
	CachedReports    int
	Page             int
	PageSize         int
	Platforms        []string
	Actions          []string
}
type OperationFinish struct {
	Status    string
	Reference string
	Account   string
	ErrorCode string
	Timings   *OperationTimings
}

// ActivityPreview 是认证管理页面即时展示，不属于持久化的通用流水。
type ActivityPreview struct {
	Title string
	Text  string
}

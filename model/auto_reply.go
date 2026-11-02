package model

// AutoReplyConfig 是用户授权的自动回复范围，账号用UID绑定而非昵称。
type AutoReplyConfig struct {
	Source            string   `json:"source,omitempty"`
	Enabled           bool     `json:"enabled"`
	AccountUID        string   `json:"accountUid"`
	PriorityUID       string   `json:"priorityUid"`
	Videos            []string `json:"videos"`
	IntervalSeconds   int      `json:"intervalSeconds"`
	OtherDailyLimit   int      `json:"otherDailyLimit"`
	PerUserDailyLimit int      `json:"perUserDailyLimit"`
}
type AutoEntryStatus int

const (
	// AutoQueued 待发送，仍须经过额度和目标复核。
	AutoQueued AutoEntryStatus = 1
	// AutoSkipped 规则跳过，不调用模型或发送。
	AutoSkipped AutoEntryStatus = 2
	// AutoProcessing 已认领，重启后不自动重放。
	AutoProcessing AutoEntryStatus = 3
	// AutoCompleted 已执行共享发送流程。
	AutoCompleted AutoEntryStatus = 4
	// AutoInterrupted 执行结果不明，必须人工处理。
	AutoInterrupted AutoEntryStatus = 5
)

type AutoEntry struct {
	Key     string          `json:"key"`
	At      string          `json:"at"`
	BVID    string          `json:"bvid"`
	Comment Comment         `json:"comment"`
	Status  AutoEntryStatus `json:"status"`
	Reason  string          `json:"reason"`
	Reply   string          `json:"reply,omitempty"`
	DraftID string          `json:"draftId,omitempty"`
	Result  string          `json:"result,omitempty"`
}
type AutoVideoState struct {
	Video       Video          `json:"video"`
	Since       int64          `json:"since"`
	Initialized bool           `json:"initialized"`
	RootCounts  map[string]int `json:"rootCounts"`
}
type AutoReplyState struct {
	Paused   bool                      `json:"paused"`
	Reason   string                    `json:"reason"`
	LastAt   string                    `json:"lastAt"`
	NextAt   string                    `json:"nextAt"`
	Revision int64                     `json:"revision"`
	Videos   map[string]AutoVideoState `json:"videos"`
	Entries  map[string]AutoEntry      `json:"entries"`
}
type AutoReplyOverview struct {
	SourceReady bool
	Config      AutoReplyConfig
	Paused      bool
	Reason      string
	LastAt      string
	NextAt      string
	Entries     []AutoEntry
	OtherUsed   int
	UserUsed    map[string]int
}
type AutoDecision struct {
	Allowed bool
	Reply   string // 预览兼容字段，筛选器不生成正文。
	Reason  string
}

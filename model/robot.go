package model

type RobotControlInput struct{ Action string }

type RobotState struct {
	Status  string `json:"status"`
	Enabled bool   `json:"enabled"`
}
type PrivateConfig struct {
	MemoryEnabled    bool   `json:"memoryEnabled"`
	MemoryGeneration string `json:"memoryGeneration,omitempty"`
	Enabled          bool   `json:"enabled"`
	AllowSend        bool   `json:"allowSend"`
	AutoReply        bool   `json:"autoReply"`
	SenderUID        string `json:"senderUid"`
	PeerUID          string `json:"peerUid"`
	IntervalSeconds  int    `json:"intervalSeconds"`
}
type PrivateSettings struct {
	MemoryEnabled   *bool
	Enabled         bool
	AllowSend       bool
	AutoReply       bool
	IntervalSeconds int
}
type PrivateMessage struct {
	Image     *ImageAttachment `json:"image,omitempty"`
	Key       string           `json:"key"`
	Seq       int64            `json:"seq"`
	Sender    string           `json:"sender"`
	Receiver  string           `json:"receiver"`
	Text      string           `json:"text"`
	Timestamp int64            `json:"timestamp"`
	Withdrawn bool             `json:"withdrawn"`
}
type PrivatePage struct {
	Messages []PrivateMessage
	HasMore  bool
}
type PrivateSendInput struct {
	StickerID string
	GroupID   string
	Part      int
	RequestID string
	Text      string
	AutoKey   string
}
type PrivateOutbox struct {
	Proactive  bool   `json:"proactive,omitempty"`
	GroupHash  string `json:"groupHash,omitempty"`
	StickerID  string `json:"stickerId,omitempty"`
	GroupID    string `json:"groupId,omitempty"`
	Part       int    `json:"part,omitempty"`
	RequestID  string `json:"requestId"`
	At         string `json:"at"`
	Text       string `json:"text"`
	AutoKey    string `json:"autoKey,omitempty"`
	Status     string `json:"status"`
	MessageKey string `json:"messageKey,omitempty"`
	ErrorCode  string `json:"errorCode,omitempty"`
}
type PrivateState struct {
	LastTriggerKind string           `json:"lastTriggerKind,omitempty"`
	LastTriggerKey  string           `json:"lastTriggerKey,omitempty"`
	LastTriggerAt   string           `json:"lastTriggerAt,omitempty"`
	ReadPaused      bool             `json:"readPaused"`
	ReadFailures    int              `json:"readFailures"`
	LastErrorCode   string           `json:"lastErrorCode,omitempty"`
	LastErrorStage  string           `json:"lastErrorStage,omitempty"`
	LastErrorAt     string           `json:"lastErrorAt,omitempty"`
	Initialized     bool             `json:"initialized"`
	Cursor          int64            `json:"cursor"`
	Since           int64            `json:"since"`
	Paused          bool             `json:"paused"`
	Reason          string           `json:"reason"`
	LastAt          string           `json:"lastAt"`
	NextAt          string           `json:"nextAt"`
	Processing      string           `json:"processing"`
	Revision        int64            `json:"revision"`
	Inbox           []PrivateMessage `json:"inbox"`
	Outbox          []PrivateOutbox  `json:"outbox"`
}
type PrivateOverview struct {
	Push   PrivatePushStatus
	Config PrivateConfig
	State  PrivateState
}

package model

import "context"

// ProactiveConfig 仅控制本机主动任务，发送仍须通过绑定私信权限。
type ProactiveConfig struct {
	Enabled          bool   `json:"enabled"`
	VideoReflections bool   `json:"videoReflections"`
	DiaryDelivery    bool   `json:"diaryDelivery"`
	Greetings        bool   `json:"greetings"`
	RandomMin        int    `json:"randomMin"`
	RandomMax        int    `json:"randomMax"`
	VideoDailyLimit  int    `json:"videoDailyLimit"`
	SenderUID        string `json:"senderUid"`
	PeerUID          string `json:"peerUid"`
}

// ProactiveJob 的请求ID及正文先持久化，发送未知时不自动重发。
type ProactiveJob struct {
	ParentOperation                                                     bool
	Key, Kind, DueAt, ExpiresAt, Status, OperationID, ErrorCode, SentAt string
	SenderUID, PeerUID                                                  string
	Parts                                                               []string
	RequestIDs                                                          []string
	Part                                                                int
}
type ProactiveState struct {
	Day  string
	Jobs []ProactiveJob
}
type ProactiveOverview struct {
	Config ProactiveConfig
	State  ProactiveState
}

// VideoReadInput 只接受平台适配器验证过的视频及受控人设。
type VideoReadInput struct {
	Video          Video
	Persona, State string
	MaxMinutes     int
}
type VideoMaterialReader interface {
	ReadVideo(context.Context, *VideoReadInput) (*MaterialExperienceInput, error)
}

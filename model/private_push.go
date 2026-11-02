package model

import "context"

// PrivatePushEvent 仅携带连接状态或已授权会话唤醒，不携带任何私聊正文。
type PrivatePushEvent struct {
	GenericHint bool
	Observed    bool
	Path        string
	Metadata    string
	Key         string
	Connected   bool
	Wake        bool
}
type PrivatePushSource interface {
	WatchPrivate(context.Context, string, string, func(*PrivatePushEvent)) error
}

type PrivatePushStatus struct {
	Hints              int
	Verified           bool
	Notifications      int
	Matched            int
	LastPath           string
	LastMetadata       string
	LastNotificationAt string
	Connected          bool
	Mode               string
	ErrorCode          string
}

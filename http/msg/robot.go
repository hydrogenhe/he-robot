package msg

import "gpt-robot/model"

type ShutdownRequest struct {
	Confirmed *bool `json:"confirmed"`
}

type RobotRequest struct {
	Action  string `json:"action,omitempty"`
	Enabled *bool  `json:"enabled"`
}
type PrivateConfigRequest struct {
	MemoryEnabled   *bool `json:"memoryEnabled"`
	Enabled         *bool `json:"enabled"`
	AllowSend       *bool `json:"allowSend"`
	AutoReply       *bool `json:"autoReply"`
	IntervalSeconds int   `json:"intervalSeconds"`
}
type PrivateSendRequest struct {
	RequestID string `json:"requestId"`
	Text      string `json:"text"`
}

func Robot(state *model.RobotState) any {
	result := map[string]any{"enabled": state.Enabled, "status": state.Status}
	return result
}
func PrivateOutbox(row *model.PrivateOutbox) any {
	result := map[string]any{"requestId": row.RequestID, "at": row.At, "text": row.Text, "automatic": row.AutoKey != "", "proactive": row.Proactive, "status": row.Status, "messageKey": row.MessageKey, "errorCode": row.ErrorCode, "groupId": row.GroupID, "part": row.Part, "sticker": row.StickerID != ""}
	return result
}
func PrivateOverview(page *model.PrivateOverview) any {
	config, state := page.Config, page.State
	inbox, outbox := []any{}, []any{}
	for _, row := range state.Inbox {
		text := row.Text
		if text == "" && row.Image != nil {
			text = "[图片 / 表情包]"
		}
		inbox = append(inbox, map[string]any{"key": row.Key, "sender": row.Sender, "receiver": row.Receiver, "text": text, "timestamp": row.Timestamp, "withdrawn": row.Withdrawn})
	}
	for _, row := range state.Outbox {
		outbox = append(outbox, PrivateOutbox(&row))
	}
	result := map[string]any{
		"push":            map[string]any{"connected": page.Push.Connected, "verified": page.Push.Verified, "notifications": page.Push.Notifications, "matched": page.Push.Matched, "hints": page.Push.Hints, "lastPath": page.Push.LastPath, "lastMetadata": page.Push.LastMetadata, "lastNotificationAt": page.Push.LastNotificationAt, "mode": page.Push.Mode, "errorCode": page.Push.ErrorCode},
		"config":          map[string]any{"enabled": config.Enabled, "allowSend": config.AllowSend, "autoReply": config.AutoReply, "memoryEnabled": config.MemoryEnabled, "senderUid": config.SenderUID, "peerUid": config.PeerUID, "intervalSeconds": config.IntervalSeconds},
		"lastTriggerKind": state.LastTriggerKind, "lastTriggerKey": state.LastTriggerKey, "lastTriggerAt": state.LastTriggerAt,
		"paused": state.Paused, "reason": state.Reason, "readPaused": state.ReadPaused, "readFailures": state.ReadFailures, "lastErrorCode": state.LastErrorCode, "lastErrorStage": state.LastErrorStage, "lastErrorAt": state.LastErrorAt, "initialized": state.Initialized, "lastAt": state.LastAt, "nextAt": state.NextAt, "processing": state.Processing != "", "inbox": inbox, "outbox": outbox,
	}
	return result
}

package msg

import "gpt-robot/model"

type ConversationRequest struct {
	Enabled       *bool  `json:"enabled"`
	MaxTurns      *int   `json:"maxTurns"`
	IdleSeconds   *int   `json:"idleSeconds"`
	MaxAgeSeconds *int   `json:"maxAgeSeconds"`
	ContextTokens *int64 `json:"contextTokens"`
	TotalTokens   *int64 `json:"totalTokens"`
}

func Conversation(status *model.ConversationStatus) any {
	config := status.Config
	windows := []map[string]any{}
	for _, w := range status.Windows {
		windows = append(windows, map[string]any{"id": w.ID, "turns": w.Turns, "tokens": w.Tokens, "pending": w.Pending})
	}
	result := map[string]any{"config": map[string]any{"enabled": config.Enabled, "maxTurns": config.MaxTurns, "idleSeconds": config.IdleSeconds, "maxAgeSeconds": config.MaxAgeSeconds, "contextTokens": config.ContextTokens, "totalTokens": config.TotalTokens}, "windows": windows}
	return result
}

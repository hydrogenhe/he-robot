package handler

import (
	"context"
	"encoding/json"
	"gpt-robot/http/middleware"
	"gpt-robot/http/msg"
	"gpt-robot/model"
)

func (h *Actions) conversationAction(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	if name == "conversation-status" {
		var p msg.EmptyRequest
		if err := middleware.Decode(raw, &p); err != nil {
			return nil, err
		}
		status, err := h.service.ConversationStatus(ctx)
		if err != nil {
			return nil, err
		}
		return msg.Conversation(status), nil
	}
	var p msg.ConversationRequest
	if err := middleware.Decode(raw, &p); err != nil {
		return nil, err
	}
	if p.Enabled == nil || p.MaxTurns == nil || p.IdleSeconds == nil || p.MaxAgeSeconds == nil || p.ContextTokens == nil || p.TotalTokens == nil {
		return nil, model.Fail("INPUT", "请完整提供会话开关和轮换阈值")
	}
	input := model.ConversationConfig{Enabled: *p.Enabled, MaxTurns: *p.MaxTurns, IdleSeconds: *p.IdleSeconds, MaxAgeSeconds: *p.MaxAgeSeconds, ContextTokens: *p.ContextTokens, TotalTokens: *p.TotalTokens}
	status, err := h.service.SaveConversationConfig(ctx, &input)
	if err != nil {
		return nil, err
	}
	return msg.Conversation(status), nil
}

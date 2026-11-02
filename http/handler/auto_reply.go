package handler

import (
	"context"
	"encoding/json"
	"gpt-robot/core"
	"gpt-robot/http/middleware"
	"gpt-robot/http/msg"
	"gpt-robot/model"
)

func (h *Actions) autoAction(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	switch name {
	case "auto-config":
		var p msg.AutoConfigRequest
		if err := middleware.Decode(raw, &p); err != nil {
			return nil, err
		}
		if p.Enabled == nil {
			return nil, model.Fail("INPUT", "缺少自动回复开关")
		}
		input := model.AutoReplyConfig{Source: p.Source, Enabled: *p.Enabled, AccountUID: p.AccountUID, PriorityUID: p.PriorityUID, Videos: p.Videos, IntervalSeconds: p.IntervalSeconds, OtherDailyLimit: p.OtherDailyLimit, PerUserDailyLimit: p.PerUserDailyLimit}
		result, err := h.service.SaveAutoConfig(ctx, &input)
		if err != nil {
			return nil, err
		}
		return msg.AutoOverview(result), nil
	case "auto-preview":
		var p msg.AutoPreviewRequest
		if err := middleware.Decode(raw, &p); err != nil {
			return nil, err
		}
		if !model.Text(p.Text, 1000) {
			return nil, model.Fail("INPUT", "请输入最多1000字的预览内容")
		}
		result := core.EvaluateAutoReply(p.Text)
		return map[string]any{"allowed": result.Allowed, "reply": result.Reply, "reason": result.Reason, "modelTokens": 0}, nil
	default:
		var p msg.EmptyRequest
		if err := middleware.Decode(raw, &p); err != nil {
			return nil, err
		}
		if name == "auto-scan" {
			if err := h.service.AutoTick(ctx); err != nil {
				return nil, err
			}
		}
		result, err := h.service.AutoOverview(ctx)
		if err != nil {
			return nil, err
		}
		return msg.AutoOverview(result), nil
	}
}

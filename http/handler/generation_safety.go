package handler

import (
	"context"
	"encoding/json"
	"gpt-robot/http/middleware"
	"gpt-robot/http/msg"
	"gpt-robot/model"
)

func (h *Actions) generationSafetyAction(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	if name == "generation-safety-status" {
		var p msg.EmptyRequest
		if err := middleware.Decode(raw, &p); err != nil {
			return nil, err
		}
		status, err := h.service.GenerationSafety(ctx)
		if err != nil {
			return nil, err
		}
		return msg.GenerationSafety(status), nil
	}
	var p msg.GenerationSafetyRequest
	if err := middleware.Decode(raw, &p); err != nil {
		return nil, err
	}
	if p.OwnerDailyCalls == nil || p.OtherDailyCalls == nil || p.OwnerDailyTokens == nil || p.OtherDailyTokens == nil {
		return nil, model.Fail("INPUT", "请提供主人和其他用户的调用与Token上限")
	}
	config := model.GenerationSafetyConfig{OwnerDailyCalls: *p.OwnerDailyCalls, OtherDailyCalls: *p.OtherDailyCalls, OwnerDailyTokens: *p.OwnerDailyTokens, OtherDailyTokens: *p.OtherDailyTokens}
	status, err := h.service.SaveGenerationSafety(ctx, &config)
	if err != nil {
		return nil, err
	}
	return msg.GenerationSafety(status), nil
}

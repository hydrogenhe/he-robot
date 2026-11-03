package handler

import (
	"context"
	"encoding/json"
	"gpt-robot/http/middleware"
	"gpt-robot/http/msg"
	"gpt-robot/model"
)

func (h *Actions) proactiveAction(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	if name == "proactive-status" {
		var request msg.EmptyRequest
		if err := middleware.Decode(raw, &request); err != nil {
			return nil, err
		}
		r, err := h.service.ProactiveStatus(ctx)
		if err != nil {
			return nil, err
		}
		return msg.Proactive(r), nil
	}
	var req msg.ProactiveRequest
	if err := middleware.Decode(raw, &req); err != nil {
		return nil, err
	}
	if req.Enabled == nil || req.VideoReflections == nil || req.DiaryDelivery == nil || req.Greetings == nil || req.RandomMin == nil || req.RandomMax == nil || req.VideoDailyLimit == nil {
		return nil, model.Fail("INPUT", "请明确主动任务开关与每日额度")
	}
	input := model.ProactiveConfig{Enabled: *req.Enabled, VideoReflections: *req.VideoReflections, DiaryDelivery: *req.DiaryDelivery, Greetings: *req.Greetings, RandomMin: *req.RandomMin, RandomMax: *req.RandomMax, VideoDailyLimit: *req.VideoDailyLimit}
	r, err := h.service.SaveProactiveConfig(ctx, &input)
	if err != nil {
		return nil, err
	}
	return msg.Proactive(r), nil
}

package handler

import (
	"context"
	"encoding/json"
	"gpt-robot/http/middleware"
	"gpt-robot/http/msg"
	"gpt-robot/model"
)

func (h *Actions) browsingAction(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	if name == "browsing-config" {
		var p msg.BrowsingRequest
		if err := middleware.Decode(raw, &p); err != nil {
			return nil, err
		}
		if p.Enabled == nil {
			return nil, model.Fail("INPUT", "缺少浏览开关")
		}
		config := model.BrowsingConfig{Enabled: *p.Enabled, Mode: p.Mode, Keywords: p.Keywords, Excluded: p.Excluded, IntervalSeconds: p.IntervalSeconds, DailyLimit: p.DailyLimit}
		config.MaxDurationMinutes = 10
		if p.MaxDurationMinutes != nil {
			config.MaxDurationMinutes = *p.MaxDurationMinutes
		}
		page, err := h.service.SaveBrowsing(ctx, &config)
		if err != nil {
			return nil, err
		}
		return msg.Browsing(page), nil
	}
	var p msg.EmptyRequest
	if err := middleware.Decode(raw, &p); err != nil {
		return nil, err
	}
	if name == "browsing-read" {
		if err := h.service.BrowseNow(ctx); err != nil {
			return nil, err
		}
	}
	page, err := h.service.BrowsingOverview(ctx)
	if err != nil {
		return nil, err
	}
	return msg.Browsing(page), nil
}

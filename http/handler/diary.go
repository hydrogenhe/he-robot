package handler

import (
	"context"
	"encoding/json"
	"gpt-robot/http/middleware"
	"gpt-robot/http/msg"
	"gpt-robot/model"
)

func (h *Actions) diaryAction(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	switch name {
	case "diary-config":
		var request msg.DiaryConfigRequest
		if err := middleware.Decode(raw, &request); err != nil {
			return nil, err
		}
		if request.Enabled == nil {
			return nil, model.Fail("INPUT", "请选择是否自动写日记")
		}
		input := model.DiarySettings{Topics: request.Topics, OwnerWeight: request.OwnerWeight, InterestWeight: request.InterestWeight, Enabled: *request.Enabled, Testing: request.Testing}
		result, err := h.service.SaveDiaryConfig(ctx, &input)
		return msg.Diaries(result), err
	case "diary-generate":
		var request msg.DiaryGenerateRequest
		if err := middleware.Decode(raw, &request); err != nil {
			return nil, err
		}
		input := model.DiaryGenerateInput{Retry: request.Retry}
		result, err := h.service.GenerateDiary(ctx, &input)
		return msg.Diary(result), err
	default:
		var request msg.EmptyRequest
		if err := middleware.Decode(raw, &request); err != nil {
			return nil, err
		}
		result, err := h.service.DiaryOverview(ctx)
		return msg.Diaries(result), err
	}
}

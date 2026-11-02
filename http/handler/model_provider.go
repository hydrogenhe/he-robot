package handler

import (
	"context"
	"encoding/json"
	"gpt-robot/http/middleware"
	"gpt-robot/http/msg"
	"gpt-robot/model"
)

func (h *Actions) modelProviderAction(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	if name == "model-list" {
		var request msg.EmptyRequest
		if err := middleware.Decode(raw, &request); err != nil {
			return nil, err
		}
		choices, err := h.service.CodexModels(ctx)
		if err != nil {
			return nil, err
		}
		items := []any{}
		for _, choice := range choices {
			items = append(items, map[string]string{"id": choice.ID, "name": choice.Name})
		}
		return map[string]any{"models": items}, nil
	}
	if name == "model-connect" {
		var request msg.HarnessConnectRequest
		if err := middleware.Decode(raw, &request); err != nil {
			return nil, err
		}
		input := model.HarnessConnectInput{Provider: request.Provider, CodexModel: request.CodexModel, DeepSeekModel: request.DeepSeekModel, CodexEffort: request.CodexEffort, HarnessEffort: request.HarnessEffort}
		status, err := h.service.ConnectModelHarness(ctx, &input)
		if err != nil {
			return nil, err
		}
		return msg.ModelProvider(status), nil
	}
	if name == "model-status" {
		var request msg.EmptyRequest
		if err := middleware.Decode(raw, &request); err != nil {
			return nil, err
		}
		status, err := h.service.ModelProviderStatus(ctx)
		if err != nil {
			return nil, err
		}
		return msg.ModelProvider(status), nil
	}
	return nil, model.Fail("INPUT", "不支持的模型配置操作")
}

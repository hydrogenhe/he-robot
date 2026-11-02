package handler

import (
	"context"
	"encoding/json"
	"gpt-robot/http/middleware"
	"gpt-robot/http/msg"
	"gpt-robot/model"
)

func (h *Actions) privateMemoryAction(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	if name == "private-memory-forget" {
		var request msg.IDRequest
		if err := middleware.Decode(raw, &request); err != nil {
			return nil, err
		}
		if err := h.service.ForgetPrivateMemory(ctx, request.ID); err != nil {
			return nil, err
		}
		return map[string]any{"forgotten": true}, nil
	}
	var request msg.PrivateMemoryQueryRequest
	if err := middleware.Decode(raw, &request); err != nil {
		return nil, err
	}
	input := model.PrivateMemoryQuery{Query: request.Query}
	if request.Page != nil {
		if *request.Page < 1 {
			return nil, model.Fail("INPUT", "页码至少为1")
		}
		input.Page = *request.Page
	}
	if request.PageSize != nil {
		if *request.PageSize < 1 {
			return nil, model.Fail("INPUT", "每页数量至少为1")
		}
		input.PageSize = *request.PageSize
	}
	result, err := h.service.PrivateMemories(ctx, &input)
	if err != nil {
		return nil, err
	}
	return msg.PrivateMemories(result), nil
}

package handler

import (
	"context"
	"encoding/json"
	"gpt-robot/http/middleware"
	"gpt-robot/http/msg"
	"gpt-robot/model"
)

func (h *Actions) robotAction(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	switch name {
	case "shutdown":
		var request msg.ShutdownRequest
		if err := middleware.Decode(raw, &request); err != nil {
			return nil, err
		}
		if request.Confirmed == nil || !*request.Confirmed {
			return nil, model.Fail("INPUT", "请确认彻底关闭本机服务")
		}
		stop := model.RobotControlInput{Action: "stop"}
		if _, err := h.service.ControlRobot(ctx, &stop); err != nil {
			return nil, err
		}
		return map[string]any{"shuttingDown": true}, nil
	case "robot-control":
		var request msg.RobotRequest
		if err := middleware.Decode(raw, &request); err != nil {
			return nil, err
		}
		if (request.Enabled == nil) == (request.Action == "") {
			return nil, model.Fail("INPUT", "请只提供action或旧版enabled字段之一")
		}
		var state *model.RobotState
		var err error
		if request.Action != "" {
			input := model.RobotControlInput{Action: request.Action}
			state, err = h.service.ControlRobot(ctx, &input)
		} else {
			state, err = h.service.SetRobot(ctx, *request.Enabled)
		}
		if err != nil {
			return nil, err
		}
		return msg.Robot(state), nil
	case "private-config":
		var request msg.PrivateConfigRequest
		if err := middleware.Decode(raw, &request); err != nil {
			return nil, err
		}
		if request.Enabled == nil || request.AllowSend == nil || request.AutoReply == nil {
			return nil, model.Fail("INPUT", "请明确私信、发送和自动回复开关")
		}
		input := model.PrivateSettings{MemoryEnabled: request.MemoryEnabled, Enabled: *request.Enabled, AllowSend: *request.AllowSend, AutoReply: *request.AutoReply, IntervalSeconds: request.IntervalSeconds}
		state, err := h.service.SavePrivateConfig(ctx, &input)
		if err != nil {
			return nil, err
		}
		return msg.PrivateOverview(state), nil
	case "private-send":
		var request msg.PrivateSendRequest
		if err := middleware.Decode(raw, &request); err != nil {
			return nil, err
		}
		input := model.PrivateSendInput{RequestID: request.RequestID, Text: request.Text}
		result, err := h.service.SendPrivate(ctx, &input)
		if err != nil {
			return nil, err
		}
		return msg.PrivateOutbox(result), nil
	default:
		var request msg.EmptyRequest
		if err := middleware.Decode(raw, &request); err != nil {
			return nil, err
		}
		if name == "development-status" {
			page, err := h.service.DevelopmentStatus(ctx)
			if err != nil {
				return nil, err
			}
			return msg.Development(page), nil
		}
		if name == "character-status" {
			state, err := h.service.CharacterStatus(ctx)
			if err != nil {
				return nil, err
			}
			page, err := h.service.DevelopmentStatus(ctx)
			if err != nil {
				return nil, err
			}
			return msg.CharacterWithDevelopment(state, page), nil
		}
		if name == "robot-status" {
			state, err := h.service.RobotStatus(ctx)
			if err != nil {
				return nil, err
			}
			return msg.Robot(state), nil
		}
		if name == "private-resume" {
			state, err := h.service.ResumePrivateReplies(ctx)
			if err != nil {
				return nil, err
			}
			return msg.PrivateOverview(state), nil
		}
		if name == "private-refresh" {
			state, err := h.service.RefreshPrivate(ctx)
			if err != nil {
				return nil, err
			}
			return msg.PrivateOverview(state), nil
		}
		state, err := h.service.PrivateOverview(ctx)
		if err != nil {
			return nil, err
		}
		return msg.PrivateOverview(state), nil
	}
}

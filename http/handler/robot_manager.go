package handler

import (
	"context"
	"encoding/json"
	"gpt-robot/core"
	"gpt-robot/http/middleware"
	"gpt-robot/http/msg"
	"gpt-robot/model"
	"strings"
	"sync"
)

type selectedRobotKey struct{}

// RobotActions 在同一认证工作台按明确机器人编号路由，不共享各机器人的业务状态。
type RobotActions struct {
	mu      sync.Mutex
	targets map[string]*Actions
	manager *core.RobotManager
	primary ActionsInterface
}
type RobotActionsInterface interface {
	Execute(context.Context, string, json.RawMessage, bool) (any, error)
}

var _ RobotActionsInterface = (*RobotActions)(nil)

func NewRobotActions(manager *core.RobotManager, primary ActionsInterface) *RobotActions {
	return &RobotActions{manager: manager, primary: primary, targets: map[string]*Actions{}}
}
func (h *RobotActions) Execute(ctx context.Context, name string, raw json.RawMessage, admin bool) (any, error) {
	if !admin {
		if strings.HasPrefix(name, "robots-") {
			return nil, model.Fail("DENIED", "MCP不能管理机器人")
		}
		_, request, cancel, err := h.manager.Resolve(ctx, "001")
		if err != nil {
			return nil, err
		}
		defer cancel()
		return h.primary.Execute(request, name, raw, false)
	}
	selected, _ := ctx.Value(selectedRobotKey{}).(string)
	if selected == "" {
		selected = "001"
	}
	if strings.HasPrefix(name, "robots-") {
		return h.manage(ctx, name, raw, selected)
	}
	if name == "robot-control" {
		var req msg.RobotRequest
		if err := middleware.Decode(raw, &req); err != nil {
			return nil, err
		}
		action := req.Action
		if action == "" && req.Enabled != nil {
			action = "pause"
			if *req.Enabled {
				action = "start"
			}
		}
		input := model.RobotManagerInput{ID: selected}
		if err := h.manager.Control(ctx, &input, action); err != nil {
			return nil, err
		}
	}
	service, request, cancel, err := h.manager.Resolve(ctx, selected)
	if err != nil {
		return nil, err
	}
	defer cancel()
	if name == "robot-control" {
		state, err := service.RobotStatus(request)
		if err != nil {
			return nil, err
		}
		return msg.Robot(state), nil
	}
	h.mu.Lock()
	actions := h.targets[selected]
	if actions == nil {
		actions = NewActions(service)
		h.targets[selected] = actions
	}
	h.mu.Unlock()
	return actions.Execute(request, name, raw, true)
}
func (h *RobotActions) manage(ctx context.Context, name string, raw json.RawMessage, selected string) (any, error) {
	var req msg.RobotManagerRequest
	if err := middleware.Decode(raw, &req); err != nil {
		return nil, err
	}
	input := model.RobotManagerInput{ID: req.ID, Name: req.Name, ConfirmID: req.ConfirmID}
	switch name {
	case "robots-list":
	case "robots-create":
		if _, err := h.manager.Create(ctx, &input); err != nil {
			return nil, err
		}
	case "robots-delete":
		if err := h.manager.SoftDelete(ctx, &input, selected); err != nil {
			return nil, err
		}
	case "robots-restore":
		if err := h.manager.Restore(ctx, &input); err != nil {
			return nil, err
		}
	case "robots-start", "robots-stop":
		action := "start"
		if name == "robots-stop" {
			action = "stop"
		}
		if err := h.manager.Control(ctx, &input, action); err != nil {
			return nil, err
		}
	default:
		return nil, model.Fail("NOT_FOUND", "未知机器人管理动作")
	}
	entries, err := h.manager.List(ctx)
	if err != nil {
		return nil, err
	}
	return msg.RobotList(entries, selected), nil
}

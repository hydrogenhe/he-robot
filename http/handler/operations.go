package handler

import (
	"context"
	"encoding/json"
	"gpt-robot/core"
	"gpt-robot/http/middleware"
	"gpt-robot/http/msg"
	"gpt-robot/model"
	"log/slog"
)

type operationTraceKey struct{}

// Execute 在共享入口记录业务操作；统计查询和页面概览不产生新业务流水。
func (h *Actions) Execute(ctx context.Context, name string, raw json.RawMessage, admin bool) (any, error) {
	platform := operationPlatform(name)
	if platform == "" || (!admin && (name == "send" || name == "cancel" || name == "recheck" || name == "qr" || name == "poll" || name == "import-session")) {
		return h.execute(ctx, name, raw, admin)
	}
	source := "mcp"
	if admin {
		source = "web"
	}
	input := model.OperationStart{
		Platform: platform,
		Action:   name,
		Source:   source,
	}
	id, err := h.service.StartOperation(ctx, &input)
	if err != nil {
		return nil, err
	}
	if trace, ok := ctx.Value(operationTraceKey{}).(*string); ok {
		*trace = id
	}
	value, callErr := h.execute(ctx, name, raw, admin)
	finish := model.OperationFinish{Status: "succeeded"}
	if callErr != nil {
		finish.Status = "failed"
		finish.ErrorCode = core.OperationErrorCode(callErr)
	}
	if result, ok := value.(map[string]any); ok {
		for _, key := range []string{"eventId", "id"} {
			if ref, ok := result[key].(string); ok {
				finish.Reference = ref
				break
			}
		}
		if account, ok := result["account"].(string); ok {
			finish.Account = account
		}
		if uid, ok := result["uid"].(string); ok {
			finish.Account = uid
		}
		if name == "send" {
			if status, ok := result["status"].(string); ok {
				finish.Status = status
			}
		}
		result["operationId"] = id
	}
	if err := h.service.FinishOperation(ctx, id, &finish); err != nil {
		slog.Error("业务已执行但流水完成状态保存失败", "operationId", id, "action", name, "error", err)
		// 保留业务结果，不能因统计失败诱导用户再次发送；运行中记录会在重启时标为未知。
		if result, ok := value.(map[string]any); ok {
			result["operationLogWarning"] = "流水完成状态未保存，请勿因此重试业务操作"
		}
	}
	return value, callErr
}
func operationPlatform(action string) string {
	switch action {
	case "context", "search", "note":
		return "local"
	case "private-send", "account", "comments", "draft", "dynamic-draft", "send", "cancel", "recheck", "qr", "poll", "import-session":
		return "bilibili"
	default:
		return ""
	}
}
func (h *Actions) usageAction(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	if name == "operations" {
		var p msg.OperationQueryRequest
		if err := middleware.Decode(raw, &p); err != nil {
			return nil, err
		}
		if (p.Page != nil && *p.Page < 1) || (p.PageSize != nil && *p.PageSize < 1) {
			return nil, model.Fail("INPUT", "页码和页大小至少为1")
		}
		input := model.OperationQuery{
			ActivitiesOnly: p.ActivitiesOnly,
			Platform:       p.Platform,
			Account:        p.Account,
			Action:         p.Action,
			Status:         p.Status,
			From:           p.From,
			To:             p.To,
			HasUsage:       p.HasUsage,
		}
		if p.Page != nil {
			input.Page = *p.Page
		}
		if p.PageSize != nil {
			input.PageSize = *p.PageSize
		}
		result, err := h.service.QueryOperations(ctx, &input)
		if err != nil {
			return nil, err
		}
		previews, err := h.service.OperationPreviews(ctx, result.Items)
		if err != nil {
			return nil, err
		}
		return msg.Operations(result, previews), nil
	}
	var p msg.UsageReportRequest
	if err := middleware.Decode(raw, &p); err != nil {
		return nil, err
	}
	if p.Input == nil || p.Output == nil {
		return nil, model.Fail("INPUT", "必须提供真实输入和输出用量，不能省略后默认记零")
	}
	usage := model.TokenUsage{
		RequestID:       p.RequestID,
		Provider:        p.Provider,
		Model:           p.Model,
		Input:           *p.Input,
		Output:          *p.Output,
		CachedInput:     p.CachedInput,
		ReasoningOutput: p.ReasoningOutput,
	}
	input := model.UsageReport{
		OperationID: p.OperationID,
		Usage:       usage,
	}
	if err := h.service.ReportUsage(ctx, &input); err != nil {
		return nil, err
	}
	return map[string]any{"saved": true}, nil
}

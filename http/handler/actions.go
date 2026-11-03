package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	qrcode "github.com/skip2/go-qrcode"
	"gpt-robot/core"
	"gpt-robot/http/middleware"
	"gpt-robot/http/msg"
	"gpt-robot/model"
)

type ActionsInterface interface {
	Execute(context.Context, string, json.RawMessage, bool) (any, error)
}
type Actions struct{ service core.ServiceInterface }

var _ ActionsInterface = (*Actions)(nil)

func NewActions(service core.ServiceInterface) *Actions { h := &Actions{service: service}; return h }
func (h *Actions) execute(ctx context.Context, name string, raw json.RawMessage, admin bool) (any, error) {
	if !admin {
		switch name {
		case "context", "search", "note", "comments", "draft", "dynamic-draft", "account", "usage-report":
		default:
			return nil, &model.Error{Code: "DENIED", Message: "MCP仅能查询、记录和准备草稿；发送及配置请由用户在网页操作", Status: 403}
		}
	}
	switch name {
	case "proactive-status", "proactive-config":
		return h.proactiveAction(ctx, name, raw)
	case "conversation-status", "conversation-config":
		return h.conversationAction(ctx, name, raw)
	case "browsing-status", "browsing-config", "browsing-read":
		return h.browsingAction(ctx, name, raw)
	case "generation-safety-status", "generation-safety-config":
		return h.generationSafetyAction(ctx, name, raw)
	case "model-status", "model-connect", "model-list":
		return h.modelProviderAction(ctx, name, raw)
	case "diary-status", "diary-config", "diary-generate":
		return h.diaryAction(ctx, name, raw)
	case "private-memories", "private-memory-forget":
		return h.privateMemoryAction(ctx, name, raw)
	case "development-status", "shutdown", "character-status", "robot-status", "robot-control", "private-status", "private-config", "private-refresh", "private-send", "private-resume":
		return h.robotAction(ctx, name, raw)
	case "auto-config", "auto-status", "auto-preview", "auto-scan":
		return h.autoAction(ctx, name, raw)
	case "operations", "usage-report":
		return h.usageAction(ctx, name, raw)
	case "overview", "context", "account", "qr", "poll":
		var p msg.EmptyRequest
		if err := middleware.Decode(raw, &p); err != nil {
			return nil, err
		}
		switch name {
		case "overview":
			v, err := h.service.Overview(ctx)
			if err != nil {
				return nil, err
			}
			return msg.Overview(v), nil
		case "context":
			v, err := h.service.Context(ctx)
			if err != nil {
				return nil, err
			}
			return msg.Context(v), nil
		case "account":
			v, err := h.service.Account(ctx)
			if err != nil {
				return nil, err
			}
			return msg.Account(v), nil
		case "qr":
			url, err := h.service.QR(ctx)
			if err != nil {
				return nil, err
			}
			png, err := qrcode.Encode(url, qrcode.Medium, 260)
			if err != nil {
				return nil, model.Internal(err)
			}
			return map[string]any{"image": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)}, nil
		case "poll":
			v, err := h.service.Poll(ctx)
			if err != nil {
				return nil, err
			}
			r := msg.Account(v.AccountResult).(map[string]any)
			r["code"] = v.Code
			return r, nil
		}
	case "persona":
		var p msg.PersonaRequest
		if err := middleware.Decode(raw, &p); err != nil {
			return nil, err
		}
		if err := h.service.SavePersona(ctx, p.Persona); err != nil {
			return nil, err
		}
		return map[string]any{"saved": true}, nil
	case "permissions":
		var p msg.PermissionsRequest
		if err := middleware.Decode(raw, &p); err != nil {
			return nil, err
		}
		if p.Enabled == nil || p.AllowedVideos == nil || p.AllowedUsers == nil || p.BlockedUsers == nil || p.DailyLimit == nil || p.CooldownSeconds == nil {
			return nil, model.Fail("INPUT", "缺少必需权限字段")
		}
		input := model.Permissions{Enabled: *p.Enabled, AllowedVideos: *p.AllowedVideos, AllowedUsers: *p.AllowedUsers, BlockedUsers: *p.BlockedUsers, DailyLimit: *p.DailyLimit, CooldownSeconds: *p.CooldownSeconds}
		if p.AllowDynamic != nil {
			input.AllowDynamic = *p.AllowDynamic
		}
		v, err := h.service.SavePermissions(ctx, &input)
		if err != nil {
			return nil, err
		}
		return msg.Permissions(v), nil
	case "import-session":
		var p msg.ImportRequest
		if err := middleware.Decode(raw, &p); err != nil {
			return nil, err
		}
		input := model.ImportSessionInput{Cookie: p.Cookie, ExpectedUID: p.ExpectedUID, UserAgent: p.UserAgent}
		v, err := h.service.ImportSession(ctx, &input)
		if err != nil {
			return nil, err
		}
		return msg.Account(v), nil
	case "comments":
		var p msg.CommentsRequest
		if err := middleware.Decode(raw, &p); err != nil {
			return nil, err
		}
		if p.BVID == nil {
			return nil, model.Fail("INPUT", "缺少视频BV号")
		}
		input := model.CommentsInput{BVID: *p.BVID, Page: p.Page, Root: p.Root}
		v, err := h.service.Comments(ctx, &input)
		if err != nil {
			return nil, err
		}
		return msg.Comments(v), nil
	case "draft":
		var p msg.DraftRequest
		if err := middleware.Decode(raw, &p); err != nil {
			return nil, err
		}
		input := model.DraftInput{BVID: p.BVID, Root: p.Root, Parent: p.Parent, Text: p.Text}
		v, err := h.service.Prepare(ctx, &input)
		if err != nil {
			return nil, err
		}
		return msg.Draft(v), nil
	case "dynamic-draft":
		var p msg.TextRequest
		if err := middleware.Decode(raw, &p); err != nil {
			return nil, err
		}
		v, err := h.service.PrepareDynamic(ctx, p.Text)
		if err != nil {
			return nil, err
		}
		return msg.Draft(v), nil
	case "send", "cancel", "recheck":
		var p msg.IDRequest
		if err := middleware.Decode(raw, &p); err != nil {
			return nil, err
		}
		if !model.ValidID(p.ID) {
			return nil, model.Fail("INPUT", "草稿ID格式不正确")
		}
		var v model.Draft
		var err error
		switch name {
		case "send":
			v, err = h.service.Send(ctx, p.ID)
		case "cancel":
			v, err = h.service.Cancel(ctx, p.ID)
		case "recheck":
			v, err = h.service.Recheck(ctx, p.ID)
		}
		if err != nil {
			return nil, err
		}
		return msg.Draft(v), nil
	case "search":
		var p msg.SearchRequest
		if err := middleware.Decode(raw, &p); err != nil {
			return nil, err
		}
		v, err := h.service.Search(ctx, p.Query)
		if err != nil {
			return nil, err
		}
		return msg.Search(v), nil
	case "note":
		var p msg.NoteRequest
		if err := middleware.Decode(raw, &p); err != nil {
			return nil, err
		}
		input := model.NoteInput{Kind: p.Kind, Title: p.Title, Text: p.Text, Sources: p.Sources}
		v, err := h.service.Note(ctx, &input)
		if err != nil {
			return nil, err
		}
		return msg.Note(v), nil
	}
	return nil, &model.Error{Code: "NOT_FOUND", Message: "没有此功能", Status: 404}
}

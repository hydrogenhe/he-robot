package handler

import (
	"context"
	"encoding/json"
	"gpt-robot/http/middleware"
	"gpt-robot/http/msg"
	"gpt-robot/model"
	"gpt-robot/web"
	"net"
	"net/http"
	"strings"
	"time"
)

// webSessionMaxAge 网页登录Cookie保留30天；直接导航会续期，后台重启不更换会话。
const webSessionMaxAge = 30 * 24 * 60 * 60

type WorkbenchInterface interface {
	Session(http.ResponseWriter, *http.Request)
	Action(http.ResponseWriter, *http.Request)
	MCPAction(http.ResponseWriter, *http.Request)
	Asset(http.ResponseWriter, *http.Request)
}
type Workbench struct {
	actions   ActionsInterface
	origin    string
	bootstrap string
	session   string
	csrf      string
	mcp       string
	shutdown  func()
}

var _ WorkbenchInterface = (*Workbench)(nil)

type WorkbenchOptions struct {
	Shutdown  func()
	Origin    string
	Bootstrap string
	Session   string
	CSRF      string
	MCP       string
}

func NewWorkbench(actions ActionsInterface, opt *WorkbenchOptions) *Workbench {
	h := &Workbench{shutdown: opt.Shutdown, actions: actions, origin: opt.Origin, bootstrap: opt.Bootstrap, session: opt.Session, csrf: opt.CSRF, mcp: opt.MCP}
	return h
}
func (h *Workbench) authorized(r *http.Request) bool {
	c, err := r.Cookie("robot_session")
	return err == nil && middleware.Equal(c.Value, h.session)
}
func (h *Workbench) setSessionCookie(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     "robot_session",
		Value:    h.session,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   webSessionMaxAge,
		Expires:  time.Now().Add(time.Duration(webSessionMaxAge) * time.Second),
	}
	http.SetCookie(w, cookie)
}

// directLocalNavigation 只给浏览器用户直接打开的顶层本机页面建立会话；普通请求、iframe、跨站导航不能领凭据。
func directLocalNavigation(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	site := r.Header.Get("Sec-Fetch-Site")
	return ip != nil && ip.IsLoopback() && r.Header.Get("Sec-Fetch-Mode") == "navigate" && r.Header.Get("Sec-Fetch-Dest") == "document" && r.Header.Get("Sec-Fetch-User") == "?1" && (site == "none" || site == "same-origin")
}
func (h *Workbench) Session(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		raw, err := middleware.Body(w, r)
		if err != nil {
			msg.Error(w, err)
			return
		}
		var p struct {
			Key string `json:"key"`
		}
		if err = middleware.Decode(raw, &p); err != nil {
			msg.Error(w, err)
			return
		}
		if !middleware.Equal(p.Key, h.bootstrap) {
			msg.Error(w, &model.Error{Code: "AUTH", Message: "请使用启动链接打开页面", Status: 401})
			return
		}
		h.setSessionCookie(w)
	} else if r.Method != http.MethodGet {
		msg.Error(w, &model.Error{Code: "METHOD", Message: "不支持该方法", Status: 405})
		return
	} else if !h.authorized(r) {
		msg.Error(w, &model.Error{Code: "AUTH", Message: "本机会话尚未建立，请刷新当前页面", Status: 401})
		return
	}
	msg.JSON(w, 200, map[string]any{"csrf": h.csrf})
}
func (h *Workbench) Action(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		msg.Error(w, &model.Error{Code: "METHOD", Message: "需要POST请求", Status: 405})
		return
	}
	if !h.authorized(r) || !middleware.Equal(r.Header.Get("X-CSRF-Token"), h.csrf) {
		msg.Error(w, &model.Error{Code: "AUTH", Message: "网页会话需要重新连接，请刷新页面", Status: 401})
		return
	}
	if r.Header.Get("Origin") != h.origin {
		msg.Error(w, &model.Error{Code: "ORIGIN", Message: "网页请求必须来自本地页面", Status: 403})
		return
	}
	raw, err := middleware.Body(w, r)
	if err != nil {
		msg.Error(w, err)
		return
	}
	action := strings.TrimPrefix(r.URL.Path, "/api/")
	if action == "shutdown" && h.shutdown == nil {
		msg.Error(w, model.Fail("UNAVAILABLE", "此实例未提供服务关闭入口"))
		return
	}
	var operationID string
	ctx := context.WithValue(r.Context(), operationTraceKey{}, &operationID)
	value, err := h.actions.Execute(ctx, action, raw, true)
	if operationID != "" {
		w.Header().Set("X-Operation-ID", operationID)
	}
	if err != nil {
		msg.Error(w, err)
		return
	}
	msg.JSON(w, 200, value)
	if action == "shutdown" {
		// 先交付结果，再通知生命周期协调器正常退出；MCP没有此入口。
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		h.shutdown()
	}
}
func (h *Workbench) MCPAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		msg.Error(w, &model.Error{Code: "METHOD", Message: "需要POST请求", Status: 405})
		return
	}
	if !middleware.Equal(r.Header.Get("Authorization"), "Bearer "+h.mcp) {
		msg.Error(w, &model.Error{Code: "AUTH", Message: "MCP连接凭据失效，请重启连接", Status: 401})
		return
	}
	raw, err := middleware.Body(w, r)
	if err != nil {
		msg.Error(w, err)
		return
	}
	var p struct {
		Action string          `json:"action"`
		Input  json.RawMessage `json:"input"`
	}
	if err = middleware.Decode(raw, &p); err != nil {
		msg.Error(w, err)
		return
	}
	if len(p.Input) == 0 {
		p.Input = json.RawMessage(`{}`)
	}
	var operationID string
	ctx := context.WithValue(r.Context(), operationTraceKey{}, &operationID)
	value, err := h.actions.Execute(ctx, p.Action, p.Input, false)
	if operationID != "" {
		w.Header().Set("X-Operation-ID", operationID)
	}
	if err != nil {
		msg.Error(w, err)
		return
	}
	msg.JSON(w, 200, value)
}
func (h *Workbench) Asset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	name := ""
	contentType := ""
	switch r.URL.Path {
	case "/":
		if directLocalNavigation(r) {
			h.setSessionCookie(w)
		}
		name = "index.html"
		contentType = "text/html"
	case "/app.js":
		name = "app.js"
		contentType = "text/javascript"
	case "/style.css":
		name = "style.css"
		contentType = "text/css"
	case "/favicon.ico":
		w.WriteHeader(204)
		return
	default:
		http.NotFound(w, r)
		return
	}
	b, err := web.Files.ReadFile(name)
	if err != nil {
		msg.Error(w, model.Internal(err))
		return
	}
	w.Header().Set("Content-Type", contentType+"; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}

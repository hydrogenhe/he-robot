package httpinit

import (
	"gpt-robot/http/handler"
	"gpt-robot/http/middleware"
	"gpt-robot/http/msg"
	"net/http"
)

func RegisterRoutes(h handler.WorkbenchInterface, origin string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/session", h.Session)
	mux.HandleFunc("/api/", h.Action)
	mux.HandleFunc("/mcp-action", h.MCPAction)
	mux.HandleFunc("/", h.Asset)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := middleware.Secure(w, r, origin); err != nil {
			msg.Error(w, err)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

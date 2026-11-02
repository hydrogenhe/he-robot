package msg

import (
	"encoding/json"
	"errors"
	"gpt-robot/model"
	"log/slog"
	"net/http"
)

func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("HTTP响应写入失败", "error", err)
	}
}
func Error(w http.ResponseWriter, err error) {
	wrapped := model.Internal(err)
	var e *model.Error
	errors.As(wrapped, &e)
	if e.Err != nil {
		slog.Error("本地请求失败", "code", e.Code, "error", e.Err)
	}
	JSON(w, e.Status, map[string]any{"error": map[string]any{"code": e.Code, "message": e.Message}})
}

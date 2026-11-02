package httpinit

import (
	"context"
	"encoding/hex"
	"gpt-robot/core"
	"gpt-robot/model"
)

type webSessionState struct {
	Origin  string `json:"origin"`
	Session string `json:"session"`
}

// persistentWebSession 保留同一本机地址的网页会话，CSRF和MCP凭据仍按进程生成。
func persistentWebSession(ctx context.Context, store *core.Store, origin, candidate string) (string, error) {
	state := webSessionState{}
	found, err := store.Read(ctx, ".private/web-session.json", &state)
	if err != nil {
		return "", err
	}
	if found {
		raw, err := hex.DecodeString(state.Session)
		if err != nil || len(raw) != 32 {
			return "", model.Fail("DATA", "本机网页会话文件无效，未自动覆盖")
		}
		if state.Origin == origin {
			return state.Session, nil
		}
	}
	state.Origin, state.Session = origin, candidate
	if err := store.Write(ctx, ".private/web-session.json", &state); err != nil {
		return "", err
	}
	return state.Session, nil
}

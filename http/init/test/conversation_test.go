package httpinit_test

import (
	"context"
	"encoding/json"
	httpinit "gpt-robot/http/init"
	"io"
	"testing"
)

func TestConversationConfigAdminAndValidation(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	cfg := httpinit.DefaultConfig(t.TempDir())
	cfg.Address = "127.0.0.1:0"
	app, err := httpinit.Init(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	go app.HTTP.Serve(app.Listener)
	defer app.HTTP.Close()
	headers := shutdownSession(t, app)
	for _, body := range []string{`{"enabled":true}`, `{"enabled":true,"maxTurns":0,"idleSeconds":600,"maxAgeSeconds":1800,"contextTokens":12000,"totalTokens":60000}`} {
		res := request(t, app, "/api/conversation-config", body, headers)
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Fatal("invalid config accepted", res.StatusCode)
		}
	}
	res := request(t, app, "/api/conversation-config", `{"enabled":false,"maxTurns":5,"idleSeconds":300,"maxAgeSeconds":1800,"contextTokens":12000,"totalTokens":60000}`, headers)
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	var result struct {
		Config struct {
			Enabled  bool
			MaxTurns int
		}
		Windows []any
	}
	if res.StatusCode != 200 || json.Unmarshal(raw, &result) != nil || result.Config.Enabled || result.Config.MaxTurns != 5 || len(result.Windows) != 0 {
		t.Fatal("config response", res.StatusCode, string(raw))
	}
}

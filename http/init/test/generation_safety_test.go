package httpinit_test

import (
	"context"
	"encoding/json"
	httpinit "gpt-robot/http/init"
	"io"
	"testing"
)

func TestGenerationBudgetIsExplicitAdminConfig(t *testing.T) {
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
	for _, invalid := range []string{`{"ownerDailyCalls":0}`, `{"ownerDailyCalls":0,"ownerDailyTokens":0,"otherDailyCalls":0,"otherDailyTokens":1000}`} {
		res := request(t, app, "/api/generation-safety-config", invalid, headers)
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Fatal("invalid budget saved", res.StatusCode)
		}
	}
	res := request(t, app, "/api/generation-safety-config", `{"ownerDailyCalls":0,"ownerDailyTokens":0,"otherDailyCalls":10,"otherDailyTokens":1000}`, headers)
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	var result struct {
		Config struct {
			OwnerDailyCalls int `json:"ownerDailyCalls"`
			OtherDailyCalls int `json:"otherDailyCalls"`
		} `json:"config"`
	}
	if res.StatusCode != 200 || json.Unmarshal(raw, &result) != nil || result.Config.OwnerDailyCalls != 0 || result.Config.OtherDailyCalls != 10 {
		t.Fatal("explicit owner zero not preserved", res.StatusCode)
	}
}

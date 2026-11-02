package httpinit_test

import (
	"context"
	"encoding/json"
	"gpt-robot/core"
	httpinit "gpt-robot/http/init"
	"gpt-robot/model"
	"io"
	"net/http"
	"strings"
	"testing"
)

func request(t *testing.T, app *httpinit.Server, path, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", app.Origin+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}
func TestHTTPBoundariesAndWhitelist(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	root := t.TempDir()
	cfg := httpinit.DefaultConfig(root)
	cfg.Address = "127.0.0.1:0"
	app, err := httpinit.Init(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	go app.HTTP.Serve(app.Listener)
	defer app.HTTP.Close()
	store := core.NewStore(root)
	var runtime model.Runtime
	store.Read(context.Background(), ".private/runtime.json", &runtime)
	headers := map[string]string{"Authorization": "Bearer " + runtime.Token}
	res := request(t, app, "/mcp-action", `{"action":"send","input":{}}`, headers)
	if res.StatusCode != 403 {
		t.Fatal(res.StatusCode)
	}
	res = request(t, app, "/mcp-action", `{"action":"context","input":{}}`, headers)
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || strings.Contains(string(body), runtime.Token) || !strings.Contains(string(body), "persona") {
		t.Fatal(res.StatusCode, string(body))
	}
	bad := map[string]string{"Authorization": "Bearer " + runtime.Token, "Origin": "https://evil.example"}
	res = request(t, app, "/mcp-action", `{"action":"context","input":{}}`, bad)
	if res.StatusCode != 403 {
		t.Fatal(res.StatusCode)
	}
	ui := shutdownSession(t, app)
	csrf := ui["X-CSRF-Token"]
	delete(ui, "X-CSRF-Token")
	res = request(t, app, "/api/overview", `{}`, ui)
	if res.StatusCode != 401 {
		t.Fatal(res.StatusCode)
	}
	ui["X-CSRF-Token"] = csrf
	res = request(t, app, "/api/overview", `{}`, ui)
	body, _ = io.ReadAll(res.Body)
	if res.StatusCode != 200 || strings.Contains(string(body), runtime.Token) {
		t.Fatal(res.StatusCode, string(body))
	}
	res = request(t, app, "/mcp-action", `{"action":"comments","input":{"bvid":"BV161nZ6JE6b","page":0}}`, headers)
	if res.StatusCode != 400 {
		t.Fatal(res.StatusCode)
	}
	res = request(t, app, "/mcp-action", `{"action":"context","input":{"unexpected":true}}`, headers)
	if res.StatusCode != 400 {
		t.Fatal(res.StatusCode)
	}
	res = request(t, app, "/mcp-action", `{"action":"auto-config","input":{"enabled":true}}`, headers)
	if res.StatusCode != 403 {
		t.Fatal("MCP can enable automatic sending", res.StatusCode)
	}

	for _, action := range []string{"conversation-status", "conversation-config", "browsing-status", "browsing-config", "browsing-read", "generation-safety-status", "generation-safety-config", "robot-control", "private-send", "private-config", "private-refresh", "private-status", "private-memories", "private-memory-forget", "private-resume", "diary-status", "diary-config", "diary-generate", "development-status", "character-status"} {
		body := `{"action":"` + action + `","input":{}}`
		response := request(t, app, "/mcp-action", body, headers)
		if response.StatusCode != 403 {
			t.Fatal("private/robot exposed to MCP", action, response.StatusCode)
		}
	}
	res = request(t, app, "/api/robot-control", `{"enabled":false}`, ui)
	if res.StatusCode != 200 {
		t.Fatal("robot pause failed", res.StatusCode)
	}
	res = request(t, app, "/api/robot-status", `{}`, ui)
	body, _ = io.ReadAll(res.Body)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"enabled":false`) {
		t.Fatal("robot status incorrect", string(body))
	}
	res = request(t, app, "/api/private-send", `{"requestId":"11111111-1111-4111-8111-111111111111","text":"test","peerUid":"999"}`, ui)
	if res.StatusCode != 400 {
		t.Fatal("recipient override accepted", res.StatusCode)
	}
	res = request(t, app, "/api/private-memories", `{"query":"植物","peerUid":"999"}`, ui)
	if res.StatusCode != 400 {
		t.Fatal("private memory recipient override accepted", res.StatusCode)
	}
	res = request(t, app, "/api/private-status", `{}`, ui)
	if res.StatusCode != 200 {
		t.Fatal("private local status unavailable", res.StatusCode)
	}
	res = request(t, app, "/api/auto-preview", `{"text":"帮我算2+2"}`, ui)
	var decision struct {
		Reply  string `json:"reply"`
		Tokens int    `json:"modelTokens"`
	}
	if err := json.NewDecoder(res.Body).Decode(&decision); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 || decision.Reply != "" || decision.Tokens != 0 {
		t.Fatal(res.StatusCode, decision)
	}
	res = request(t, app, "/api/auto-status", `{}`, ui)
	if res.StatusCode != 200 {
		t.Fatal("auto status unavailable", res.StatusCode)
	}

	req, _ := http.NewRequest("GET", app.Origin+"/", nil)
	req.Host = "evil.example"
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal(res.StatusCode)
	}
}
func TestDuplicateStartDoesNotOverwriteRuntime(t *testing.T) {
	root := t.TempDir()
	cfg := httpinit.DefaultConfig(root)
	cfg.Address = "127.0.0.1:0"
	first, err := httpinit.Init(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Listener.Close()
	cfg.Address = first.Listener.Addr().String()
	second, err := httpinit.Init(context.Background(), cfg, nil)
	if err == nil {
		second.Listener.Close()
		t.Fatal("second bind unexpectedly succeeded")
	}
	var value model.Runtime
	store := core.NewStore(root)
	store.Read(context.Background(), ".private/runtime.json", &value)
	if value.Origin != first.Origin {
		t.Fatal(value)
	}
}
func TestNonLoopbackRejected(t *testing.T) {
	cfg := httpinit.DefaultConfig(t.TempDir())
	cfg.Address = "0.0.0.0:0"
	app, err := httpinit.Init(context.Background(), cfg, nil)
	if err == nil {
		app.Listener.Close()
		t.Fatal("public binding accepted")
	}
}

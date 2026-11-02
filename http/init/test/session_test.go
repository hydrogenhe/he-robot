package httpinit_test

import (
	"context"
	"encoding/json"
	"gpt-robot/core"
	httpinit "gpt-robot/http/init"
	"gpt-robot/model"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectNavigationSessionSurvivesRestartButCSRFMustRefresh(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	root := t.TempDir()
	cfg := httpinit.DefaultConfig(root)
	cfg.Address = "127.0.0.1:0"
	first, err := httpinit.Init(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	go first.HTTP.Serve(first.Listener)
	if strings.Contains(first.OpenURL, "#") || strings.Contains(first.OpenURL, "key=") {
		t.Fatal("URL still contains a changing secret")
	}
	headers := shutdownSession(t, first)
	var oldRuntime model.Runtime
	store := core.NewStore(root)
	if _, err := store.Read(context.Background(), ".private/runtime.json", &oldRuntime); err != nil {
		t.Fatal(err)
	}
	cfg.Address = first.Listener.Addr().String()
	first.HTTP.Close()
	second, err := httpinit.Init(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	go second.HTTP.Serve(second.Listener)
	defer second.HTTP.Close()
	if first.OpenURL != second.OpenURL {
		t.Fatal("stable address changed")
	}
	response := request(t, second, "/api/robot-status", "{}", headers)
	if response.StatusCode != 401 {
		t.Fatal("old CSRF was accepted after restart", response.StatusCode)
	}
	req, _ := http.NewRequest("GET", second.Origin+"/session", nil)
	req.Header.Set("Cookie", headers["Cookie"])
	session, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Body.Close()
	var value struct {
		CSRF string `json:"csrf"`
	}
	if err := json.NewDecoder(session.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	if session.StatusCode != 200 || value.CSRF == "" || value.CSRF == headers["X-CSRF-Token"] {
		t.Fatal("cookie lost or CSRF not renewed")
	}
	headers["X-CSRF-Token"] = value.CSRF
	response = request(t, second, "/api/robot-status", "{}", headers)
	if response.StatusCode != 200 {
		t.Fatal("existing browser could not recover", response.StatusCode)
	}
	oldMCP := map[string]string{"Authorization": "Bearer " + oldRuntime.Token}
	response = request(t, second, "/mcp-action", `{"action":"context","input":{}}`, oldMCP)
	if response.StatusCode != 401 {
		t.Fatal("old MCP credential unexpectedly persisted")
	}
	info, err := os.Stat(filepath.Join(root, ".private/web-session.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("persistent session file is not private", err)
	}
}

func TestOnlyLocalTopLevelNavigationCanEstablishSession(t *testing.T) {
	cfg := httpinit.DefaultConfig(t.TempDir())
	cfg.Address = "127.0.0.1:0"
	app, err := httpinit.Init(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	go app.HTTP.Serve(app.Listener)
	defer app.HTTP.Close()
	for _, scenario := range []string{"plain", "image", "iframe", "script-fetch", "cross-site", "same-site", "host"} {
		t.Run(scenario, func(t *testing.T) {
			req, _ := http.NewRequest("GET", app.OpenURL, nil)
			req.Header.Set("Sec-Fetch-Site", "none")
			req.Header.Set("Sec-Fetch-Mode", "navigate")
			req.Header.Set("Sec-Fetch-Dest", "document")
			req.Header.Set("Sec-Fetch-User", "?1")
			switch scenario {
			case "plain":
				req.Header = make(http.Header)
			case "image":
				req.Header.Set("Sec-Fetch-Dest", "image")
			case "iframe":
				req.Header.Set("Sec-Fetch-Dest", "iframe")
			case "script-fetch":
				req.Header.Set("Sec-Fetch-Mode", "cors")
			case "cross-site":
				req.Header.Set("Sec-Fetch-Site", "cross-site")
				req.Header.Set("Origin", "https://evil.example")
			case "same-site":
				req.Header.Set("Sec-Fetch-Site", "same-site")
			case "host":
				req.Host = "evil.example"
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if len(res.Cookies()) != 0 || strings.Contains(string(body), "robot_session=") {
				t.Fatal("non-user navigation obtained a session")
			}
		})
	}
	req, _ := http.NewRequest("GET", app.Origin+"/session", nil)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 || len(response.Cookies()) != 0 {
		t.Fatal("unauthenticated session endpoint minted credentials")
	}
}

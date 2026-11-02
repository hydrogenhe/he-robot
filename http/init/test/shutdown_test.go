package httpinit_test

import (
	"context"
	"encoding/json"
	"gpt-robot/core"
	httpinit "gpt-robot/http/init"
	"gpt-robot/model"
	"io"
	"net/http"
	"testing"
	"time"
)

type shutdownWorker struct {
	started chan struct{}
	stopped chan struct{}
}
type shutdownWorkerInterface interface{ Run(context.Context) }

var _ shutdownWorkerInterface = (*shutdownWorker)(nil)

func (worker *shutdownWorker) Run(ctx context.Context) {
	close(worker.started)
	<-ctx.Done()
	close(worker.stopped)
}
func runShutdownServer(app *httpinit.Server, done chan<- error) {
	done <- app.Run(context.Background())
}
func shutdownSession(t *testing.T, app *httpinit.Server) map[string]string {
	t.Helper()
	req, err := http.NewRequest("GET", app.OpenURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"Sec-Fetch-Site": "none", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document", "Sec-Fetch-User": "?1"} {
		req.Header.Set(k, v)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	cookies := response.Cookies()
	response.Body.Close()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].MaxAge <= 0 {
		t.Fatal("bad persistent session cookie")
	}
	sessionRequest, err := http.NewRequest("GET", app.Origin+"/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	sessionRequest.AddCookie(cookies[0])
	response, err = http.DefaultClient.Do(sessionRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var session struct {
		CSRF string `json:"csrf"`
	}
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{"Cookie": cookies[0].String(), "Origin": app.Origin, "X-CSRF-Token": session.CSRF}
	return headers
}
func TestBrowserShutdownAcknowledgesCancelsBackgroundAndClosesListener(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	root := t.TempDir()
	service := core.NewService(root, nil)
	config := httpinit.DefaultConfig(root)
	config.Address = "127.0.0.1:0"
	app, err := httpinit.Init(context.Background(), config, service)
	if err != nil {
		t.Fatal(err)
	}
	defer app.HTTP.Close()
	worker := &shutdownWorker{started: make(chan struct{}), stopped: make(chan struct{})}
	app.Background = worker.Run
	done := make(chan error, 1)
	go runShutdownServer(app, done)
	select {
	case <-worker.started:
	case <-time.After(3 * time.Second):
		t.Fatal("background did not start")
	}
	response := request(t, app, "/api/shutdown", `{"confirmed":true}`, nil)
	if response.StatusCode != 401 {
		t.Fatal("unauthenticated shutdown allowed")
	}
	headers := shutdownSession(t, app)
	response = request(t, app, "/api/shutdown", `{}`, headers)
	if response.StatusCode != 400 {
		t.Fatal("shutdown without confirmation allowed")
	}
	var runtime model.Runtime
	if _, err := service.Store.Read(context.Background(), ".private/runtime.json", &runtime); err != nil {
		t.Fatal(err)
	}
	mcp := map[string]string{"Authorization": "Bearer " + runtime.Token}
	response = request(t, app, "/mcp-action", `{"action":"shutdown","input":{"confirmed":true}}`, mcp)
	if response.StatusCode != 403 {
		t.Fatal("MCP can shut down control service")
	}
	response = request(t, app, "/api/shutdown", `{"confirmed":true}`, headers)
	body, err := io.ReadAll(response.Body)
	var value struct {
		ShuttingDown bool `json:"shuttingDown"`
	}
	if err != nil || json.Unmarshal(body, &value) != nil || response.StatusCode != 200 || !value.ShuttingDown {
		t.Fatal("ack lost", string(body), err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("intentional shutdown must exit successfully", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not exit")
	}
	select {
	case <-worker.stopped:
	default:
		t.Fatal("background not cancelled before return")
	}
	state, err := service.RobotStatus(context.Background())
	if err != nil || state.Enabled || state.Status != "stopped" {
		t.Fatal("shutdown did not persist stopped state", state, err)
	}
	client := &http.Client{Timeout: time.Second}
	if response, err := client.Get(app.Origin + "/"); err == nil {
		response.Body.Close()
		t.Fatal("listener remains open")
	}
	restored := core.NewService(root, nil)
	if err := restored.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err = restored.RobotStatus(context.Background())
	if err != nil || state.Status != "stopped" {
		t.Fatal("reopen unexpectedly enabled robot", state, err)
	}
}

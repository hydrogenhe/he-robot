package businesshost

import (
	"context"
	"encoding/json"
	"fmt"
	"gpt-robot/model"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"time"
)

type DeepSeekHarnessBackend struct {
	Model  string
	Effort string
}
type DeepSeekHarnessBackendInterface interface {
	OpenConversation(context.Context, string) (JSONSession, error)
	GenerateJSON(context.Context, string, []byte) (*model.GeneratedReply, error)
}

var _ DeepSeekHarnessBackendInterface = (*DeepSeekHarnessBackend)(nil)
var _ JSONBackend = (*DeepSeekHarnessBackend)(nil)

func LocalHarnessPath(provider string) (string, error) {
	if provider == "codex" {
		return codexExecutable()
	}
	if provider != "deepseek" {
		return "", model.Fail("INPUT", "不支持的本机Harness")
	}
	if path := os.Getenv("GPT_ROBOT_DSH_PATH"); path != "" {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
		return "", model.Fail("HARNESS_NOT_FOUND", "配置的DeepSeek Harness程序不存在")
	}
	if path, err := exec.LookPath("dsh"); err == nil {
		return path, nil
	}
	path := "/Applications/DeepSeek Harness.app/Contents/Resources/runtime/cli/bin/dsh"
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path, nil
	}
	return "", model.Fail("HARNESS_NOT_FOUND", "未找到DeepSeek Harness，请先安装本机程序")
}

// ProbeLocalHarness 仅启动协议并握手，不创建业务输入或调用模型。
func ProbeLocalHarness(ctx context.Context, root, provider string, models ...string) error {
	if _, err := LocalHarnessPath(provider); err != nil {
		return model.Fail("HARNESS_NOT_FOUND", "未找到所选本机Harness，请先安装")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if provider == "codex" {
		_, _, _, cleanup, err := openSession(ctx, root, true)
		if err != nil {
			return model.Fail("HARNESS_CONNECT", "Codex程序握手失败，请检查安装和运行环境")
		}
		cleanup()
		return nil
	}
	modelID := ""
	effort := ""
	if len(models) > 0 {
		modelID = models[0]
	}
	if len(models) > 1 {
		effort = models[1]
	}
	_, cleanup, err := openDeepSeekHarness(ctx, "只生成JSON，不使用工具。", modelID, effort)
	if err != nil {
		return err
	}
	cleanup()
	return nil
}

func openDeepSeekHarness(ctx context.Context, instruction, modelID, effort string) (*connection, func(), error) {
	if modelID == "" {
		modelID = "deepseek-flash"
	}
	if effort == "" {
		effort = "low"
	}
	if modelID != "deepseek-flash" && modelID != "deepseek-v4-pro" {
		return nil, nil, model.Fail("INPUT", "不支持的DeepSeek Harness模型")
	}
	if effort != "none" && effort != "low" && effort != "high" {
		return nil, nil, model.Fail("INPUT", "不支持的DeepSeek思考档位")
	}
	path, err := LocalHarnessPath("deepseek")
	if err != nil {
		return nil, nil, err
	}
	work, err := os.MkdirTemp("", "gpt-robot-dsh-")
	if err != nil {
		return nil, nil, err
	}
	dumpPath, dumpArgs, err := sandboxHarness(path, work, []string{"--profile", "sdk", "--dump-default-config"})
	if err != nil {
		os.RemoveAll(work)
		return nil, nil, err
	}
	dump := exec.CommandContext(ctx, dumpPath, dumpArgs...)
	dump.Dir = work
	dump.Env = append(businessEnvironment(), "DSH_HOME="+work)
	raw, err := dump.Output()
	if err != nil {
		os.RemoveAll(work)
		return nil, nil, model.Fail("HARNESS_CONNECT", "DeepSeek Harness配置探测失败")
	}
	patch, err := deepSeekHarnessPatch(raw, instruction)
	if err != nil {
		os.RemoveAll(work)
		return nil, nil, err
	}
	patchPath := filepath.Join(work, "robot-overlay.json")
	if err := os.WriteFile(patchPath, patch, 0600); err != nil {
		os.RemoveAll(work)
		return nil, nil, err
	}
	args := []string{"--profile", "sdk", "--patch", patchPath}
	runPath, runArgs, err := sandboxHarness(path, work, args)
	if err != nil {
		os.RemoveAll(work)
		return nil, nil, err
	}
	c, err := start(ctx, runPath, work, runArgs, "DSH_HOME="+work)
	if err != nil {
		os.RemoveAll(work)
		return nil, nil, model.Fail("HARNESS_CONNECT", "DeepSeek Harness无法启动")
	}
	cleanup := (&sessionCleanup{c: c, work: work}).close
	params := map[string]any{"cwd": work, "provider": "deepseek-account", "model": modelID, "reasoningEffort": effort, "maxTokens": 2048}
	err = c.send(1, "initialize", params)
	var result json.RawMessage
	if err == nil {
		result, err = c.wait(ctx, 1)
	}
	var identity struct {
		ServerInfo struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if err != nil || json.Unmarshal(result, &identity) != nil || identity.ServerInfo.Name != "deepseek-harness-sdk-runtime" {
		cleanup()
		return nil, nil, model.Fail("HARNESS_CONNECT", "DeepSeek Harness SDK握手失败或版本不兼容")
	}
	return c, cleanup, nil
}

// deepSeekHarnessPatch 默认关闭所有已探测的插件，仅保留无工具、无持久会话的账户生成依赖。
func deepSeekHarnessPatch(raw []byte, instruction string) ([]byte, error) {
	allowed := map[string]bool{"timer": true, "llm": true, "session": true, "session-log-deepseek": true, "agent": true, "agent-loop": true, "session-projection": true, "authorization": true, "deepseek-account": true, "credentials": true, "tools": true, "system-prompt": true, "sdk-app-startup": true, "sdk-jsonrpc-server": true, "deepseek-llm-api-extensions": true, "llm-deepseek-account": true}
	matches := regexp.MustCompile(`(?m)^- id: ([a-z0-9-]+)\r?$`).FindAllSubmatch(raw, -1)
	if len(matches) == 0 || len(matches) > 500 {
		return nil, model.Fail("HARNESS_VERSION", "Harness配置格式不兼容")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	nativeHome := os.Getenv("DSH_HOME")
	if nativeHome == "" {
		nativeHome = filepath.Join(home, ".dsh")
	}
	rows := []map[string]any{}
	seen := map[string]bool{}
	for _, match := range matches {
		id := string(match[1])
		seen[id] = true
		row := map[string]any{"id": id, "disabled": !allowed[id]}
		if id == "credentials" {
			row["config"] = map[string]any{"path": filepath.Join(nativeHome, ".credentials.yaml"), "dshHome": nativeHome, "watch": false}
		}
		if id == "deepseek-account" {
			var platform any
			if runtime.GOOS == "darwin" || runtime.GOOS == "win32" || runtime.GOOS == "windows" {
				platform = runtime.GOOS
				if platform == "windows" {
					platform = "win32"
				}
			}
			row["config"] = map[string]any{"desktopPlatform": platform}
		}
		if id == "system-prompt" {
			row["config"] = map[string]any{"personaPrefix": instruction, "personaSuffix": ""}
		}
		if id == "llm-deepseek-account" {
			row["config"] = map[string]any{"retryPolicy": map[string]any{"mode": "normal", "maxRetries": 0}}
		}
		rows = append(rows, row)
	}
	for id := range allowed {
		if !seen[id] {
			return nil, model.Fail("HARNESS_VERSION", "Harness缺少必要插件，暂不兼容此版本")
		}
	}
	return json.Marshal(rows)
}

func (g *DeepSeekHarnessBackend) GenerateJSON(ctx context.Context, instruction string, body []byte) (*model.GeneratedReply, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	session, err := g.OpenConversation(ctx, instruction)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	return session.GenerateJSON(ctx, instruction, body)
}

type deepSeekConversation struct {
	c        *connection
	id       string
	model    string
	sequence int
	cleanup  func()
}

var _ JSONSession = (*deepSeekConversation)(nil)

func (g *DeepSeekHarnessBackend) OpenConversation(ctx context.Context, instruction string) (JSONSession, error) {
	c, cleanup, err := openDeepSeekHarness(ctx, instruction, g.Model, g.Effort)
	if err != nil {
		return nil, err
	}
	id := g.Model
	if id == "" {
		id = "deepseek-flash"
	}
	session := &deepSeekConversation{c: c, id: "robot-" + model.ID(), model: id, sequence: 10, cleanup: cleanup}
	return session, nil
}

func (s *deepSeekConversation) Close() { s.cleanup() }

func (s *deepSeekConversation) GenerateJSON(ctx context.Context, instruction string, body []byte) (*model.GeneratedReply, error) {
	if len(body) > model.ReplyContextMaxBytes || len(instruction)+len(body) > 10000 {
		return nil, model.Fail("CONTEXT_BUDGET", "模型输入超过业务预算")
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	c := s.c
	sessionID := s.id
	params := map[string]any{"sessionId": sessionID, "contentBlocks": []map[string]string{{"type": "text", "text": string(body)}}}
	s.sequence++
	if err := c.send(s.sequence, "session/prompt", params); err != nil {
		return nil, model.Fail("HARNESS_PROTOCOL", "Harness输入失败，不自动重试")
	}
	result := &model.GeneratedReply{}
	turns := 0
	for {
		message, err := c.next(ctx)
		if err != nil {
			return result, model.Fail("HARNESS_RUN", "Harness处理失败或超时，不自动重试")
		}
		if message.Error != nil || (len(message.ID) > 0 && message.Method != "") {
			return result, model.Fail("HARNESS_RUN", "Harness拒绝请求或要求额外动作")
		}
		if message.Method != "session.event" {
			continue
		}
		var event struct {
			SessionID string `json:"sessionId"`
			Event     struct {
				Type string `json:"type"`
				Data struct {
					Reason  string `json:"reason"`
					Message struct {
						Content []struct {
							Type string `json:"type"`
							Text string `json:"text"`
						} `json:"content"`
					} `json:"message"`
					Usage *struct {
						Input  *int64 `json:"inputTokens"`
						Output *int64 `json:"outputTokens"`
						Cached *int64 `json:"cacheReadTokens"`
					} `json:"usage"`
				} `json:"data"`
			} `json:"event"`
		}
		if json.Unmarshal(message.Params, &event) != nil || event.SessionID != sessionID {
			return result, model.Fail("HARNESS_PROTOCOL", "Harness事件无法核验")
		}
		switch event.Event.Type {
		case "turn/start":
			turns++
			if turns > 1 {
				return result, model.Fail("HARNESS_EXTRA_CALL", "Harness出现额外生成，已停止")
			}
		case "assistant/attempt":
			return result, model.Fail("HARNESS_EXTRA_CALL", "Harness生成未完成，不自动重试")
		case "assistant/message":
			for _, block := range event.Event.Data.Message.Content {
				if block.Type == "text" {
					result.Text += block.Text
				} else if block.Type != "reasoning" {
					return result, model.Fail("HARNESS_TOOL", "Harness执行非文本动作，已停止")
				}
			}
			if u := event.Event.Data.Usage; u != nil && u.Input != nil && u.Output != nil {
				modelID := s.model
				if modelID == "" {
					modelID = "deepseek-flash"
				}
				result.Usage = &model.TokenUsage{Provider: "deepseek", Model: modelID, RequestID: fmt.Sprintf("%s/%d", sessionID, s.sequence), Input: *u.Input, Output: *u.Output, CachedInput: u.Cached, Source: "deepseek_harness"}
			}
		case "turn/end":
			if event.Event.Data.Reason != "completed" || turns != 1 {
				return result, model.Fail("HARNESS_RUN", "Harness未正常完成")
			}
			return result, nil
		}
	}
}

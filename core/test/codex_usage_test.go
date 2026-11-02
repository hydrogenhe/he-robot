package core_test

import (
	"encoding/json"
	"fmt"
	"gpt-robot/core"
	"gpt-robot/http/handler"
	"gpt-robot/model"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeUsageRecord(t *testing.T, f *os.File, typ string, payload map[string]any) {
	t.Helper()
	row := map[string]any{"type": typ, "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "payload": payload}
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(append(b, '\n')); err != nil {
		t.Fatal(err)
	}
}
func writeUsageTokens(t *testing.T, f *os.File, input, output int64) {
	totals := map[string]any{"input_tokens": input, "output_tokens": output, "cached_input_tokens": input / 2, "reasoning_output_tokens": output / 2}
	writeUsageRecord(t, f, "event_msg", map[string]any{"type": "token_count", "info": map[string]any{"total_token_usage": totals, "last_token_usage": totals}})
}
func appendBusinessTurn(t *testing.T, file, turn string, ids []string, dirty bool) {
	t.Helper()
	f, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	writeUsageTokens(t, f, 100, 20)
	writeUsageRecord(t, f, "event_msg", map[string]any{"type": "task_started", "turn_id": turn})
	writeUsageRecord(t, f, "turn_context", map[string]any{"turn_id": turn, "model": "fixture-model"})
	for i, id := range ids {
		call := fmt.Sprintf("call%d", i)
		writeUsageRecord(t, f, "response_item", map[string]any{"type": "function_call", "name": "mcp__gpt_robot__character_context", "call_id": call})
		writeUsageRecord(t, f, "response_item", map[string]any{"type": "function_call_output", "call_id": call, "output": "{\"operationId\":\"" + id + "\"}"})
	}
	if dirty {
		writeUsageRecord(t, f, "response_item", map[string]any{"type": "custom_tool_call", "name": "exec", "call_id": "dev"})
	}
	writeUsageTokens(t, f, 300, 60)
	writeUsageTokens(t, f, 300, 60)
	writeUsageRecord(t, f, "event_msg", map[string]any{"type": "task_complete", "turn_id": turn})
}
func TestCodexRealEntryAssociationSharedCountingAndRestart(t *testing.T) {
	s, fake := setup(t)
	home := os.Getenv("CODEX_HOME")
	dir := filepath.Join(home, "sessions")
	os.MkdirAll(dir, 0700)
	file := filepath.Join(dir, "business.jsonl")
	threadID, turnID := model.ID(), model.ID()
	// 测试专用临时只读索引，生产索引不会被写入。
	escaped := strings.ReplaceAll(file, "'", "''")
	sql := fmt.Sprintf("CREATE TABLE threads(id TEXT,rollout_path TEXT,updated_at INTEGER,source TEXT); INSERT INTO threads VALUES('%s','%s',%d,'cli');", threadID, escaped, time.Now().Unix())
	cmd := exec.Command("/usr/bin/sqlite3", filepath.Join(home, "state_1.sqlite"), sql)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	actions := handler.NewActions(s)
	ids := []string{}
	for range 2 {
		result, err := actions.Execute(ctx, "context", json.RawMessage(`{}`), false)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, result.(map[string]any)["operationId"].(string))
	}
	appendBusinessTurn(t, file, turnID, ids, false)
	query := model.OperationQuery{}
	page, err := s.QueryOperations(ctx, &query)
	if err != nil || page.Reported != 2 || page.Input != 200 || page.Output != 40 {
		t.Fatalf("shared usage counted twice: %+v %v", page, err)
	}
	for _, op := range page.Items {
		if op.UsageOwner != ids[0] || len(op.Usage) != 1 || op.Usage[0].Source != "codex_local" || op.Usage[0].ThreadID != threadID {
			t.Fatal(op)
		}
	}
	// 重复查询和新进程读取同一日志，仍只计240，不加缓存子集或重复检查点。
	restored := core.NewService(s.Store.Root, fake)
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	page, err = restored.QueryOperations(ctx, &query)
	if err != nil || page.Input+page.Output != 240 || page.Reported != 2 {
		t.Fatal(page, err)
	}
	// 筛选命中共享操作时返回整轮去重用量，避免主操作不在当前页/筛选造成假零。
	report := model.UsageReport{OperationID: ids[1], Usage: model.TokenUsage{Provider: "codex-local", RequestID: "duplicate", Model: "fixture", Input: 1, Output: 1}}
	if err := restored.ReportUsage(ctx, &report); err == nil {
		t.Fatal("manual report duplicated collected turn")
	}
	// 新的混合开发轮次有真实计量和合法ID也必须排除。
	result, err := actions.Execute(ctx, "context", json.RawMessage(`{}`), false)
	if err != nil {
		t.Fatal(err)
	}
	dirtyID := result.(map[string]any)["operationId"].(string)
	appendBusinessTurn(t, file, model.ID(), []string{dirtyID}, true)
	page, err = s.QueryOperations(ctx, &query)
	if err != nil || page.Total != 3 || page.Reported != 2 || page.Input+page.Output != 240 {
		t.Fatal(page, err)
	}
	for _, op := range page.Items {
		if op.ID == dirtyID && len(op.Usage) != 0 {
			t.Fatal("development counted")
		}
	}
	// 显式排除整个开发聊天，即使其中存在纯机器人工具轮次也不计入。
	policy := map[string]any{"excludedThreadIds": []string{threadID}}
	if err := s.Store.Write(ctx, "config/usage.json", policy); err != nil {
		t.Fatal(err)
	}
	excluded := core.NewService(s.Store.Root, fake)
	if err := excluded.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	page, err = excluded.QueryOperations(ctx, &query)
	if err != nil || page.Reported != 0 || page.Input+page.Output != 0 {
		t.Fatal("excluded development thread counted", page, err)
	}
	if len(fake.counts()) != 0 {
		t.Fatal("usage queried platform")
	}
}

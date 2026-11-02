package codex_test

import (
	"bytes"
	"context"
	"encoding/json"
	"gpt-robot/usage/codex"
	"os"
	"path/filepath"
	"testing"
)

func record(t *testing.T, typ string, payload any) []byte {
	t.Helper()
	value := map[string]any{"type": typ, "timestamp": "2026-10-31T04:00:00Z", "payload": payload}
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}
func event(t *testing.T, kind string, extra map[string]any) []byte {
	extra["type"] = kind
	return record(t, "event_msg", extra)
}
func tokens(t *testing.T, input, output int) []byte {
	totals := map[string]any{"input_tokens": input, "cached_input_tokens": input / 2, "output_tokens": output, "reasoning_output_tokens": output / 2}
	return event(t, "token_count", map[string]any{"info": map[string]any{"total_token_usage": totals, "last_token_usage": totals}})
}
func fixture(t *testing.T, dirty bool) []byte {
	b := tokens(t, 100, 20)
	b = append(b, event(t, "task_started", map[string]any{"turn_id": "turn1"})...)
	b = append(b, record(t, "turn_context", map[string]any{"turn_id": "turn1", "model": "actual-model"})...)
	b = append(b, record(t, "response_item", map[string]any{"type": "function_call", "call_id": "call1", "name": "mcp__gpt_robot__character_context"})...)
	output := map[string]any{"content": []any{map[string]any{"type": "text", "text": "{\"operationId\":\"operation1\"}"}}}
	b = append(b, record(t, "response_item", map[string]any{"type": "function_call_output", "call_id": "call1", "output": output})...)
	if dirty {
		b = append(b, record(t, "response_item", map[string]any{"type": "custom_tool_call", "call_id": "dev", "name": "exec", "input": "not persisted"})...)
	}
	b = append(b, tokens(t, 300, 60)...)
	b = append(b, tokens(t, 300, 60)...)
	b = append(b, event(t, "task_complete", map[string]any{"turn_id": "turn1"})...)
	return b
}
func TestIncrementalCountersExcludeDevelopmentAndPartialLines(t *testing.T) {
	for _, dirty := range []bool{false, true} {
		home := t.TempDir()
		dir := filepath.Join(home, "sessions")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(dir, "test.jsonl")
		data := fixture(t, dirty)
		if err := os.WriteFile(file, data[:len(data)-3], 0600); err != nil {
			t.Fatal(err)
		}
		collector := codex.New(home)
		state := codex.State{}
		input := codex.ScanInput{State: &state, Threads: []codex.Thread{{ID: "thread1", Path: file}}}
		batch, err := collector.Scan(context.Background(), &input)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Turns) != 0 {
			t.Fatal("unfinished line imported")
		}
		input.State = &batch.State
		f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		f.Write(data[len(data)-3:])
		f.Close()
		batch, err = collector.Scan(context.Background(), &input)
		if err != nil || len(batch.Turns) != 1 {
			t.Fatal(batch, err)
		}
		delta := codex.Delta(&batch.Turns[0])
		if dirty && delta != nil {
			t.Fatal("development mixed turn counted")
		}
		if !dirty && (delta == nil || delta.Input != 200 || delta.Output != 40 || *delta.Cached != 100) {
			t.Fatal(delta)
		}
		input.State = &batch.State
		next, err := collector.Scan(context.Background(), &input)
		if err != nil || next.Bytes != 0 || len(next.Turns) != 0 {
			t.Fatal("unchanged file reread", next, err)
		}
	}
}
func TestCounterResetAndOutsidePathAreExcluded(t *testing.T) {
	baseline := codex.Totals{Input: 100, Output: 20}
	end := codex.Totals{Input: 10, Output: 2}
	turn := codex.Turn{ID: "turn", Model: "m", Baseline: &baseline, End: &end, Completed: true}
	if codex.Delta(&turn) != nil {
		t.Fatal("reset counted")
	}
	home := t.TempDir()
	file := filepath.Join(t.TempDir(), "outside.jsonl")
	os.WriteFile(file, fixture(t, false), 0600)
	state := codex.State{}
	input := codex.ScanInput{State: &state, Threads: []codex.Thread{{ID: "thread", Path: file}}}
	batch, err := codex.New(home).Scan(context.Background(), &input)
	if err != nil || batch.Bytes != 0 {
		t.Fatal(batch, err)
	}
}

func TestSafeRobotWrapperAndInjectedScript(t *testing.T) {
	for _, safe := range []bool{true, false} {
		home := t.TempDir()
		dir := filepath.Join(home, "sessions")
		os.MkdirAll(dir, 0700)
		file := filepath.Join(dir, "test.jsonl")
		data := fixture(t, false)
		direct := record(t, "response_item", map[string]any{"type": "function_call", "call_id": "call1", "name": "mcp__gpt_robot__character_context"})
		input := `text(await tools.mcp__gpt_robot__character_context({}));`
		if !safe {
			input += ` await tools.exec_command({"cmd":"pwd"});`
		}
		wrapped := record(t, "response_item", map[string]any{"type": "custom_tool_call", "call_id": "call1", "name": "exec", "input": input})
		data = bytes.Replace(data, direct, wrapped, 1)
		native := event(t, "item_completed", map[string]any{"item": map[string]any{"type": "McpToolCall", "id": "native-call1", "server": "gpt_robot", "tool": "character_context", "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "{\"operationId\":\"operation1\"}"}}}}})
		checkpoint := tokens(t, 300, 60)
		data = bytes.Replace(data, checkpoint, append(native, checkpoint...), 1)
		os.WriteFile(file, data, 0600)
		state := codex.State{}
		scan := codex.ScanInput{State: &state, Threads: []codex.Thread{{ID: "thread", Path: file}}}
		result, err := codex.New(home).Scan(context.Background(), &scan)
		if err != nil || len(result.Turns) != 1 {
			t.Fatal(result, err)
		}
		delta := codex.Delta(&result.Turns[0])
		if safe != (delta != nil) {
			t.Fatal("unsafe wrapper classification", safe, delta)
		}
	}
}

func TestMissingTokenFieldsAreNotZero(t *testing.T) {
	var totals codex.Totals
	if err := json.Unmarshal([]byte(`{"output_tokens":20}`), &totals); err == nil {
		t.Fatal("missing input silently counted zero")
	}
	if err := json.Unmarshal([]byte(`{"input_tokens":0,"output_tokens":0}`), &totals); err != nil {
		t.Fatal(err)
	}
}

func TestActualCodeModeDiscoveryAndTwoNativeReceipts(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions")
	os.MkdirAll(dir, 0700)
	file := filepath.Join(dir, "actual.jsonl")
	data := tokens(t, 100, 20)
	data = append(data, event(t, "task_started", map[string]any{"turn_id": "turn"})...)
	data = append(data, record(t, "turn_context", map[string]any{"model": "actual-model", "turn_id": "turn"})...)
	inputs := []string{`const matches = ALL_TOOLS.filter(x => /gpt_robot.*(bilibili_comments|memory_search)$/.test(x.name)); text(matches);`, `text(await tools.mcp__gpt_robot__bilibili_comments({bvid:"BV18VLu6JEit",page:1}));
text(await tools.mcp__gpt_robot__memory_search({query:"记忆"}));`}
	for i, input := range inputs {
		call := string(rune('a' + i))
		data = append(data, record(t, "response_item", map[string]any{"type": "custom_tool_call", "call_id": call, "name": "exec", "input": input})...)
		data = append(data, record(t, "response_item", map[string]any{"type": "custom_tool_call_output", "call_id": call, "output": []any{map[string]any{"type": "input_text", "text": "not used for attribution"}}})...)
	}
	for i, tool := range []string{"bilibili_comments", "memory_search"} {
		data = append(data, event(t, "item_completed", map[string]any{"item": map[string]any{"type": "McpToolCall", "id": string(rune('c' + i)), "server": "gpt_robot", "tool": tool, "result": map[string]any{"_meta": map[string]any{"gpt-robot/operationId": string(rune('e' + i))}}}})...)
	}
	data = append(data, tokens(t, 300, 60)...)
	data = append(data, event(t, "task_complete", map[string]any{"turn_id": "turn"})...)
	os.WriteFile(file, data, 0600)
	state := codex.State{}
	input := codex.ScanInput{State: &state, Threads: []codex.Thread{{ID: "thread", Path: file}}}
	batch, err := codex.New(home).Scan(context.Background(), &input)
	if err != nil || len(batch.Turns) != 1 {
		t.Fatal(batch, err)
	}
	delta := codex.Delta(&batch.Turns[0])
	if delta == nil || delta.Input+delta.Output != 240 || len(batch.Turns[0].Order) != 2 {
		t.Fatal(batch.Turns[0], delta)
	}
}

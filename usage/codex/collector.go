// Package codex 只读取本地Codex计量与工具关联元数据，不导入聊天正文。
package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

type Totals struct {
	Input     int64  `json:"input_tokens"`
	Cached    *int64 `json:"cached_input_tokens"`
	Output    int64  `json:"output_tokens"`
	Reasoning *int64 `json:"reasoning_output_tokens"`
}
type Call struct {
	Action      string `json:"action"`
	OperationID string `json:"operationId"`
}
type Turn struct {
	WrapperCalls   map[string]bool `json:"wrapperCalls,omitempty"`
	NativeExpected []string        `json:"nativeExpected,omitempty"`
	NativeReceived int             `json:"nativeReceived,omitempty"`
	ThreadID       string          `json:"threadId"`
	ID             string          `json:"id"`
	Model          string          `json:"model"`
	StartedAt      string          `json:"startedAt"`
	FinishedAt     string          `json:"finishedAt"`
	Baseline       *Totals         `json:"baseline"`
	End            *Totals         `json:"end"`
	Dirty          bool            `json:"dirty"`
	Completed      bool            `json:"completed"`
	Calls          map[string]Call `json:"calls"`
	Order          []string        `json:"order"`
}
type FileState struct {
	DiscardLine bool    `json:"discardLine"`
	ThreadID    string  `json:"threadId"`
	Offset      int64   `json:"offset"`
	Identity    string  `json:"identity"`
	Last        *Totals `json:"last"`
	Turn        *Turn   `json:"turn"`
}
type State struct {
	Files map[string]FileState `json:"files"`
}
type Thread struct {
	ID   string `json:"id"`
	Path string `json:"rollout_path"`
}
type ScanInput struct {
	State   *State
	Threads []Thread
}
type Batch struct {
	State State
	Turns []Turn
	Bytes int64
}
type CollectorInterface interface {
	Discover(context.Context, time.Time) ([]Thread, error)
	Scan(context.Context, *ScanInput) (*Batch, error)
}
type Collector struct{ home string }

var _ CollectorInterface = (*Collector)(nil)

func New(home string) *Collector { c := &Collector{home: home}; return c }

// Discover 使用GPT小人同样的只读索引；只返回候选ID及路径，不读取标题和消息。
func (c *Collector) Discover(ctx context.Context, since time.Time) ([]Thread, error) {
	files, err := filepath.Glob(filepath.Join(c.home, "state_*.sqlite"))
	if err != nil {
		return nil, err
	}
	var database string
	version := -1
	for _, file := range files {
		var n int
		if _, err := fmt.Sscanf(filepath.Base(file), "state_%d.sqlite", &n); err == nil && n > version {
			database = file
			version = n
		}
	}
	if database == "" {
		return []Thread{}, nil
	}
	query := fmt.Sprintf("SELECT id,rollout_path FROM threads WHERE updated_at >= %d AND source NOT LIKE '%%\"subagent\"%%' ORDER BY updated_at DESC LIMIT 200;", since.Add(-time.Minute).Unix())
	commandCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, "/usr/bin/sqlite3", "-readonly", "-json", database, query)
	raw, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("读取Codex只读线程索引失败: %w", err)
	}
	rows := []Thread{}
	if len(raw) == 0 {
		return rows, nil
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// Scan 只读取上次完整行之后的新增字节。先持久化用量，再保存返回游标，重放仍幂等。
func (c *Collector) Scan(ctx context.Context, input *ScanInput) (*Batch, error) {
	if input == nil || input.State == nil {
		return nil, fmt.Errorf("缺少Codex扫描状态")
	}
	result := &Batch{State: State{Files: map[string]FileState{}}, Turns: []Turn{}}
	for path, state := range input.State.Files {
		result.State.Files[path] = state
	}
	for _, thread := range input.Threads {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if result.Bytes >= 32<<20 {
			break
		}
		path, err := c.allowedPath(thread.Path)
		if err != nil {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, err
		}
		state := cloneFileState(result.State.Files[path])
		identity := fileIdentity(info)
		if state.Identity != identity || state.Offset > info.Size() {
			state = FileState{Identity: identity}
		}
		if _, err = f.Seek(state.Offset, io.SeekStart); err != nil {
			f.Close()
			return nil, err
		}
		state.ThreadID = thread.ID
		reader := bufio.NewReaderSize(io.LimitReader(f, 8<<20), 64<<10)
		for {
			line, readErr := reader.ReadBytes('\n')
			if readErr != nil {
				if readErr != io.EOF {
					f.Close()
					return nil, readErr
				}
				if len(line) >= 2<<20 && state.Offset+int64(len(line)) < info.Size() {
					state.Offset += int64(len(line))
					result.Bytes += int64(len(line))
					state.DiscardLine = true
					if state.Turn != nil {
						state.Turn.Dirty = true
					}
				}
				break
			}
			state.Offset += int64(len(line))
			result.Bytes += int64(len(line))
			if state.DiscardLine {
				state.DiscardLine = false
				continue
			}
			if len(line) > 2<<20 {
				if state.Turn != nil {
					state.Turn.Dirty = true
				}
				continue
			}
			acceptLine(&state, thread.ID, line, &result.Turns)
		}
		f.Close()
		result.State.Files[path] = state
	}
	return result, nil
}
func fileIdentity(info os.FileInfo) string {
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer && !value.IsNil() {
		value = value.Elem()
		if value.Kind() == reflect.Struct {
			ino, dev := value.FieldByName("Ino"), value.FieldByName("Dev")
			if ino.IsValid() && dev.IsValid() {
				return fmt.Sprintf("%v:%v", dev, ino)
			}
		}
	}
	return info.Name()
}
func (c *Collector) allowedPath(path string) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	for _, dir := range []string{"sessions", "archived_sessions"} {
		root, err := filepath.EvalSymlinks(filepath.Join(c.home, dir))
		if err != nil {
			continue
		}
		relative, err := filepath.Rel(root, real)
		if err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) && filepath.Ext(real) == ".jsonl" {
			return real, nil
		}
	}
	return "", fmt.Errorf("不是Codex会话目录内的日志")
}
func cloneFileState(state FileState) FileState {
	if state.Last != nil {
		value := *state.Last
		state.Last = &value
	}
	if state.Turn != nil {
		value := *state.Turn
		value.WrapperCalls = map[string]bool{}
		for id, v := range state.Turn.WrapperCalls {
			value.WrapperCalls[id] = v
		}
		value.NativeExpected = append([]string{}, state.Turn.NativeExpected...)
		value.Calls = map[string]Call{}
		for id, call := range state.Turn.Calls {
			value.Calls[id] = call
		}
		value.Order = append([]string{}, state.Turn.Order...)
		state.Turn = &value
	}
	return state
}

type record struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}
type event struct {
	Item   json.RawMessage `json:"item"`
	Type   string          `json:"type"`
	TurnID string          `json:"turn_id"`
	Model  string          `json:"model"`
	Info   *struct {
		Total *Totals `json:"total_token_usage"`
		Last  *Totals `json:"last_token_usage"`
	} `json:"info"`
}

func acceptLine(state *FileState, threadID string, line []byte, turns *[]Turn) {
	var r record
	if err := json.Unmarshal(line, &r); err != nil {
		if state.Turn != nil {
			state.Turn.Dirty = true
		}
		return
	}
	if r.Type == "event_msg" || r.Type == "turn_context" {
		var e event
		if err := json.Unmarshal(r.Payload, &e); err != nil {
			if state.Turn != nil {
				state.Turn.Dirty = true
			}
			return
		}
		if r.Type == "turn_context" {
			if state.Turn != nil && (e.TurnID == "" || state.Turn.ID == e.TurnID) {
				state.Turn.Model = e.Model
			}
			return
		}
		switch e.Type {
		case "item_completed":
			if state.Turn != nil {
				acceptNativeItem(state.Turn, e.Item)
			}
		case "task_started":
			state.Turn = &Turn{ThreadID: threadID, ID: e.TurnID, StartedAt: r.Timestamp, Baseline: state.Last, Calls: map[string]Call{}, Order: []string{}}
		case "token_count":
			if e.Info == nil || !validTotals(e.Info.Total) {
				return
			}
			next := e.Info.Total
			// 新会话首个累计值等于该次实际用量，才有证据确认起点为零。
			if state.Last == nil && state.Turn != nil && e.Info.Last != nil && sameTotals(next, e.Info.Last) {
				state.Turn.Baseline = &Totals{Cached: zeroPointer(next.Cached), Reasoning: zeroPointer(next.Reasoning)}
			}
			if state.Last != nil && (next.Input < state.Last.Input || next.Output < state.Last.Output || decreasedOptional(next.Cached, state.Last.Cached) || decreasedOptional(next.Reasoning, state.Last.Reasoning)) {
				if state.Turn != nil {
					state.Turn.Dirty = true
				}
			}
			state.Last = next
			if state.Turn != nil {
				state.Turn.End = next
				if state.Turn.Completed {
					*turns = append(*turns, *state.Turn)
				}
			}
		case "task_complete":
			if state.Turn != nil && e.TurnID == state.Turn.ID {
				state.Turn.Completed = true
				state.Turn.FinishedAt = r.Timestamp
				*turns = append(*turns, *state.Turn)
			}
		case "turn_aborted":
			if state.Turn != nil {
				state.Turn.Dirty = true
			}
		}
		return
	}
	if r.Type != "response_item" || state.Turn == nil {
		return
	}
	acceptItem(state.Turn, r.Payload)
}
func validTotals(t *Totals) bool {
	return t != nil && t.Input >= 0 && t.Output >= 0 && t.Input <= 1000000000000 && t.Output <= 1000000000000 && (t.Cached == nil || *t.Cached >= 0 && *t.Cached <= t.Input) && (t.Reasoning == nil || *t.Reasoning >= 0 && *t.Reasoning <= t.Output)
}
func decreasedOptional(next, prev *int64) bool { return next != nil && prev != nil && *next < *prev }

// Delta 用累计差值取本轮真实消耗；缺失起点或计数器重置时不猜测。
func Delta(turn *Turn) *Totals {
	if turn == nil || turn.Dirty || !turn.Completed || turn.ID == "" || turn.Model == "" || turn.NativeReceived != len(turn.NativeExpected) || turn.Baseline == nil || turn.End == nil || !validTotals(turn.Baseline) || !validTotals(turn.End) {
		return nil
	}
	input, output := turn.End.Input-turn.Baseline.Input, turn.End.Output-turn.Baseline.Output
	if input < 0 || output < 0 {
		return nil
	}
	delta := &Totals{Input: input, Output: output}
	if turn.Baseline.Cached != nil && turn.End.Cached != nil {
		value := *turn.End.Cached - *turn.Baseline.Cached
		if value < 0 || value > input {
			return nil
		}
		delta.Cached = &value
	}
	if turn.Baseline.Reasoning != nil && turn.End.Reasoning != nil {
		value := *turn.End.Reasoning - *turn.Baseline.Reasoning
		if value < 0 || value > output {
			return nil
		}
		delta.Reasoning = &value
	}
	return delta
}

func zeroPointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	zero := int64(0)
	return &zero
}
func sameTotals(a, b *Totals) bool {
	return validTotals(a) && validTotals(b) && a.Input == b.Input && a.Output == b.Output && sameOptional(a.Cached, b.Cached) && sameOptional(a.Reasoning, b.Reasoning)
}
func sameOptional(a, b *int64) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

// UnmarshalJSON 保留真实总量字段的存在性，缺失输入/输出不能默认成实测零。
func (t *Totals) UnmarshalJSON(raw []byte) error {
	var value struct {
		Input     *int64 `json:"input_tokens"`
		Output    *int64 `json:"output_tokens"`
		Cached    *int64 `json:"cached_input_tokens"`
		Reasoning *int64 `json:"reasoning_output_tokens"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	if value.Input == nil || value.Output == nil {
		return fmt.Errorf("Codex计量缺少输入或输出总量")
	}
	t.Input = *value.Input
	t.Output = *value.Output
	t.Cached = value.Cached
	t.Reasoning = value.Reasoning
	return nil
}

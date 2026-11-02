package codex

import (
	"encoding/json"
	"regexp"
	"strings"
)

type item struct {
	Type      string          `json:"type"`
	CallID    string          `json:"call_id"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Namespace string          `json:"namespace"`
	Output    json.RawMessage `json:"output"`
	Input     string          `json:"input"`
}

var toolActions = map[string]string{
	"character_context":        "context",
	"memory_search":            "search",
	"memory_record":            "note",
	"bilibili_status":          "account",
	"bilibili_comments":        "comments",
	"bilibili_prepare_reply":   "draft",
	"bilibili_prepare_dynamic": "dynamic-draft",
}

func robotAction(name, namespace string) string {
	for _, prefix := range []string{"mcp__gpt_robot__", "mcp__gpt_robot.", "gpt_robot."} {
		if strings.HasPrefix(name, prefix) {
			return toolActions[strings.TrimPrefix(name, prefix)]
		}
	}
	if namespace == "mcp__gpt_robot" || namespace == "gpt_robot" {
		return toolActions[name]
	}
	return ""
}
func acceptItem(turn *Turn, raw json.RawMessage) {
	var i item
	if err := json.Unmarshal(raw, &i); err != nil {
		turn.Dirty = true
		return
	}
	switch i.Type {
	case "function_call", "custom_tool_call":
		if len(turn.Order) >= 512 {
			turn.Dirty = true
			return
		}
		if i.Name == "exec" || i.Name == "functions.exec" {
			actions, ok := codeModeActions(i.Input)
			if !ok {
				turn.Dirty = true
				return
			}
			id := i.CallID
			if id == "" {
				id = i.ID
			}
			if id == "" {
				turn.Dirty = true
				return
			}
			if turn.WrapperCalls == nil {
				turn.WrapperCalls = map[string]bool{}
			}
			turn.WrapperCalls[id] = true
			turn.NativeExpected = append(turn.NativeExpected, actions...)
			return
		}
		action := robotAction(i.Name, i.Namespace)
		if action == "" {
			action = robotWrapperAction(i.Name, i.Input)
		}
		if action == "" {
			turn.Dirty = true
			return
		} // 命令、文件、浏览器等开发/混合轮次全部排除。
		id := i.CallID
		if id == "" {
			id = i.ID
		}
		if id == "" {
			turn.Dirty = true
			return
		}
		if _, exists := turn.Calls[id]; exists {
			turn.Dirty = true
			return
		}
		turn.Calls[id] = Call{Action: action}
		turn.Order = append(turn.Order, id)
	case "function_call_output", "custom_tool_call_output":
		id := i.CallID
		if id == "" {
			id = i.ID
		}
		if turn.WrapperCalls[id] {
			return
		}
		call, ok := turn.Calls[id]
		if !ok {
			turn.Dirty = true
			return
		}
		operationID := operationIDFromOutput(i.Output, 0)
		if operationID == "" {
			turn.Dirty = true
			return
		}
		call.OperationID = operationID
		turn.Calls[id] = call
	}
}

// 只识别工具回执的固定封装，不递归搜索任意正文中的operationId，避免误关联。
func operationIDFromOutput(raw json.RawMessage, depth int) string {
	if depth > 3 || len(raw) == 0 {
		return ""
	}
	var value struct {
		OperationID string                     `json:"operationId"`
		Meta        map[string]json.RawMessage `json:"_meta"`
		Content     []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &value) == nil {
		if value.OperationID != "" {
			return value.OperationID
		}
		var id string
		if json.Unmarshal(value.Meta["gpt-robot/operationId"], &id) == nil && id != "" {
			return id
		}
		for _, content := range value.Content {
			if content.Type == "text" {
				if id := operationIDFromOutput(json.RawMessage(content.Text), depth+1); id != "" {
					return id
				}
			}
		}
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		for _, block := range blocks {
			if block.Type == "text" {
				if id := operationIDFromOutput(json.RawMessage(block.Text), depth+1); id != "" {
					return id
				}
			}
		}
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return operationIDFromOutput(json.RawMessage(text), depth+1)
	}
	return ""
}

// 只接纳可静态证明是单次机器人调用的代码模式封装；任意脚本、命令或多工具编排仍排除。
var directRobotWrapper = regexp.MustCompile(`(?s)^\s*text\(await tools\.(mcp__gpt_robot__[A-Za-z0-9_]+)\((.*)\)\);?\s*$`)
var assignedRobotWrapper = regexp.MustCompile(`(?s)^\s*const ([A-Za-z_][A-Za-z0-9_]*)\s*=\s*await tools\.(mcp__gpt_robot__[A-Za-z0-9_]+)\((.*)\);\s*text\(([A-Za-z_][A-Za-z0-9_]*)\);?\s*$`)

func robotWrapperAction(name, input string) string {
	if name != "exec" && name != "functions.exec" {
		return ""
	}
	if parts := directRobotWrapper.FindStringSubmatch(input); parts != nil && json.Valid([]byte(parts[2])) {
		return robotAction(parts[1], "")
	}
	if parts := assignedRobotWrapper.FindStringSubmatch(input); parts != nil && parts[1] == parts[4] && json.Valid([]byte(parts[3])) {
		return robotAction(parts[2], "")
	}
	return ""
}

func acceptNativeItem(turn *Turn, raw json.RawMessage) {
	var value struct {
		Type   string          `json:"type"`
		ID     string          `json:"id"`
		Server string          `json:"server"`
		Tool   string          `json:"tool"`
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(raw, &value) != nil {
		turn.Dirty = true
		return
	}
	if value.Type != "McpToolCall" && value.Type != "mcpToolCall" {
		return
	}
	if len(turn.NativeExpected) == 0 {
		return
	}
	action := toolActions[value.Tool]
	if value.Server != "gpt_robot" || action == "" || turn.NativeReceived >= len(turn.NativeExpected) || turn.NativeExpected[turn.NativeReceived] != action {
		turn.Dirty = true
		return
	}
	operationID := operationIDFromOutput(value.Result, 0)
	if operationID == "" {
		turn.Dirty = true
		return
	}
	if _, exists := turn.Calls[value.ID]; exists {
		turn.Dirty = true
		return
	}
	turn.Calls[value.ID] = Call{Action: action, OperationID: operationID}
	turn.Order = append(turn.Order, value.ID)
	turn.NativeReceived++
}

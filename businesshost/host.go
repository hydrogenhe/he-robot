// Package businesshost 运行只读业务专用Codex会话，不复用开发聊天上下文。
package businesshost

import (
	"context"
	"encoding/json"
	"fmt"
	"gpt-robot/model"
	"time"
)

type ReadInput struct {
	Root        string
	Video       string
	MemoryQuery string
}
type ToolResult struct {
	Tool        string `json:"tool"`
	OperationID string `json:"operationId"`
	Count       int    `json:"count"`
}
type Usage struct {
	Input  int64 `json:"inputTokens"`
	Output int64 `json:"outputTokens"`
	Cached int64 `json:"cachedInputTokens"`
	Total  int64 `json:"totalTokens"`
}
type Result struct {
	Ephemeral bool         `json:"ephemeral"`
	ThreadID  string       `json:"threadId"`
	TurnID    string       `json:"turnId"`
	Model     string       `json:"model"`
	Status    string       `json:"status"`
	Tools     []ToolResult `json:"tools"`
	Calls     []Usage      `json:"calls"`
	Total     Usage        `json:"total"`
}

// Read 只运行读取评论及搜索记忆，各一次；失败即返回，不重新运行模型或平台请求。
func Read(ctx context.Context, input *ReadInput) (*Result, error) {
	if input == nil || input.Root == "" || !model.ValidBV(input.Video) || !model.Text(input.MemoryQuery, 100) {
		return nil, model.Fail("INPUT", "需要有效视频BV号和1–100字记忆关键词")
	}
	runCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	c, work, overrides, cleanup, err := openSession(runCtx, input.Root, false)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	threadInput := map[string]any{
		"ephemeral":             true,
		"cwd":                   work,
		"sandbox":               "read-only",
		"approvalPolicy":        "never",
		"baseInstructions":      businessInstructions,
		"developerInstructions": "仅调用已提供的bilibili_comments和memory_search，每项一次，完成后简短回答。",
		"config":                overrides,
	}
	if err := c.send(3, "thread/start", threadInput); err != nil {
		return nil, err
	}
	raw, err := c.wait(runCtx, 3)
	if err != nil {
		return nil, err
	}
	var thread struct {
		Thread struct {
			ID        string `json:"id"`
			Ephemeral bool   `json:"ephemeral"`
		} `json:"thread"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(raw, &thread); err != nil {
		return nil, err
	}
	if !thread.Thread.Ephemeral {
		return nil, fmt.Errorf("业务宿主未接受临时会话，已停止，禁止使用持久聊天回退")
	}
	result := &Result{Ephemeral: true, ThreadID: thread.Thread.ID, Model: thread.Model, Tools: []ToolResult{}, Calls: []Usage{}}
	commentsArgs, _ := json.Marshal(map[string]any{"bvid": input.Video, "page": 1})
	memoryArgs, _ := json.Marshal(map[string]any{"query": input.MemoryQuery})
	text := fmt.Sprintf("读取视频评论然后查询记忆，各一次。工具名称与参数已确定，直接执行下面两行，无需发现工具或查询ALL_TOOLS：\ntext(await tools.mcp__gpt_robot__bilibili_comments(%s));\ntext(await tools.mcp__gpt_robot__memory_search(%s));\n只读取，不生成草稿、不发送、不写记忆；完成后用一句中文报告评论数和记忆匹配数。", commentsArgs, memoryArgs)
	turnInput := map[string]any{"threadId": result.ThreadID, "input": []any{map[string]any{"type": "text", "text": text}}}
	if err := c.send(4, "turn/start", turnInput); err != nil {
		return result, err
	}
	raw, err = c.wait(runCtx, 4)
	if err != nil {
		return result, err
	}
	var turn struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(raw, &turn); err != nil {
		return result, err
	}
	result.TurnID = turn.Turn.ID
	for {
		message, err := c.next(runCtx)
		if err != nil {
			return result, err
		}
		if len(message.ID) > 0 && message.Method != "" {
			return result, fmt.Errorf("业务宿主请求额外审批或客户端动作，已停止")
		}
		done, err := acceptNotification(result, &message)
		if err != nil {
			return result, err
		}
		if done {
			break
		}
	}
	if result.Status != "completed" || len(result.Tools) != 2 || result.Tools[0].Tool != "bilibili_comments" || result.Tools[1].Tool != "memory_search" {
		return result, fmt.Errorf("只读业务未按预期完成，禁止盲目重试")
	}
	return result, nil
}
func businessOverrides(raw json.RawMessage) (map[string]any, error) {
	var skills struct {
		Data []struct {
			Skills []struct {
				Path string `json:"path"`
			} `json:"skills"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &skills); err != nil {
		return nil, err
	}
	disabled := []any{}
	seen := map[string]bool{}
	for _, row := range skills.Data {
		for _, skill := range row.Skills {
			if skill.Path != "" && !seen[skill.Path] {
				disabled = append(disabled, map[string]any{"path": skill.Path, "enabled": false})
				seen[skill.Path] = true
			}
		}
	}
	result := map[string]any{"skills.config": disabled, "skills.max_context_tokens": 1}
	return result, nil
}
func acceptNotification(result *Result, message *rpcMessage) (bool, error) {
	switch message.Method {
	case "thread/tokenUsage/updated":
		var value struct {
			TokenUsage struct {
				Last  Usage `json:"last"`
				Total Usage `json:"total"`
			} `json:"tokenUsage"`
		}
		if err := json.Unmarshal(message.Params, &value); err != nil {
			return false, err
		}
		if len(result.Calls) > 0 && value.TokenUsage.Total == result.Total {
			return false, nil
		}
		result.Calls = append(result.Calls, value.TokenUsage.Last)
		result.Total = value.TokenUsage.Total
	case "item/completed":
		var value struct {
			Item struct {
				Type   string `json:"type"`
				Server string `json:"server"`
				Tool   string `json:"tool"`
				Status string `json:"status"`
				Result struct {
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"result"`
			} `json:"item"`
		}
		if err := json.Unmarshal(message.Params, &value); err != nil {
			return false, err
		}
		item := value.Item
		if item.Type == "mcpToolCall" {
			if item.Server != "gpt_robot" || (item.Tool != "bilibili_comments" && item.Tool != "memory_search") || item.Status != "completed" {
				return false, fmt.Errorf("业务工具未完成或超出范围")
			}
			tool := ToolResult{Tool: item.Tool}
			for _, content := range item.Result.Content {
				if content.Type != "text" {
					continue
				}
				var data struct {
					OperationID string            `json:"operationId"`
					Comments    []json.RawMessage `json:"comments"`
					Notes       []json.RawMessage `json:"notes"`
				}
				if json.Unmarshal([]byte(content.Text), &data) == nil {
					tool.OperationID = data.OperationID
					if item.Tool == "bilibili_comments" {
						tool.Count = len(data.Comments)
					} else {
						tool.Count = len(data.Notes)
					}
				}
			}
			if !model.ValidID(tool.OperationID) {
				return false, fmt.Errorf("业务回执缺少操作ID")
			}
			result.Tools = append(result.Tools, tool)
		} else if item.Type == "commandExecution" || item.Type == "fileChange" || item.Type == "dynamicToolCall" || item.Type == "webSearch" {
			return false, fmt.Errorf("业务调用了非业务工具，已停止")
		}
	case "turn/completed":
		var value struct {
			Turn struct {
				Status string `json:"status"`
			} `json:"turn"`
		}
		if err := json.Unmarshal(message.Params, &value); err != nil {
			return false, err
		}
		result.Status = value.Turn.Status
		return true, nil
	}
	return false, nil
}

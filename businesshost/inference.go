package businesshost

import (
	"context"
	"encoding/json"
	"fmt"
	"gpt-robot/model"
	"time"
)

// CodexBackend 使用本机Codex临时会话，不持久保存业务聊天。
type CodexBackend struct {
	root   string
	model  string
	effort string
}
type CodexBackendInterface interface {
	GenerateJSON(context.Context, string, []byte) (*model.GeneratedReply, error)
	OpenConversation(context.Context, string) (JSONSession, error)
	GenerateImages(context.Context, string, []byte, []string) (*ImageGenerationResult, error)
}

var _ CodexBackendInterface = (*CodexBackend)(nil)
var _ JSONBackend = (*CodexBackend)(nil)

func NewCodexBackend(root string, models ...string) *CodexBackend {
	backend := &CodexBackend{root: root}
	if len(models) > 0 {
		backend.model = models[0]
	}
	if len(models) > 1 {
		backend.effort = models[1]
	}
	return backend
}

// GenerateJSON 复用临时、禁工具的宿主协议，回复与日记各自校验业务结构。
func (g *CodexBackend) GenerateJSON(ctx context.Context, instruction string, body []byte) (*model.GeneratedReply, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	session, err := g.OpenConversation(ctx, instruction)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	return session.GenerateJSON(ctx, instruction, body)
}

type codexConversation struct {
	c        *connection
	threadID string
	model    string
	effort   string
	cleanup  func()
	sequence int
	last     Usage
}

var _ JSONSession = (*codexConversation)(nil)

func (g *CodexBackend) OpenConversation(ctx context.Context, instruction string) (JSONSession, error) {
	c, work, overrides, cleanup, err := openSession(ctx, g.root, true)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			cleanup()
		}
	}()
	params := map[string]any{"ephemeral": true, "allowProviderModelFallback": false, "dynamicTools": []any{}, "environments": []any{}, "cwd": work, "sandbox": "read-only", "approvalPolicy": "never", "baseInstructions": instruction, "developerInstructions": "只生成一次请求的JSON，不使用工具。", "config": overrides}
	if g.model != "" {
		params["model"] = g.model
	}
	if g.effort != "" {
		overrides["model_reasoning_effort"] = g.effort
	}
	if err := c.send(3, "thread/start", params); err != nil {
		return nil, err
	}
	raw, err := c.wait(ctx, 3)
	if err != nil {
		return nil, err
	}
	var thread struct {
		Thread struct {
			ID        string `json:"id"`
			Ephemeral bool   `json:"ephemeral"`
		} `json:"thread"`
		Model   string `json:"model"`
		Sandbox struct {
			Type string `json:"type"`
		} `json:"sandbox"`
		ApprovalPolicy  string `json:"approvalPolicy"`
		ReasoningEffort string `json:"reasoningEffort"`
	}
	if err := json.Unmarshal(raw, &thread); err != nil {
		return nil, err
	}
	if !thread.Thread.Ephemeral {
		return nil, fmt.Errorf("生成宿主未接受临时会话，已停止，禁止回退持久聊天")
	}
	if thread.Sandbox.Type != "readOnly" || thread.ApprovalPolicy != "never" {
		return nil, model.Fail("HARNESS_POLICY", "宿主未接受只读与禁止审批策略，未提交生成")
	}
	if g.effort != "" && thread.ReasoningEffort != g.effort {
		return nil, model.Fail("MODEL_MISMATCH", "Codex未接受所选思考档位，未提交生成")
	}
	if g.model != "" && thread.Model != g.model {
		return nil, model.Fail("MODEL_MISMATCH", "Codex未接受所选模型，已停止，禁止回退")
	}
	if err := verifyGenerationMCP(ctx, c, thread.Thread.ID); err != nil {
		return nil, err
	}
	session := &codexConversation{c: c, threadID: thread.Thread.ID, model: thread.Model, effort: g.effort, cleanup: cleanup, sequence: 100}
	ok = true
	return session, nil
}

func (s *codexConversation) Close() { s.cleanup() }

func (s *codexConversation) GenerateJSON(ctx context.Context, instruction string, body []byte) (*model.GeneratedReply, error) {
	if len(body) > model.ReplyContextMaxBytes || len(instruction)+len(body) > replyRequestMaxBytes {
		return nil, model.Fail("CONTEXT_BUDGET", "模型输入超过业务预算")
	}
	inputs := []any{map[string]any{"type": "text", "text": string(body)}}
	return s.generateInputs(ctx, inputs)
}

func (s *codexConversation) generateInputs(ctx context.Context, inputs []any) (*model.GeneratedReply, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	c := s.c
	turnParams := map[string]any{"threadId": s.threadID, "input": inputs}
	if s.effort != "" {
		turnParams["effort"] = s.effort
	}
	s.sequence++
	if err := c.send(s.sequence, "turn/start", turnParams); err != nil {
		return nil, err
	}
	raw, err := c.wait(ctx, s.sequence)
	if err != nil {
		return nil, err
	}
	var turn struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(raw, &turn); err != nil {
		return nil, err
	}
	result := &model.GeneratedReply{}
	tracking := &Result{ThreadID: s.threadID, TurnID: turn.Turn.ID, Model: s.model}
	reply := ""
	for {
		message, nextErr := c.next(ctx)
		if nextErr != nil {
			return result, nextErr
		}
		if len(message.ID) > 0 && message.Method != "" {
			return result, fmt.Errorf("生成宿主请求额外动作，已停止")
		}
		if message.Method == "item/completed" {
			var value struct {
				Item struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"item"`
			}
			if err := json.Unmarshal(message.Params, &value); err != nil {
				return result, err
			}
			switch value.Item.Type {
			case "agentMessage":
				reply = value.Item.Text
			case "reasoning", "userMessage":
			default:
				return result, fmt.Errorf("生成宿主执行了非文本动作 %s，已停止", value.Item.Type)
			}
		}
		done, notifyErr := acceptNotification(tracking, &message)
		if len(tracking.Calls) > 0 {
			cached := tracking.Total.Cached
			cached = tracking.Total.Cached - s.last.Cached
			result.Usage = &model.TokenUsage{Provider: "codex", Model: tracking.Model, RequestID: tracking.ThreadID + "/" + tracking.TurnID, ThreadID: tracking.ThreadID, TurnID: tracking.TurnID, Input: tracking.Total.Input - s.last.Input, CachedInput: &cached, Output: tracking.Total.Output - s.last.Output, Source: "codex_app_server"}
			if result.Usage.Input < 0 || result.Usage.Output < 0 || cached < 0 || cached > result.Usage.Input {
				result.Usage = nil
				return result, model.Fail("HARNESS_USAGE", "会话用量回执无效，未自动重试")
			}
		}
		if notifyErr != nil {
			return result, notifyErr
		}
		if done {
			break
		}
	}
	if tracking.Status != "completed" {
		return result, fmt.Errorf("模型生成未完成，不自动重试")
	}

	result.Text = reply
	s.last = tracking.Total
	return result, nil
}

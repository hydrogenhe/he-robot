package businesshost

import (
	"context"
	"encoding/json"
	"gpt-robot/model"
)

// generationOverrides 读取有效配置后逐项关闭所有MCP和插件，仅改变本次线程。
func generationOverrides(ctx context.Context, c *connection, work string, overrides map[string]any) error {
	if err := c.send(11, "config/read", map[string]any{"cwd": work, "includeLayers": false}); err != nil {
		return err
	}
	raw, err := c.wait(ctx, 11)
	if err != nil {
		return err
	}
	var result struct {
		Config map[string]json.RawMessage `json:"config"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Config == nil {
		return model.Fail("HARNESS_POLICY", "无法核验宿主安全配置，未提交生成")
	}
	for _, section := range []string{"mcp_servers", "plugins"} {
		var entries map[string]json.RawMessage
		if data := result.Config[section]; len(data) > 0 && string(data) != "null" {
			if err := json.Unmarshal(data, &entries); err != nil {
				return model.Fail("HARNESS_POLICY", "宿主安全配置格式不兼容")
			}
		}
		disabled := map[string]any{}
		for name := range entries {
			disabled[name] = map[string]any{"enabled": false}
		}
		overrides[section] = disabled
	}
	overrides["features.code_mode_host"] = false
	overrides["features.shell_tool"] = false
	overrides["features.apps"] = false
	overrides["features.hooks"] = false
	// 项目在远小于此值时主动轮换，避免宿主自动压缩触发额外模型请求。
	overrides["model_auto_compact_token_limit"] = 1000000000
	return nil
}

// verifyGenerationMCP 在发给模型前核对当前线程的实际MCP工具清单，非空/分页/无法验证均拒绝。
func verifyGenerationMCP(ctx context.Context, c *connection, thread string) error {
	if err := c.send(12, "mcpServerStatus/list", map[string]any{"threadId": thread, "limit": 100, "detail": "toolsAndAuthOnly"}); err != nil {
		return err
	}
	raw, err := c.wait(ctx, 12)
	if err != nil {
		return err
	}
	var status struct {
		Data []struct {
			Tools map[string]json.RawMessage `json:"tools"`
		} `json:"data"`
		NextCursor *string `json:"nextCursor"`
	}
	if json.Unmarshal(raw, &status) != nil || status.Data == nil || status.NextCursor != nil {
		return model.Fail("HARNESS_POLICY", "无法确认禁用工具，未提交生成")
	}
	for _, server := range status.Data {
		if len(server.Tools) > 0 {
			return model.Fail("HARNESS_POLICY", "宿主仍暴露MCP工具，未提交生成")
		}
	}
	return nil
}

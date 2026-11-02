package businesshost

import (
	"context"
	"encoding/json"
	"gpt-robot/model"
	"time"
)

// CodexModels 只读取本机程序模型目录，不提交业务输入或调用推理。
func CodexModels(ctx context.Context, root string) ([]model.ModelChoice, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	c, _, _, cleanup, err := openSession(ctx, root, true)
	if err != nil {
		return nil, model.Fail("HARNESS_CONNECT", "无法读取本机Codex模型列表")
	}
	defer cleanup()
	params := map[string]any{"limit": 100, "includeHidden": false}
	if err := c.send(3, "model/list", params); err != nil {
		return nil, model.Fail("HARNESS_PROTOCOL", "读取模型目录失败")
	}
	raw, err := c.wait(ctx, 3)
	if err != nil {
		return nil, model.Fail("HARNESS_PROTOCOL", "此Codex版本无法读取模型目录")
	}
	var response struct {
		Data []struct {
			ID          string `json:"id"`
			Model       string `json:"model"`
			DisplayName string `json:"displayName"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return nil, model.Fail("HARNESS_PROTOCOL", "模型目录无法解析")
	}
	choices := []model.ModelChoice{}
	for _, item := range response.Data {
		id := item.Model
		if id == "" {
			id = item.ID
		}
		if model.Text(id, 100) {
			choice := model.ModelChoice{ID: id, Name: item.DisplayName}
			choices = append(choices, choice)
		}
	}
	return choices, nil
}

package core

import (
	"context"
	"fmt"
	"gpt-robot/businesshost"
	"gpt-robot/model"
	"gpt-robot/usage/codex"
	"os"
	"path/filepath"
	"strings"
)

// collectNativeBusinessUsage 临时会话不留宿主聊天文件，改为采集本项目已保存的原生回执元数据。
func (s *Service) collectNativeBusinessUsage(ctx context.Context) error {
	entries, err := os.ReadDir(filepath.Join(s.Store.Root, ".private", "business-runs"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.nativeUsageFiles == nil {
		s.nativeUsageFiles = map[string]string{}
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") || !model.ValidID(strings.TrimSuffix(name, ".json")) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > 65536 {
			continue
		}
		fingerprint := fmt.Sprintf("%d/%d", info.ModTime().UnixNano(), info.Size())
		if s.nativeUsageFiles[name] == fingerprint {
			continue
		}
		var result businesshost.Result
		if _, err := s.Store.Read(ctx, ".private/business-runs/"+name, &result); err != nil {
			return err
		}
		if !result.Ephemeral {
			s.nativeUsageFiles[name] = fingerprint
			continue
		}
		if result.Status != "completed" || result.ThreadID+".json" != name || !model.ValidID(result.TurnID) || !model.Text(result.Model, 100) || len(result.Tools) != 2 || result.Tools[0].Tool != "bilibili_comments" || result.Tools[1].Tool != "memory_search" || len(result.Calls) == 0 {
			continue
		}
		usage := result.Total
		if usage.Input < 0 || usage.Output < 0 || usage.Cached < 0 || usage.Cached > usage.Input || usage.Total != usage.Input+usage.Output {
			continue
		}
		zero, cached := int64(0), usage.Cached
		start := &codex.Totals{Cached: &zero}
		end := &codex.Totals{Input: usage.Input, Output: usage.Output, Cached: &cached}
		calls := map[string]codex.Call{}
		order := []string{}
		valid := true
		s.operationsMu.Lock()
		for _, tool := range result.Tools {
			action := "comments"
			if tool.Tool == "memory_search" {
				action = "search"
			}
			op, ok := s.operations[tool.OperationID]
			if !ok || !model.ValidID(tool.OperationID) || op.Source != "mcp" || op.Action != action || op.Status == "running" {
				valid = false
				break
			}
			call := codex.Call{Action: action, OperationID: tool.OperationID}
			calls[tool.Tool] = call
			order = append(order, tool.Tool)
		}
		s.operationsMu.Unlock()
		if !valid {
			continue
		}
		turn := codex.Turn{ThreadID: result.ThreadID, ID: result.TurnID, Model: result.Model, Completed: true, Baseline: start, End: end, Calls: calls, Order: order, NativeExpected: order, NativeReceived: len(order)}
		if err := s.applyCodexTurn(ctx, &turn, "codex_app_server"); err != nil {
			return err
		}
		s.nativeUsageFiles[name] = fingerprint
	}
	return nil
}

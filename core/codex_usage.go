package core

import (
	"context"
	"fmt"
	"gpt-robot/model"
	"gpt-robot/usage/codex"
	"os"
	"path/filepath"
	"slices"
	"time"
)

func codexHome() string {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

// CollectCodexUsage 只由本地用量查询触发，不唤醒模型或平台，不后台轮询。
func (s *Service) CollectCodexUsage(ctx context.Context) (string, error) {
	s.collectionMu.Lock()
	defer s.collectionMu.Unlock()
	if err := s.collectNativeBusinessUsage(ctx); err != nil {
		return "临时业务会话计量暂不可读，已保留原账本", err
	}
	if s.codexCollector == nil {
		return "本机Codex用量来源不可用", nil
	}
	var earliest time.Time
	count := 0
	s.operationsMu.Lock()
	for _, op := range s.operations {
		if op.Source != "mcp" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, op.At)
		if err != nil {
			continue
		}
		count++
		if earliest.IsZero() || at.Before(earliest) {
			earliest = at
		}
	}
	s.operationsMu.Unlock()
	if count == 0 {
		return "尚无可关联的机器人MCP操作；开发聊天不会计入", nil
	}
	if count != s.discoveredOperationCount || time.Since(s.discoveredAt) >= 30*time.Second {
		threads, err := s.codexCollector.Discover(ctx, earliest)
		if err != nil {
			return "本机Codex计量索引暂不可读，已保留原有记录", err
		}
		eligible := threads[:0]
		for _, thread := range threads {
			if !s.excludedUsageThreads[thread.ID] {
				eligible = append(eligible, thread)
			}
		}
		threads = eligible
		known := map[string]bool{}
		for _, t := range threads {
			known[t.Path] = true
		}
		for path, state := range s.codexState.Files {
			if !known[path] && !s.excludedUsageThreads[state.ThreadID] {
				threads = append(threads, codex.Thread{ID: state.ThreadID, Path: path})
			}
		}
		s.codexThreads = threads
		s.discoveredAt = time.Now()
		s.discoveredOperationCount = count
	}
	input := codex.ScanInput{State: &s.codexState, Threads: s.codexThreads}
	batch, err := s.codexCollector.Scan(ctx, &input)
	if err != nil {
		return "本机Codex计量读取失败，未覆盖已有用量", err
	}
	for i := range batch.Turns {
		if err := s.applyCodexTurn(ctx, &batch.Turns[i], "codex_local"); err != nil {
			return "用量关联保存失败，下次查询会幂等续采", err
		}
	}
	// 无新增完整行不落盘；避免每次查看页面写同一份游标。
	if batch.Bytes > 0 {
		if err := s.Store.Write(ctx, "data/usage/codex-cursor.json", &batch.State); err != nil {
			return "采集游标未保存，下次幂等重放", err
		}
		s.codexState = batch.State
	}
	return "已按需增量读取本机真实计量；只接纳完整纯业务轮次，开发和混合轮次已排除", nil
}

func (s *Service) applyCodexTurn(ctx context.Context, turn *codex.Turn, source string) error {
	delta := codex.Delta(turn)
	if s.excludedUsageThreads[turn.ThreadID] {
		return nil
	}
	if delta == nil || len(turn.Order) == 0 || !model.ValidID(turn.ThreadID) || !model.ValidID(turn.ID) {
		return nil
	}
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	ids := []string{}
	for _, callID := range turn.Order {
		call := turn.Calls[callID]
		op, ok := s.operations[call.OperationID]
		// 只信任本项目保存的MCP流水和相同动作，不能从开发聊天的文本中猜ID。
		if !ok || op.Source != "mcp" || op.Action != call.Action || op.Status == "running" {
			return nil
		}
		if !slices.Contains(ids, op.ID) {
			ids = append(ids, op.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	requestID := turn.ThreadID + "/" + turn.ID
	ownerID := ids[0]
	key := usageRequestKey{provider: "codex-local", requestID: requestID}
	if existing, ok := s.usageRequests[key]; ok && existing != ownerID {
		return model.Fail("CONFLICT", "同一Codex轮次已有其他归属，拒绝重复计数")
	}
	for _, id := range ids {
		op := s.operations[id]
		if op.UsageOwner != "" && op.UsageOwner != ownerID {
			return model.Fail("CONFLICT", "操作已关联其他共享用量")
		}
		for _, u := range op.Usage {
			if u.Provider != "codex-local" || u.RequestID != requestID {
				return nil
			}
		} // 不能叠加手工上报和整轮采集。
	}
	usage := model.TokenUsage{
		RequestID:         requestID,
		Provider:          "codex-local",
		Model:             turn.Model,
		Input:             delta.Input,
		Output:            delta.Output,
		CachedInput:       delta.Cached,
		ReasoningOutput:   delta.Reasoning,
		Source:            source,
		ThreadID:          turn.ThreadID,
		TurnID:            turn.ID,
		Scope:             "business_turn",
		RelatedOperations: slices.Clone(ids),
	}
	for _, id := range ids {
		op := s.operations[id]
		unchanged := op.UsageOwner == ownerID && op.UsageThread == turn.ThreadID && op.UsageTurn == turn.ID
		op.UsageOwner = ownerID
		op.UsageThread = turn.ThreadID
		op.UsageTurn = turn.ID
		if id == ownerID {
			if len(op.Usage) == 1 && sameUsage(&op.Usage[0], &usage) {
				continue
			}
			if len(op.Usage) == 1 && (usage.Input < op.Usage[0].Input || usage.Output < op.Usage[0].Output) {
				return model.Fail("CONFLICT", "Codex累计用量回退，保留已有计量")
			}
			op.Usage = []model.TokenUsage{usage}
		} else if unchanged {
			continue
		}
		if err := s.Store.AppendOperation(ctx, &op); err != nil {
			return fmt.Errorf("保存Codex轮次关联失败: %w", err)
		}
		s.operations[id] = op
	}
	s.usageRequests[key] = ownerID
	return nil
}

func (s *Service) initializeUsageExclusions(ctx context.Context) error {
	var policy struct {
		ExcludedThreadIDs []string `json:"excludedThreadIds"`
	}
	if _, err := s.Store.Read(ctx, "config/usage.json", &policy); err != nil {
		return err
	}
	if len(policy.ExcludedThreadIDs) > 1000 {
		return model.Fail("CONFIG", "排除聊天数量超过1000")
	}
	s.excludedUsageThreads = map[string]bool{}
	for _, id := range policy.ExcludedThreadIDs {
		if !model.ValidID(id) {
			return model.Fail("CONFIG", "用量排除聊天ID格式不正确")
		}
		s.excludedUsageThreads[id] = true
	}
	return nil
}

package core_test

import (
	"encoding/json"
	"gpt-robot/core"
	"gpt-robot/http/handler"
	"gpt-robot/model"
	"sync"
	"testing"
)

func TestBusinessOperationsUsageAndRestart(t *testing.T) {
	s, f := setup(t)
	actions := handler.NewActions(s)
	result, err := actions.Execute(ctx, "context", json.RawMessage(`{}`), false)
	if err != nil {
		t.Fatal(err)
	}
	id := result.(map[string]any)["operationId"].(string)
	query := model.OperationQuery{}
	page, err := s.QueryOperations(ctx, &query)
	if err != nil || page.Total != 1 || page.Reported != 0 || page.Items[0].Status != "succeeded" || len(page.Items[0].Usage) != 0 {
		t.Fatalf("unknown usage must not be zero: %+v %v", page, err)
	}
	// 查询自身不产生日志；未获准的动作不会执行或被算成业务动作。
	if _, err := actions.Execute(ctx, "operations", json.RawMessage(`{}`), true); err != nil {
		t.Fatal(err)
	}
	if _, err := actions.Execute(ctx, "send", json.RawMessage(`{}`), false); err == nil {
		t.Fatal("MCP send allowed")
	}
	if _, err := actions.Execute(ctx, "search", json.RawMessage(`{"query":""}`), false); err == nil {
		t.Fatal("invalid search accepted")
	}
	cached, reasoning := int64(40), int64(5)
	usage := model.TokenUsage{
		RequestID:       "request-1",
		Provider:        "test-provider",
		Model:           "test-model",
		Input:           100,
		Output:          20,
		CachedInput:     &cached,
		ReasoningOutput: &reasoning,
	}
	report := model.UsageReport{
		OperationID: id,
		Usage:       usage,
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.ReportUsage(ctx, &report); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	page, err = s.QueryOperations(ctx, &query)
	if err != nil || page.Total != 2 || page.Reported != 1 || page.Input+page.Output != 120 || page.CachedInput != 40 {
		t.Fatalf("duplicate/cached counted twice: %+v %v", page, err)
	}
	report.Usage.Output = 21
	if err := s.ReportUsage(ctx, &report); err == nil {
		t.Fatal("conflicting update accepted")
	}
	start := model.OperationStart{
		Platform: "qq",
		Action:   "reply",
		Source:   "mcp",
	}
	qqID, err := s.StartOperation(ctx, &start)
	if err != nil {
		t.Fatal(err)
	}
	report.OperationID = qqID
	report.Usage.Output = 20
	if err := s.ReportUsage(ctx, &report); err == nil {
		t.Fatal("one inference charged to two operations")
	}
	restored := core.NewService(s.Store.Root, f)
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	platform := "qq"
	query.Platform = &platform
	page, err = restored.QueryOperations(ctx, &query)
	if err != nil || page.Total != 1 || page.Items[0].Status != "unknown" || page.Items[0].ErrorCode != "INTERRUPTED" {
		t.Fatalf("restart: %+v %v", page, err)
	}
	query.Platform = nil
	page, err = restored.QueryOperations(ctx, &query)
	if err != nil || page.Total != 3 || page.Reported != 1 || page.Input+page.Output != 120 {
		t.Fatalf("restored totals %+v %v", page, err)
	}
	no := false
	query.HasUsage = &no
	page, err = restored.QueryOperations(ctx, &query)
	if err != nil || page.Total != 2 || page.Reported != 0 {
		t.Fatal(page, err)
	}
	if len(f.counts()) != 0 {
		t.Fatal("local usage queried platform", f.counts())
	}
}
func TestHistoricalOperationsBackfillOnce(t *testing.T) {
	root := t.TempDir()
	store := core.NewStore(root)
	event, err := store.Event(ctx, "sent", "历史事实", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := core.NewService(root, nil)
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	query := model.OperationQuery{}
	page, err := s.QueryOperations(ctx, &query)
	if err != nil || page.Total != 1 || page.Items[0].ID != event.ID || page.Items[0].Source != "history" || page.Reported != 0 {
		t.Fatal(page, err)
	}
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	page, err = s.QueryOperations(ctx, &query)
	if err != nil || page.Total != 1 {
		t.Fatal(page, err)
	}
}
func TestUsageProtocolRejectsMissingAndInvalidTotals(t *testing.T) {
	s, _ := setup(t)
	actions := handler.NewActions(s)
	result, err := actions.Execute(ctx, "context", json.RawMessage(`{}`), false)
	if err != nil {
		t.Fatal(err)
	}
	id := result.(map[string]any)["operationId"].(string)
	for _, fields := range []string{`"output":1`, `"input":-1,"output":1`, `"input":1,"output":1,"cachedInput":2`} {
		body := json.RawMessage(`{"operationId":"` + id + `","requestId":"x","provider":"p","model":"m",` + fields + `}`)
		if _, err := actions.Execute(ctx, "usage-report", body, false); err == nil {
			t.Fatal("invalid totals accepted", fields)
		}
	}
	// 实测零与未获取必须能区分。
	body := json.RawMessage(`{"operationId":"` + id + `","requestId":"zero","provider":"p","model":"m","input":0,"output":0}`)
	if _, err := actions.Execute(ctx, "usage-report", body, false); err != nil {
		t.Fatal(err)
	}
	query := model.OperationQuery{}
	page, err := s.QueryOperations(ctx, &query)
	if err != nil || page.Reported != 1 || page.Input+page.Output != 0 {
		t.Fatal(page, err)
	}
}

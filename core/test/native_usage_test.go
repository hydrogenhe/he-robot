package core_test

import (
	"encoding/json"
	"gpt-robot/businesshost"
	"gpt-robot/core"
	"gpt-robot/http/handler"
	"gpt-robot/model"
	"testing"
)

func TestEphemeralBusinessUsageSharedCountingWithoutHostRollout(t *testing.T) {
	service, fake := setup(t)
	actions := handler.NewActions(service)
	raw, _ := json.Marshal(map[string]any{"bvid": bvid, "page": 1})
	comments, err := actions.Execute(ctx, "comments", raw, false)
	if err != nil {
		t.Fatal(err)
	}
	search, err := actions.Execute(ctx, "search", json.RawMessage(`{"query":"记忆"}`), false)
	if err != nil {
		t.Fatal(err)
	}
	commentsID := comments.(map[string]any)["operationId"].(string)
	searchID := search.(map[string]any)["operationId"].(string)
	usage := businesshost.Usage{Input: 100, Output: 10, Cached: 40, Total: 110}
	toolComments := businesshost.ToolResult{Tool: "bilibili_comments", OperationID: commentsID, Count: 1}
	toolSearch := businesshost.ToolResult{Tool: "memory_search", OperationID: searchID, Count: 0}
	result := businesshost.Result{Ephemeral: true, ThreadID: model.ID(), TurnID: model.ID(), Model: "native-fixture", Status: "completed", Tools: []businesshost.ToolResult{toolComments, toolSearch}, Calls: []businesshost.Usage{usage}, Total: usage}
	name := ".private/business-runs/" + result.ThreadID + ".json"
	if err := service.Store.Write(ctx, name, &result); err != nil {
		t.Fatal(err)
	}
	query := model.OperationQuery{}
	for range 2 {
		page, err := service.QueryOperations(ctx, &query)
		if err != nil || page.Reported != 2 || page.Input != 100 || page.Output != 10 || page.CachedInput != 40 {
			t.Fatal("ephemeral native usage missing or double counted", page, err)
		}
	}
	restored := core.NewService(service.Store.Root, fake)
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	filter := "search"
	filtered := model.OperationQuery{Action: &filter}
	page, err := restored.QueryOperations(ctx, &filtered)
	if err != nil || page.Total != 1 || page.Input != 100 || page.Output != 10 || len(page.Items[0].Usage[0].RelatedOperations) != 2 || page.Items[0].Usage[0].Source != "codex_app_server" {
		t.Fatal("restart/shared filter usage incorrect", page, err)
	}
	excluded := model.ID()
	if err := service.Store.Write(ctx, "config/usage.json", map[string]any{"excludedThreadIds": []string{excluded}}); err != nil {
		t.Fatal(err)
	}
	result.ThreadID = excluded
	result.Total.Input = 90000
	result.Total.Total = 90010
	if err := service.Store.Write(ctx, ".private/business-runs/"+excluded+".json", &result); err != nil {
		t.Fatal(err)
	}
	restored = core.NewService(service.Store.Root, fake)
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	page, err = restored.QueryOperations(ctx, &query)
	if err != nil || page.Input != 100 || page.Output != 10 {
		t.Fatal("development exclusion lost", page, err)
	}
}

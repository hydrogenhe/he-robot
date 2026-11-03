package handler_test

import (
	"context"
	"encoding/json"
	"gpt-robot/core"
	"gpt-robot/http/handler"
	"gpt-robot/model"
	"testing"
)

func TestMCPRejectsRobotManagementAndProactiveConfig(t *testing.T) {
	ctx := context.Background()
	s := core.NewService(t.TempDir(), nil)
	if err := s.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Store.Database.Close()
	manager, err := core.NewRobotManager(ctx, s.Store.Root, s)
	if err != nil {
		t.Fatal(err)
	}
	actions := handler.NewRobotActions(manager, handler.NewActions(s))
	for _, action := range []string{"robots-list", "robots-create", "robots-delete", "robots-restore", "proactive-config", "proactive-status"} {
		if _, err = actions.Execute(ctx, action, json.RawMessage(`{}`), false); err == nil {
			t.Fatal("MCP accepted admin action", action)
		}
	}
	if _, err = actions.Execute(ctx, "robots-create", json.RawMessage(`{"name":"独立002"}`), true); err != nil {
		t.Fatal(err)
	}
	if _, err = actions.Execute(ctx, "robots-delete", json.RawMessage(`{"id":"002","confirmId":"001"}`), true); err == nil {
		t.Fatal("wrong confirmation accepted")
	}
	if _, err = actions.Execute(ctx, "robots-delete", json.RawMessage(`{"id":"002","confirmId":"002"}`), true); err != nil {
		t.Fatal(err)
	}
	restore := model.RobotManagerInput{ID: "002"}
	if err = manager.Restore(ctx, &restore); err != nil {
		t.Fatal(err)
	}
	remove := model.RobotManagerInput{ID: "001", ConfirmID: "001"}
	if err = manager.SoftDelete(ctx, &remove, "002"); err != nil {
		t.Fatal(err)
	}
	if _, err = actions.Execute(ctx, "context", json.RawMessage(`{}`), false); err == nil {
		t.Fatal("MCP accessed soft-deleted 001")
	}
}

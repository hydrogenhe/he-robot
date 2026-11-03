package core_test

import (
	"gpt-robot/core"
	"gpt-robot/model"
	"gpt-robot/modelprovider"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRobotManagerIsolatesModelsCredentialsMemoryAndSoftDelete(t *testing.T) {
	primary, _ := setup(t)
	root := primary.Store.Root
	if err := primary.SavePersona(ctx, "001独有人设，喜欢植物。"); err != nil {
		t.Fatal(err)
	}
	credentials := model.Credentials{UID: "123", Cookies: map[string]string{"SESSDATA": "only-001", "bili_jct": "fixture"}}
	if err := primary.Store.Write(ctx, ".private/bilibili.json", &credentials); err != nil {
		t.Fatal(err)
	}
	config := modelprovider.New(root).Config()
	config.CodexModel = "only-001-model"
	config.CodexEffort = "low"
	if err := primary.Store.Write(ctx, ".private/model-provider.json", &config); err != nil {
		t.Fatal(err)
	}
	manager, err := core.NewRobotManager(ctx, root, primary)
	if err != nil {
		t.Fatal(err)
	}
	create := model.RobotManagerInput{Name: "独立朋友"}
	entry, err := manager.Create(ctx, &create)
	if err != nil || entry.ID != "002" {
		t.Fatal(entry, err)
	}
	second, request, cancel, err := manager.Resolve(ctx, "002")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	secondContext, err := second.Context(request)
	if err != nil || strings.Contains(secondContext.Persona, "001独有") {
		t.Fatal(secondContext, err)
	}
	if account, err := second.Credentials(request); err != nil || account != nil {
		t.Fatal("credentials copied", account, err)
	}
	var copied model.ModelProviderConfig
	if found, err := second.Store.Read(ctx, ".private/model-provider.json", &copied); err != nil || found && copied.CodexModel == config.CodexModel {
		t.Fatal("model copied", copied, err)
	}
	if primary.Store.Database.RobotID == second.Store.Database.RobotID {
		t.Fatal("shared database identity")
	}
	if err = second.SavePersona(ctx, "002自己的独立人设"); err != nil {
		t.Fatal(err)
	}
	primaryContext, err := primary.Context(ctx)
	if err != nil || !strings.Contains(primaryContext.Persona, "001独有") {
		t.Fatal("001 changed", primaryContext, err)
	}
	start := model.RobotManagerInput{ID: "002"}
	if err = manager.Control(ctx, &start, "start"); err != nil {
		t.Fatal(err)
	}
	status, _ := primary.RobotStatus(ctx)
	if status.Status != "running" {
		t.Fatal("002 controls affected 001")
	}
	remove := model.RobotManagerInput{ID: "002"}
	if err = manager.SoftDelete(ctx, &remove, "001"); err == nil {
		t.Fatal("missing confirmation accepted")
	}
	remove.ConfirmID = "002"
	if err = manager.SoftDelete(ctx, &remove, "002"); err == nil {
		t.Fatal("deleted current selection")
	}
	if err = manager.SoftDelete(ctx, &remove, "not-real"); err == nil {
		t.Fatal("invalid management selection accepted")
	}
	if err = manager.SoftDelete(ctx, &remove, "001"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = manager.Resolve(ctx, "002"); err == nil {
		t.Fatal("deleted robot still accessible")
	}
	if _, err = os.Stat(filepath.Join(second.Store.Root, "data/robots", second.Store.Database.RobotID, "memory.sqlite")); err != nil {
		t.Fatal("soft delete removed database", err)
	}
	second.Store.Database.Close()
	primary.Store.Database.Close()
	reopened := core.NewService(root, nil)
	if err = reopened.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	defer reopened.Store.Database.Close()
	manager, err = core.NewRobotManager(ctx, root, reopened)
	if err != nil {
		t.Fatal(err)
	}
	views, err := manager.List(ctx)
	if err != nil || len(views) != 2 || views[1].DeletedAt == "" {
		t.Fatal("soft delete lost after restart", views, err)
	}
	if err = manager.Control(ctx, &start, "start"); err == nil {
		t.Fatal("deleted robot started")
	}
	if err = manager.Restore(ctx, &start); err != nil {
		t.Fatal(err)
	}
	restored, request, cancel2, err := manager.Resolve(ctx, "002")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel2()
	defer restored.Store.Database.Close()
	restoredContext, err := restored.Context(request)
	if err != nil || !strings.Contains(restoredContext.Persona, "002自己的") {
		t.Fatal("restoration lost persona", restoredContext, err)
	}
	state, err := restored.RobotStatus(request)
	if err != nil || state.Status != "stopped" {
		t.Fatal("restoration auto-started", state, err)
	}
	entry, err = manager.Create(ctx, &create)
	if err != nil || entry.ID != "003" {
		t.Fatal("reused robot number", entry, err)
	}
}

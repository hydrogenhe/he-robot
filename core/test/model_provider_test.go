package core_test

import (
	"encoding/json"
	"gpt-robot/core"
	"gpt-robot/http/handler"
	"gpt-robot/model"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHarnessOnlyConfigAndRemovedAPIEntry(t *testing.T) {
	service, fake := setup(t)
	actions := handler.NewActions(service)
	for _, name := range []string{"model-status", "model-connect", "model-list"} {
		if _, err := actions.Execute(ctx, name, json.RawMessage(`{}`), false); err == nil {
			t.Fatal("MCP reached model settings")
		}
	}
	if _, err := actions.Execute(ctx, "model-config", json.RawMessage(`{"mode":"api"}`), true); err == nil {
		t.Fatal("removed API entry accepted")
	}
	invalid := model.ModelProviderInput{Mode: "api", Provider: "deepseek", DeepSeekModel: "deepseek-flash"}
	if _, err := service.SaveModelProviderConfig(ctx, &invalid); err == nil {
		t.Fatal("non-harness mode accepted")
	}
	input := model.ModelProviderInput{Mode: "local", Provider: "deepseek", DeepSeekModel: "deepseek-flash", HarnessEffort: "low"}
	if _, err := service.SaveModelProviderConfig(ctx, &input); err != nil {
		t.Fatal(err)
	}
	restored := core.NewService(service.Store.Root, fake)
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := restored.ModelProviderStatus(ctx)
	if err != nil || status.Provider != "deepseek" || status.Mode != "local" || status.HarnessEffort != "low" {
		t.Fatal(status, err)
	}
	raw, err := os.ReadFile(filepath.Join(service.Store.Root, ".private/model-provider.json"))
	if err != nil || strings.Contains(string(raw), "apiKey") || strings.Contains(string(raw), "openai") {
		t.Fatal("removed API fields remained", err)
	}
}

func TestLegacyConfigurationIsPrivatelyArchivedWithoutImplicitModelSwitch(t *testing.T) {
	service, fake := setup(t)
	legacy := map[string]any{"mode": "api", "provider": "deepseek", "apiKey": "old-private-value", "deepseekModel": "deepseek-flash", "codexModel": "old-unselected-model"}
	if err := service.Store.Write(ctx, ".private/model-provider.json", legacy); err != nil {
		t.Fatal(err)
	}
	restored := core.NewService(service.Store.Root, fake)
	if err := restored.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := restored.ModelProviderStatus(ctx)
	if err != nil || status.Mode != "local" || status.CodexModel != "" {
		t.Fatal("migration silently selected a billable model", status, err)
	}
	raw, _ := os.ReadFile(filepath.Join(service.Store.Root, ".private/model-provider.json"))
	if strings.Contains(string(raw), "old-private-value") {
		t.Fatal("legacy key remained active")
	}
	backup, _ := os.ReadFile(filepath.Join(service.Store.Root, ".private/archive/model-provider-before-harness-only.json"))
	if !strings.Contains(string(backup), "old-private-value") {
		t.Fatal("original private configuration not preserved")
	}
}

func TestMissingHarnessDoesNotReplaceCurrentHarness(t *testing.T) {
	service, _ := setup(t)
	input := model.ModelProviderInput{Mode: "local", Provider: "codex", CodexModel: "chosen-model", CodexEffort: "low"}
	if _, err := service.SaveModelProviderConfig(ctx, &input); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GPT_ROBOT_DSH_PATH", filepath.Join(t.TempDir(), "missing"))
	selection := model.HarnessConnectInput{Provider: "deepseek", DeepSeekModel: "deepseek-flash", HarnessEffort: "low"}
	if _, err := service.ConnectModelHarness(ctx, &selection); err == nil {
		t.Fatal("missing harness accepted")
	}
	status, _ := service.ModelProviderStatus(ctx)
	if status.Provider != "codex" || status.CodexModel != "chosen-model" || status.CodexEffort != "low" {
		t.Fatal("failed connection changed selection", status)
	}
}

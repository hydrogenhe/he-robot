package modelprovider_test

import (
	"context"
	"gpt-robot/model"
	"gpt-robot/modelprovider"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func policyHarness(t *testing.T) (*modelprovider.Router, string) {
	t.Helper()
	root := t.TempDir()
	fake := filepath.Join(root, "codex")
	capture := filepath.Join(root, "calls.jsonl")
	if err := os.WriteFile(fake, []byte(conversationHarnessScript), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_APP_RUNTIME_CLI_PATH", fake)
	t.Setenv("ROBOT_TEST_CAPTURE", capture)
	router := modelprovider.New(root)
	config := model.ModelProviderConfig{Mode: "local", Provider: "codex", CodexModel: "test-model", CodexEffort: "low", DeepSeekModel: "deepseek-flash"}
	if err := router.Configure(&config); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(router.CloseConversations)
	return router, capture
}

func TestHarnessWindowTimeTokenAndPendingBoundaries(t *testing.T) {
	for _, boundary := range []string{"idle", "age", "tokens", "unknown", "disabled"} {
		t.Run(boundary, func(t *testing.T) {
			t.Setenv("ROBOT_TEST_RETURN_MODEL", boundary)
			router, capture := policyHarness(t)
			policy := model.DefaultConversationConfig()
			policy.IdleSeconds = 60
			policy.MaxAgeSeconds = 120
			if boundary == "age" {
				policy.IdleSeconds = 3600
			}
			if boundary == "tokens" {
				policy.TotalTokens = 12000
			}
			if boundary == "disabled" {
				policy.Enabled = false
			}
			if err := router.ConfigureConversations(&policy); err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			router.ConversationNow = func() time.Time { return now }
			scope := model.ConversationScope{Platform: "future-platform", Account: "123", Peer: "456", Channel: "private-message"}
			for i := 0; i < 2; i++ {
				scope.RequestID = model.ID()
				if _, err := router.GenerateConversationJSON(context.Background(), &scope, "仅生成JSON", []byte(`{"message":"你好"}`)); err != nil {
					t.Fatal(err)
				}
				router.FinishConversation(&scope, true)
				if boundary == "idle" {
					now = now.Add(61 * time.Second)
				}
				if boundary == "age" {
					now = now.Add(121 * time.Second)
				}
			}
			raw, _ := os.ReadFile(capture)
			if strings.Count(string(raw), `"method": "turn/start"`) != 2 || strings.Count(string(raw), `"method": "thread/start"`) != 2 {
				t.Fatal("rotation or inference count incorrect")
			}
		})
	}
	router, capture := policyHarness(t)
	scope := model.ConversationScope{Platform: "another-platform", Account: "123", Peer: "456", Channel: "private-message", RequestID: model.ID()}
	if _, err := router.GenerateConversationJSON(context.Background(), &scope, "仅生成JSON", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := router.GenerateConversationJSON(context.Background(), &scope, "仅生成JSON", []byte(`{}`)); err == nil {
		t.Fatal("pending send allowed another generation")
	}
	router.FinishConversation(&scope, false)
	scope.RequestID = model.ID()
	if _, err := router.GenerateConversationJSON(context.Background(), &scope, "仅生成JSON", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	router.FinishConversation(&scope, true)
	raw, _ := os.ReadFile(capture)
	if strings.Count(string(raw), `"method": "turn/start"`) != 2 || strings.Count(string(raw), `"method": "thread/start"`) != 2 {
		t.Fatal("failed send window reused or inference repeated")
	}
}

func TestClosingHarnessWindowCancelsInFlightGeneration(t *testing.T) {
	t.Setenv("ROBOT_TEST_RETURN_MODEL", "block")
	router, capture := policyHarness(t)
	scope := model.ConversationScope{Platform: "next-platform", Account: "123", Peer: "456", Channel: "private-message", RequestID: model.ID()}
	finished := make(chan error, 1)
	go func() {
		_, err := router.GenerateConversationJSON(context.Background(), &scope, "仅生成JSON", []byte(`{}`))
		finished <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		raw, _ := os.ReadFile(capture)
		if strings.Contains(string(raw), `"method": "turn/start"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("protocol did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	router.CloseConversations()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("cancelled round reported success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("closing window did not cancel generation")
	}
	if len(router.ConversationStatus().Windows) != 0 {
		t.Fatal("closed window retained")
	}
}

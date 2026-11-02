package businesshost_test

import (
	"context"
	"encoding/json"
	"gpt-robot/businesshost"
	"gpt-robot/model"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeepSeekHarnessProbeAndIsolatedSingleGeneration(t *testing.T) {
	root := t.TempDir()
	capture := filepath.Join(root, "calls.jsonl")
	fake := filepath.Join(root, "dsh")
	script := `#!/usr/bin/python3
import sys,json,os
ids=['timer','llm','session','session-log-deepseek','agent','agent-loop','session-projection','authorization','deepseek-account','credentials','tools','system-prompt','sdk-app-startup','sdk-jsonrpc-server','deepseek-llm-api-extensions','llm-deepseek-account','tool-bash','session-persistence-jsonl','llm-retry','session-title-llm','unknown-future-tool']
if '--dump-default-config' in sys.argv:
 print('\n'.join('- id: '+x for x in ids));sys.exit(0)
patch=json.load(open(sys.argv[sys.argv.index('--patch')+1]))
for row in patch:
 if row['id'] in ['tool-bash','session-persistence-jsonl','llm-retry','session-title-llm','unknown-future-tool']:assert row['disabled'] is True
 if row['id']=='llm-deepseek-account':assert row['config']['retryPolicy']['maxRetries']==0
for line in sys.stdin:
 m=json.loads(line)
 with open(os.environ['ROBOT_TEST_CAPTURE'],'a') as f:f.write(json.dumps({'method':m['method'],'home':os.environ['DSH_HOME'],'patch':patch})+'\n')
 def emit(v):print(json.dumps(v),flush=True)
 if m['method']=='initialize':emit({'jsonrpc':'2.0','id':m['id'],'result':{'serverInfo':{'name':'deepseek-harness-sdk-runtime'}}})
 elif m['method']=='session/prompt':
  sid=m['params']['sessionId']
  emit({'jsonrpc':'2.0','id':m['id'],'result':{'messageId':'user-one'}})
  for kind,data in [('turn/start',{}),('assistant/message',{'message':{'content':[{'type':'text','text':json.dumps({'reply':'hello'})}]},'usage':{'inputTokens':100,'outputTokens':10,'cacheReadTokens':40}}),('turn/end',{'reason':'completed'})]:emit({'method':'session.event','params':{'sessionId':sid,'event':{'type':kind,'data':data}}})
`
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GPT_ROBOT_DSH_PATH", fake)
	t.Setenv("ROBOT_TEST_CAPTURE", capture)
	t.Setenv("DSH_HOME", filepath.Join(root, "native-home"))
	if err := businesshost.ProbeLocalHarness(context.Background(), root, "deepseek"); err != nil {
		t.Fatal(err)
	}
	backend := &businesshost.DeepSeekHarnessBackend{}
	generator := businesshost.NewGenerator(root, backend)
	input := model.ReplyPrompt{Channel: "private-message", Persona: "test", Text: "hello"}
	result, err := generator.Generate(context.Background(), &input)
	if err != nil || result.Text != "hello" || result.Usage.Source != "deepseek_harness" || *result.Usage.CachedInput != 40 {
		t.Fatal("protocol result", err)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	prompts, handshakes := 0, 0
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var item struct {
			Method string
			Home   string
			Patch  []struct {
				ID       string         `json:"id"`
				Disabled bool           `json:"disabled"`
				Config   map[string]any `json:"config"`
			}
		}
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			t.Fatal(err)
		}
		if item.Method == "session/prompt" {
			prompts++
		}
		if item.Method == "initialize" {
			handshakes++
		}
		if item.Home == filepath.Join(root, "native-home") {
			t.Fatal("runtime reused user's profile home")
		}
		for _, p := range item.Patch {
			if p.ID == "credentials" && p.Config["path"] != filepath.Join(root, "native-home", ".credentials.yaml") {
				t.Fatal("native login not reused")
			}
		}
	}
	if prompts != 1 || handshakes != 2 {
		t.Fatal("probe called model or generation repeated", prompts, handshakes)
	}
	// 协议替身核验真实适配器复用sessionId，且每轮回执仍独立计量。
	session, err := backend.OpenConversation(context.Background(), "只生成JSON，不使用工具。")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	first, err := session.GenerateJSON(context.Background(), "只生成JSON，不使用工具。", []byte(`{"message":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := session.GenerateJSON(context.Background(), "只生成JSON，不使用工具。", []byte(`{"message":"again"}`))
	if err != nil || second.Usage.Input != 100 || first.Usage.RequestID == second.Usage.RequestID || strings.Split(first.Usage.RequestID, "/")[0] != strings.Split(second.Usage.RequestID, "/")[0] {
		t.Fatal("session reuse or turn usage", first, second, err)
	}
}
func TestHarnessMissingDoesNotChangeOrStartAnything(t *testing.T) {
	t.Setenv("GPT_ROBOT_DSH_PATH", filepath.Join(t.TempDir(), "missing"))
	if err := businesshost.ProbeLocalHarness(context.Background(), t.TempDir(), "deepseek"); err == nil {
		t.Fatal("missing executable accepted")
	}
}

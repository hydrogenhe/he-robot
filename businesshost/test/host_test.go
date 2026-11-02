package businesshost_test

import (
	"context"
	"encoding/json"
	"gpt-robot/businesshost"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBusinessProfileAndNoDiscoveryRound(t *testing.T) {
	root := t.TempDir()
	capture := filepath.Join(root, "capture.jsonl")
	fake := filepath.Join(root, "codex")
	// 测试替身实现app-server边界协议，不发起模型或平台请求。
	script := `#!/usr/bin/python3
import sys,json,os
capture=open(os.environ['ROBOT_TEST_CAPTURE'],'w')
def emit(v):print(json.dumps(v),flush=True)
for line in sys.stdin:
 m=json.loads(line);capture.write(json.dumps({'message':m,'env':{k:os.environ.get(k) for k in ['CODEX_APP_TOOLS_PIPE_PATH','CODEX_TEAM_ID','CODEX_SANDBOX_NETWORK_DISABLED']},'args':sys.argv[1:]})+'\n');capture.flush()
 method=m['method'];id=m.get('id')
 if method=='initialize':emit({'id':id,'result':{}})
 elif method=='config/read':print(json.dumps({'id':id,'result':{'config':{}}}),flush=True)
 elif method=='mcpServerStatus/list':print(json.dumps({'id':id,'result':{'data':[],'nextCursor':None}}),flush=True)
 elif method=='skills/list':emit({'id':id,'result':{'data':[{'skills':[{'path':'/example/skill/SKILL.md'}]}]}})
 elif method=='thread/start':emit({'id':id,'result':{'thread':{'id':'11111111-1111-4111-8111-111111111111','ephemeral':True},'sandbox':{'type':'readOnly'},'approvalPolicy':'never','reasoningEffort':'low','model':'same-model'}})
 elif method=='turn/start':
  emit({'id':id,'result':{'turn':{'id':'22222222-2222-4222-8222-222222222222'}}})
  for tool,op,field in [('bilibili_comments','33333333-3333-4333-8333-333333333333','comments'),('memory_search','44444444-4444-4444-8444-444444444444','notes')]:
   emit({'method':'item/completed','params':{'item':{'type':'mcpToolCall','server':'gpt_robot','tool':tool,'status':'completed','result':{'content':[{'type':'text','text':json.dumps({'operationId':op,field:[{}]})}]}}}})
  usage={'inputTokens':300,'outputTokens':20,'cachedInputTokens':0,'totalTokens':320}
  emit({'method':'thread/tokenUsage/updated','params':{'tokenUsage':{'last':usage,'total':usage}}})
  emit({'method':'thread/tokenUsage/updated','params':{'tokenUsage':{'last':usage,'total':usage}}})
  emit({'method':'turn/completed','params':{'turn':{'status':'completed'}}})
`
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_APP_RUNTIME_CLI_PATH", fake)
	t.Setenv("CODEX_APP_TOOLS_PIPE_PATH", "development-bridge")
	t.Setenv("CODEX_TEAM_ID", "development-team")
	t.Setenv("CODEX_SANDBOX_NETWORK_DISABLED", "1")
	t.Setenv("ROBOT_TEST_CAPTURE", capture)
	input := businesshost.ReadInput{Root: root, Video: "BV18VLu6JEit", MemoryQuery: "指定记忆"}
	result, err := businesshost.Read(context.Background(), &input)
	if err != nil || len(result.Tools) != 2 || result.Total.Total != 320 || len(result.Calls) != 1 {
		t.Fatal(result, err)
	}
	rows, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(rows)), "\n") {
		var value struct {
			Message struct {
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			} `json:"message"`
			Env  map[string]any `json:"env"`
			Args []string       `json:"args"`
		}
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			t.Fatal(err)
		}
		if value.Env["CODEX_APP_TOOLS_PIPE_PATH"] != nil || value.Env["CODEX_TEAM_ID"] != nil || value.Env["CODEX_SANDBOX_NETWORK_DISABLED"] != "1" {
			t.Fatal("development bridge leaked or security environment removed")
		}
		if value.Message.Method == "thread/start" {
			params := value.Message.Params
			if params["ephemeral"] != true || len(params["baseInstructions"].(string)) > 1500 || params["sandbox"] != "read-only" || params["approvalPolicy"] != "never" {
				t.Fatal(params)
			}
			config := params["config"].(map[string]any)
			disabled := config["skills.config"].([]any)
			if len(disabled) != 1 || disabled[0].(map[string]any)["enabled"] != false {
				t.Fatal(config)
			}
		}
	}
}
func TestInvalidInputDoesNotStartHost(t *testing.T) {
	if _, err := businesshost.Read(context.Background(), nil); err == nil {
		t.Fatal("missing input accepted")
	}
	input := businesshost.ReadInput{Root: t.TempDir(), Video: "wrong", MemoryQuery: "memory"}
	if _, err := businesshost.Read(context.Background(), &input); err == nil {
		t.Fatal("invalid video accepted")
	}
}

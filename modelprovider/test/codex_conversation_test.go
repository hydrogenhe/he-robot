package modelprovider_test

import (
	"context"
	"encoding/json"
	"gpt-robot/businesshost"
	"gpt-robot/model"
	"gpt-robot/modelprovider"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// conversationHarnessScript 仅模拟本机协议，不调用任何真实模型。
const conversationHarnessScript = `#!/usr/bin/python3
import sys,json,os,uuid
thread=str(uuid.uuid4());n=0
def emit(v):print(json.dumps(v),flush=True)
for line in sys.stdin:
 m=json.loads(line);method=m['method'];i=m.get('id')
 with open(os.environ['ROBOT_TEST_CAPTURE'],'a') as f:f.write(json.dumps({'method':method,'thread':thread,'params':m['params']})+'\n')
 if method=='initialize':emit({'id':i,'result':{}})
 elif method=='skills/list':emit({'id':i,'result':{'data':[]}})
 elif method=='config/read':emit({'id':i,'result':{'config':{}}})
 elif method=='mcpServerStatus/list':emit({'id':i,'result':{'data':[],'nextCursor':None}})
 elif method=='thread/start':emit({'id':i,'result':{'thread':{'id':thread,'ephemeral':True},'model':'test-model','reasoningEffort':'low','sandbox':{'type':'readOnly'},'approvalPolicy':'never'}})
 elif method=='turn/start':
  n+=1;emit({'id':i,'result':{'turn':{'id':str(uuid.uuid4())}}})
  if os.environ.get('ROBOT_TEST_RETURN_MODEL')=='block':continue
  last={'inputTokens':100,'outputTokens':10,'cachedInputTokens':20,'totalTokens':110}
  if os.environ.get('ROBOT_TEST_RETURN_MODEL')=='tokens':last['inputTokens']=12000
  total={k:v*n for k,v in last.items()}
  if os.environ.get('ROBOT_TEST_RETURN_MODEL')!='unknown':emit({'method':'thread/tokenUsage/updated','params':{'tokenUsage':{'last':last,'total':total}}})
  emit({'method':'item/completed','params':{'item':{'type':'agentMessage','text':'{"reply":"你好呀"}'}}})
  emit({'method':'turn/completed','params':{'turn':{'status':'completed'}}})
`

func TestCodexConversationReusesThreadAndReportsTurnDelta(t *testing.T) {
	root := t.TempDir()
	capture := filepath.Join(root, "calls.jsonl")
	fake := filepath.Join(root, "codex")
	script := conversationHarnessScript
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_APP_RUNTIME_CLI_PATH", fake)
	t.Setenv("ROBOT_TEST_CAPTURE", capture)
	router := modelprovider.New(root)
	defer router.CloseConversations()
	config := model.ModelProviderConfig{Mode: "local", Provider: "codex", CodexModel: "test-model", CodexEffort: "low", DeepSeekModel: "deepseek-flash"}
	if err := router.Configure(&config); err != nil {
		t.Fatal(err)
	}
	policy := model.DefaultConversationConfig()
	policy.MaxTurns = 2
	policy.ContextTokens = 64000
	if err := router.ConfigureConversations(&policy); err != nil {
		t.Fatal(err)
	}
	g := businesshost.NewGenerator(root, router)
	scope := model.ConversationScope{Platform: "another-platform", Account: "123", Peer: "456", Channel: "private-message", RequestID: model.ID()}
	prompt := model.ReplyPrompt{Channel: "private-message", Owner: true, Text: "你好", Conversation: &scope}
	threads := []string{}
	turns := []string{}
	for range 3 {
		scope.RequestID = model.ID()
		result, err := g.Generate(context.Background(), &prompt)
		if err != nil || result.Usage == nil || result.Usage.Input != 100 || result.Usage.Output != 10 || *result.Usage.CachedInput != 20 {
			t.Fatal("usage was cumulative or missing", result, err)
		}
		threads = append(threads, result.Usage.ThreadID)
		turns = append(turns, result.Usage.TurnID)
		router.FinishConversation(&scope, true)
	}
	if threads[0] != threads[1] || threads[1] == threads[2] || turns[0] == turns[1] {
		t.Fatal("thread rotation or turn identity", threads, turns)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	starts, prompts := 0, 0
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var row struct {
			Method string
			Params map[string]any
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if row.Method == "thread/start" {
			starts++
			if row.Params["ephemeral"] != true {
				t.Fatal("non-ephemeral")
			}
			config := row.Params["config"].(map[string]any)
			if config["model_auto_compact_token_limit"] != float64(1000000000) {
				t.Fatal("automatic compaction not suppressed")
			}
		}
		if row.Method == "turn/start" {
			prompts++
			if row.Params["effort"] != "low" {
				t.Fatal("effort changed")
			}
		}
	}
	if starts != 2 || prompts != 3 {
		t.Fatal("unexpected handshake or inference count", starts, prompts)
	}
}

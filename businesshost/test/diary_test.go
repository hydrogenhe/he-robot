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

func TestDiaryUsesBoundedEphemeralNoToolHostAndRealUsageProtocol(t *testing.T) {
	root := t.TempDir()
	fake := filepath.Join(root, "codex")
	capture := filepath.Join(root, "capture.json")
	script := `#!/usr/bin/python3
import sys,json,os
for line in sys.stdin:
 m=json.loads(line);method=m['method'];i=m.get('id')
 if method=='initialize':print(json.dumps({'id':i,'result':{}}),flush=True)
 elif method=='config/read':print(json.dumps({'id':i,'result':{'config':{}}}),flush=True)
 elif method=='mcpServerStatus/list':print(json.dumps({'id':i,'result':{'data':[],'nextCursor':None}}),flush=True)
 elif method=='skills/list':print(json.dumps({'id':i,'result':{'data':[]}}),flush=True)
 elif method=='thread/start':
  assert m['params']['ephemeral'] is True
  print(json.dumps({'id':i,'result':{'thread':{'id':'11111111-1111-4111-8111-111111111111','ephemeral':True},'sandbox':{'type':'readOnly'},'approvalPolicy':'never','reasoningEffort':'low','model':'test'}}),flush=True)
 elif method=='turn/start':
  open(os.environ['DIARY_TEST_CAPTURE'],'w').write(m['params']['input'][0]['text'])
  print(json.dumps({'id':i,'result':{'turn':{'id':'22222222-2222-4222-8222-222222222222'}}}),flush=True)
  usage={'inputTokens':80,'outputTokens':30,'cachedInputTokens':10,'totalTokens':110}
  print(json.dumps({'method':'thread/tokenUsage/updated','params':{'tokenUsage':{'last':usage,'total':usage}}}),flush=True)
  text=json.dumps({'title':'今天的好奇','body':'今天读到了几句讨论，我把一点好奇留给明天。'})
  print(json.dumps({'method':'item/completed','params':{'item':{'type':'agentMessage','text':text}}}),flush=True)
  print(json.dumps({'method':'turn/completed','params':{'turn':{'status':'completed'}}}),flush=True)
`
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_APP_RUNTIME_CLI_PATH", fake)
	t.Setenv("DIARY_TEST_CAPTURE", capture)
	input := model.DiaryPrompt{Date: "2026-10-31", Persona: strings.Repeat("龙", 8000), State: strings.Repeat("开心", 1000)}
	for i := 0; i < 1000; i++ {
		fact := model.DiaryFact{Source: "event-id", Kind: "comments", Summary: strings.Repeat("公开材料", 200)}
		input.Facts = append(input.Facts, fact)
	}
	generator := businesshost.NewGenerator(root)
	result, err := generator.GenerateDiary(context.Background(), &input)
	if err != nil || result.Title != "今天的好奇" || result.Usage.Input != 80 || result.Usage.Output != 30 {
		t.Fatal(result, err)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	var prompt model.DiaryPrompt
	if err := json.Unmarshal(raw, &prompt); err != nil {
		t.Fatal(err)
	}
	if len(raw) > 6000 || len(prompt.Facts) > 12 || len([]rune(prompt.Persona)) > 800 || len(input.Facts) != 1000 {
		t.Fatal("unbounded or mutated diary context")
	}
}

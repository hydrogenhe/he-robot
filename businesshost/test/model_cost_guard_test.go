package businesshost_test

import (
	"context"
	"gpt-robot/businesshost"
	"gpt-robot/model"
	"os"
	"path/filepath"
	"testing"
)

func TestCodexUsesExactModelAndLowEffortAndRejectsSilentUpgrade(t *testing.T) {
	for _, accepted := range []bool{true, false} {
		t.Run(map[bool]string{true: "exact", false: "refuse-upgrade"}[accepted], func(t *testing.T) {
			root := t.TempDir()
			fake := filepath.Join(root, "codex")
			marker := filepath.Join(root, "generated")
			script := `#!/usr/bin/python3
import json,sys,os
def emit(v):print(json.dumps(v),flush=True)
for line in sys.stdin:
 m=json.loads(line);method=m['method'];i=m.get('id')
 if method=='initialize':emit({'id':i,'result':{}})
 elif method=='config/read':print(json.dumps({'id':i,'result':{'config':{}}}),flush=True)
 elif method=='mcpServerStatus/list':print(json.dumps({'id':i,'result':{'data':[],'nextCursor':None}}),flush=True)
 elif method=='skills/list':emit({'id':i,'result':{'data':[]}})
 elif method=='model/list':emit({'id':i,'result':{'data':[{'id':'sol','model':'gpt-6.1-sol','displayName':'Sol'},{'id':'terra','model':'gpt-6.1-terra','displayName':'Terra'}]}})
 elif method=='thread/start':
  assert m['params']['model']=='gpt-6.1-sol'
  assert m['params']['config']['model_reasoning_effort']=='low'
  emit({'id':i,'result':{'thread':{'id':'thread','ephemeral':True},'sandbox':{'type':'readOnly'},'approvalPolicy':'never','reasoningEffort':'low','model':os.environ['ROBOT_TEST_RETURN_MODEL']}})
 elif method=='turn/start':
  assert m['params']['effort']=='low'
  open(os.environ['ROBOT_TEST_MARKER'],'w').write('generated')
  emit({'id':i,'result':{'turn':{'id':'turn'}}})
  emit({'method':'item/completed','params':{'item':{'type':'agentMessage','text':'{"reply":"hello"}'}}})
  emit({'method':'turn/completed','params':{'turn':{'status':'completed'}}})
`
			if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CODEX_APP_RUNTIME_CLI_PATH", fake)
			t.Setenv("ROBOT_TEST_MARKER", marker)
			returned := "gpt-6.1-sol"
			if !accepted {
				returned = "gpt-6.1-terra"
			}
			t.Setenv("ROBOT_TEST_RETURN_MODEL", returned)
			choices, err := businesshost.CodexModels(context.Background(), root)
			if err != nil || len(choices) != 2 || choices[1].ID != "gpt-6.1-terra" {
				t.Fatal("model catalog", err)
			}
			backend := businesshost.NewCodexBackend(root, "gpt-6.1-sol", "low")
			generator := businesshost.NewGenerator(root, backend)
			input := model.ReplyPrompt{Channel: "private-message", Text: "hello"}
			_, err = generator.Generate(context.Background(), &input)
			if accepted && err != nil {
				t.Fatal(err)
			}
			if !accepted && err == nil {
				t.Fatal("silent model upgrade accepted")
			}
			_, statErr := os.Stat(marker)
			if accepted && statErr != nil {
				t.Fatal("selected model did not generate")
			}
			if !accepted && !os.IsNotExist(statErr) {
				t.Fatal("upgraded model was called")
			}
		})
	}
}

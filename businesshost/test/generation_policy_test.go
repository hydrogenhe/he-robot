package businesshost_test

import (
	"context"
	"gpt-robot/businesshost"
	"gpt-robot/model"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInheritedMCPDisabledAndRemainingToolsRejectBeforeInference(t *testing.T) {
	for _, remaining := range []string{"{}", "{\"read_secret\":{}}"} {
		t.Run(remaining, func(t *testing.T) {
			root := t.TempDir()
			fake := filepath.Join(root, "codex")
			marker := filepath.Join(root, "generated")
			script := `#!/usr/bin/python3
import sys,json,os
def emit(v):print(json.dumps(v),flush=True)
for line in sys.stdin:
 m=json.loads(line);i=m.get('id');method=m['method']
 if method=='initialize':emit({'id':i,'result':{}})
 elif method=='skills/list':emit({'id':i,'result':{'data':[]}})
 elif method=='config/read':emit({'id':i,'result':{'config':{'mcp_servers':{'foreign.server':{'command':'unused'}},'plugins':{'foreign@plugin':{'enabled':True}}}}})
 elif method=='thread/start':
  c=m['params']['config'];assert c['mcp_servers']['foreign.server']['enabled'] is False;assert c['plugins']['foreign@plugin']['enabled'] is False
  assert 'UNRELATED_SECRET' not in os.environ
  assert m['params']['dynamicTools']==[] and m['params']['environments']==[]
  emit({'id':i,'result':{'thread':{'id':'thread','ephemeral':True},'sandbox':{'type':'readOnly'},'approvalPolicy':'never','reasoningEffort':'low','model':'same-model'}})
 elif method=='mcpServerStatus/list':emit({'id':i,'result':{'data':[{'tools':json.loads(os.environ['ROBOT_TEST_RETURN_MODEL'])}],'nextCursor':None}})
 elif method=='turn/start':
  open(os.environ['ROBOT_TEST_MARKER'],'w').write('called')
  emit({'id':i,'result':{'turn':{'id':'turn'}}})
  emit({'method':'item/completed','params':{'item':{'type':'agentMessage','text':'{"reply":"你好"}'}}})
  emit({'method':'turn/completed','params':{'turn':{'status':'completed'}}})
`
			if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CODEX_APP_RUNTIME_CLI_PATH", fake)
			t.Setenv("ROBOT_TEST_MARKER", marker)
			t.Setenv("ROBOT_TEST_RETURN_MODEL", remaining)
			t.Setenv("UNRELATED_SECRET", "never-forward-me")
			generator := businesshost.NewGenerator(root)
			input := model.ReplyPrompt{Channel: "private-message", Text: "你好"}
			_, err := generator.Generate(context.Background(), &input)
			_, stat := os.Stat(marker)
			if remaining == "{}" {
				if err != nil || stat != nil {
					t.Fatal(err, stat)
				}
			} else {
				if err == nil || !os.IsNotExist(stat) {
					t.Fatal("tool-enabled generation reached model", err, stat)
				}
			}
		})
	}
}

func TestDeepSeekOSIsolationDeniesFilesOutsideRuntime(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS Seatbelt boundary")
	}
	binaryDir, secretDir, scratch := t.TempDir(), t.TempDir(), t.TempDir()
	secret := filepath.Join(secretDir, "private-data")
	if err := os.WriteFile(secret, []byte("fake-secret-not-for-runtime"), 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(scratch, "denied")
	fake := filepath.Join(binaryDir, "dsh")
	script := `#!/usr/bin/python3
import sys,json,os
if '--dump-default-config' in sys.argv:
 try:
  open(os.environ['ROBOT_TEST_RETURN_MODEL']).read()
  sys.exit(9)
 except PermissionError:open(os.environ['ROBOT_TEST_MARKER'],'w').write('denied')
 ids=['timer','llm','session','session-log-deepseek','agent','agent-loop','session-projection','authorization','deepseek-account','credentials','tools','system-prompt','sdk-app-startup','sdk-jsonrpc-server','deepseek-llm-api-extensions','llm-deepseek-account']
 print('\n'.join('- id: '+i for i in ids));sys.exit(0)
for line in sys.stdin:
 m=json.loads(line)
 if m['method']=='initialize':print(json.dumps({'id':m['id'],'result':{'serverInfo':{'name':'deepseek-harness-sdk-runtime'}}}),flush=True)
`
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GPT_ROBOT_DSH_PATH", fake)
	t.Setenv("TMPDIR", scratch)
	t.Setenv("ROBOT_TEST_RETURN_MODEL", secret)
	t.Setenv("ROBOT_TEST_MARKER", marker)
	if err := businesshost.ProbeLocalHarness(context.Background(), binaryDir, "deepseek"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(marker); err != nil || string(b) != "denied" {
		t.Fatal("OS file boundary not enforced", err)
	}
}

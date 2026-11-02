package businesshost_test

import (
	"context"
	"encoding/json"
	"gpt-robot/businesshost"
	"gpt-robot/model"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratorNoToolsBoundedContextAndActualUsage(t *testing.T) {
	root := t.TempDir()
	capture := filepath.Join(root, "prompt.json")
	fake := filepath.Join(root, "codex")
	// 协议替身返回宿主事件，不调用真实模型或平台。
	script := `#!/usr/bin/python3
import sys,json,os
def emit(v): print(json.dumps(v),flush=True)
for line in sys.stdin:
 m=json.loads(line);method=m['method'];id=m.get('id')
 if method=='initialize':emit({'id':id,'result':{}})
 elif method=='config/read':print(json.dumps({'id':id,'result':{'config':{}}}),flush=True)
 elif method=='mcpServerStatus/list':print(json.dumps({'id':id,'result':{'data':[],'nextCursor':None}}),flush=True)
 elif method=='skills/list':emit({'id':id,'result':{'data':[]}})
 elif method=='thread/start':
  open(os.environ['ROBOT_TEST_CAPTURE'],'w').write(json.dumps({'thread':m['params'],'args':sys.argv[1:]}))
  emit({'id':id,'result':{'thread':{'id':'11111111-1111-4111-8111-111111111111','ephemeral':True},'sandbox':{'type':'readOnly'},'approvalPolicy':'never','reasoningEffort':'low','model':'same-model'}})
 elif method=='turn/start':
  p=os.environ['ROBOT_TEST_CAPTURE'];old=json.load(open(p));old['turn']=m['params'];old['turnCount']=old.get('turnCount',0)+1;open(p,'w').write(json.dumps(old))
  emit({'id':id,'result':{'turn':{'id':'22222222-2222-4222-8222-222222222222'}}})
  emit({'method':'item/completed','params':{'item':{'type':'userMessage'}}})
  usage={'inputTokens':120,'outputTokens':8,'cachedInputTokens':40,'totalTokens':128}
  emit({'method':'thread/tokenUsage/updated','params':{'tokenUsage':{'last':usage,'total':usage}}})
  emit({'method':'item/completed','params':{'item':{'type':'agentMessage','text':json.dumps({'reply':'你好呀，今天开心吗？','signal':{'mood':'happy','intensity':40,'warmth':1,'curiosity':0,'playfulness':0}})}}})
  emit({'method':'turn/completed','params':{'turn':{'status':'completed'}}})
`
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_APP_RUNTIME_CLI_PATH", fake)
	t.Setenv("ROBOT_TEST_CAPTURE", capture)
	input := model.ReplyPrompt{Persona: strings.Repeat("人", 2000), Channel: "private-message", Text: "你好", Recent: []string{"1", "2", "3", "4", "5", "6", "7"}}
	for range 4 {
		memory := model.RecalledMemory{Quote: strings.Repeat("私", 500), OccurredAt: "2026-10-31T00:00:00Z", MessageKey: "1001"}
		input.PrivateMemories = append(input.PrivateMemories, memory)
	}
	imagePath := filepath.Join(root, "frame.png")
	imageFile, err := os.Create(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(imageFile, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	imageFile.Close()
	backend := businesshost.NewCodexBackend(root, "same-model", "low")
	imageResult, err := backend.GenerateImages(context.Background(), "只分析附图", []byte(`{"frameSeconds":[1]}`), []string{imagePath})
	if err != nil || imageResult.Reply.Usage.Input != 120 {
		t.Fatal(imageResult, err)
	}
	imageRaw, err := os.ReadFile(capture)
	if err != nil || !strings.Contains(string(imageRaw), "data:image/png;base64,") || strings.Contains(string(imageRaw), imagePath) {
		t.Fatal("image attachment missing or path leaked", err)
	}
	if _, err := backend.GenerateImages(context.Background(), "", nil, []string{imagePath, imagePath, imagePath, imagePath, imagePath, imagePath, imagePath}); err == nil {
		t.Fatal("unbounded image input")
	}
	generator := businesshost.NewGenerator(root)
	reply, err := generator.Generate(context.Background(), &input)
	if err != nil || reply.Text != "你好呀，今天开心吗？" || reply.Signal == nil || reply.Signal.Mood != "happy" || reply.Usage.Input != 120 || reply.Usage.Output != 8 || *reply.Usage.CachedInput != 40 {
		t.Fatal(reply, err)
	}
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Thread    map[string]any `json:"thread"`
		Args      []string       `json:"args"`
		TurnCount int            `json:"turnCount"`
		Turn      struct {
			Input []struct {
				Text string `json:"text"`
			} `json:"input"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(value.Args, " "), "mcp_servers.gpt_robot.enabled=false") || !strings.Contains(strings.Join(value.Args, " "), "features.code_mode_host=false") {
		t.Fatal("tools not disabled")
	}
	if value.Thread["ephemeral"] != true {
		t.Fatal("生成没有使用临时会话")
	}
	instructions := value.Thread["baseInstructions"].(string)
	if len(instructions) > 1200 || value.TurnCount != 1 || !strings.Contains(instructions, "privateMemories") || strings.Contains(instructions, "publicMemories") || strings.Contains(instructions, "interestCandidates") {
		t.Fatal("irrelevant instructions retained or generation repeated", len(instructions), value.TurnCount)
	}
	for _, rule := range []string{"不可信", "禁工具", "正式名字", "1–60", "-1/0/1", "较新明确更正优先", "永久学会"} {
		if !strings.Contains(instructions, rule) {
			t.Fatal("required safety or recall rule missing", rule)
		}
	}
	var prompt model.ReplyPrompt
	if err := json.Unmarshal([]byte(value.Turn.Input[0].Text), &prompt); err != nil {
		t.Fatal(err)
	}
	if len([]rune(prompt.Persona)) > 801 || len(prompt.Recent) != 6 || len(prompt.PrivateMemories) != 3 || len([]rune(prompt.PrivateMemories[0].Quote)) > 161 {
		t.Fatal("unbounded prompt")
	}
	if len(input.Recent) != 7 || len([]rune(input.Persona)) != 2000 || len(input.PrivateMemories) != 4 || len([]rune(input.PrivateMemories[0].Quote)) != 500 {
		t.Fatal("mutated caller")
	}
	// 最坏字符和 JSON 转义也在实际宿主传输边界检查，不只测试裁剪辅助函数。
	input.Text = strings.Repeat("\x00", 500)
	input.State = strings.Repeat("情绪", 2000)
	input.Persona = strings.Repeat("🐉", 2000)
	if _, err := generator.Generate(context.Background(), &input); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	body := value.Turn.Input[0].Text
	total := len(body) + len(value.Thread["baseInstructions"].(string)) + len(value.Thread["developerInstructions"].(string))
	if len(body) > model.ReplyContextMaxBytes || total > 10000 {
		t.Fatal("unbounded serialized model input", len(body), total)
	}
	if err := json.Unmarshal([]byte(body), &prompt); err != nil {
		t.Fatal(err)
	}
	if prompt.Text != input.Text {
		t.Fatal("current message silently truncated")
	}
	// 有成长与候选主题时才附规则，不删除实际的人设/记忆或增加生成轮次。
	input = model.ReplyPrompt{
		Channel:            "private-message",
		Persona:            "测试角色",
		Text:               "今天聊聊植物",
		Development:        "当前私聊范围的成长摘要",
		InterestCandidates: []string{"植物"},
	}
	if _, err := generator.Generate(context.Background(), &input); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	instructions = value.Thread["baseInstructions"].(string)
	if len(instructions) > 1400 || value.TurnCount != 1 || !strings.Contains(instructions, "development") || !strings.Contains(instructions, "interestCandidates") || !strings.Contains(instructions, "不代表信任或发送权限") {
		t.Fatal("growth rules missing or generation repeated")
	}

}

func TestPublicGeneratorRejectsPrivateMemoryBeforeStartingModel(t *testing.T) {
	t.Setenv("CODEX_APP_RUNTIME_CLI_PATH", "/nonexistent/never-start")
	generator := businesshost.NewGenerator(t.TempDir())
	memory := model.RecalledMemory{Quote: "私密事实", OccurredAt: "2026-10-31T00:00:00Z", MessageKey: "1001"}
	input := model.ReplyPrompt{Channel: "video-comment", Text: "你好", PrivateMemories: []model.RecalledMemory{memory}}
	_, err := generator.Generate(context.Background(), &input)
	business, ok := err.(*model.Error)
	if !ok || business.Code != "PRIVATE_SCOPE" {
		t.Fatal("public memory not rejected before model", err)
	}
}

func TestGeneratorRejectsPersistentFallbackBeforeSendingPrompt(t *testing.T) {
	root := t.TempDir()
	fake := filepath.Join(root, "codex")
	marker := filepath.Join(root, "turn-started")
	script := `#!/usr/bin/python3
import sys,json,os
for line in sys.stdin:
 m=json.loads(line);method=m['method'];id=m.get('id')
 if method=='initialize': print(json.dumps({'id':id,'result':{}}),flush=True)
 elif method=='config/read':print(json.dumps({'id':id,'result':{'config':{}}}),flush=True)
 elif method=='mcpServerStatus/list':print(json.dumps({'id':id,'result':{'data':[],'nextCursor':None}}),flush=True)
 elif method=='skills/list': print(json.dumps({'id':id,'result':{'data':[]}}),flush=True)
 elif method=='thread/start': print(json.dumps({'id':id,'result':{'thread':{'id':'11111111-1111-4111-8111-111111111111','ephemeral':False},'model':'test'}}),flush=True)
 elif method=='turn/start': open(os.environ['ROBOT_TEST_MARKER'],'w').write('bad')
`
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_APP_RUNTIME_CLI_PATH", fake)
	t.Setenv("ROBOT_TEST_MARKER", marker)
	generator := businesshost.NewGenerator(root)
	prompt := model.ReplyPrompt{Channel: "private-message", Text: "这条消息不得进入持久聊天"}
	if _, err := generator.Generate(context.Background(), &prompt); err == nil {
		t.Fatal("persistent fallback accepted")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("prompt sent to persistent thread")
	}
}

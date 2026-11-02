package mcpbridge_test

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	httpinit "gpt-robot/http/init"
	"gpt-robot/mcpbridge"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestMCPHelper(t *testing.T) {
	if os.Getenv("GPT_ROBOT_MCP_TEST_HELPER") != "1" {
		return
	}
	if err := mcpbridge.Run(context.Background(), os.Getenv("GPT_ROBOT_TEST_ROOT")); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
func TestRealStdioHandshakeToolsAndRestrictedCall(t *testing.T) {
	root := t.TempDir()
	cfg := httpinit.DefaultConfig(root)
	cfg.Address = "127.0.0.1:0"
	app, err := httpinit.Init(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	go app.HTTP.Serve(app.Listener)
	defer app.HTTP.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.Command(os.Args[0], "-test.run=TestMCPHelper")
	cmd.Env = append(os.Environ(), "GPT_ROBOT_MCP_TEST_HELPER=1", "GPT_ROBOT_TEST_ROOT="+root)
	transport := &mcp.CommandTransport{Command: cmd}
	client := mcp.NewClient(&mcp.Implementation{Name: "acceptance", Version: "1"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil || len(tools.Tools) != 7 {
		t.Fatal(tools, err)
	}
	for _, tool := range tools.Tools {
		if (tool.Name == "bilibili_comments" || tool.Name == "memory_search") && (tool.Annotations == nil || !tool.Annotations.ReadOnlyHint) {
			t.Fatal("read-only tool missing hint", tool.Name)
		}
		if strings.Contains(tool.Name, "send") {
			t.Fatal("send exposed")
		}
	}
	p := &mcp.CallToolParams{Name: "character_context", Arguments: map[string]any{}}
	result, err := session.CallTool(ctx, p)
	if err != nil || result.IsError || len(result.Content) == 0 {
		t.Fatal(result, err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "persona") || strings.Contains(text, "token") {
		t.Fatal(text)
	}
	denied := &mcp.CallToolParams{Name: "bilibili_prepare_reply", Arguments: map[string]any{"bvid": "BV161nZ6JE6b", "text": "不应发送"}}
	result, err = session.CallTool(ctx, denied)
	if err != nil || !result.IsError {
		t.Fatal(result, err)
	}
}

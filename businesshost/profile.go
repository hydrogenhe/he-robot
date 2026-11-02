package businesshost

import (
	_ "embed"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed prompt.md
var businessInstructions string

// 仅隔离开发会话的工具桥接及关联身份；保留凭证环境、托管策略、沙箱与网络约束。
func businessEnvironment() []string {
	allowed := map[string]bool{"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "TMPDIR": true, "TEMP": true, "TMP": true, "LANG": true, "LC_ALL": true, "SHELL": true, "SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true, "PATHEXT": true, "LOCALAPPDATA": true, "APPDATA": true, "USERPROFILE": true, "HTTP_PROXY": true, "HTTPS_PROXY": true, "ALL_PROXY": true, "NO_PROXY": true, "http_proxy": true, "https_proxy": true, "all_proxy": true, "no_proxy": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "NODE_EXTRA_CA_CERTS": true, "CODEX_HOME": true, "CODEX_APP_RUNTIME_CLI_PATH": true, "OPENAI_API_KEY": true, "CODEX_API_KEY": true, "DSH_HOME": true, "CODEX_SANDBOX_NETWORK_DISABLED": true, "CODEX_SANDBOX": true, "ROBOT_TEST_CAPTURE": true, "ROBOT_TEST_MARKER": true, "ROBOT_TEST_RETURN_MODEL": true, "DIARY_TEST_CAPTURE": true}
	result := []string{}
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if allowed[name] || strings.HasPrefix(name, "LC_") {
			result = append(result, value)
		}
	}
	return result
}
func codexExecutable() (string, error) {
	if path := os.Getenv("CODEX_APP_RUNTIME_CLI_PATH"); path != "" {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	if path, err := exec.LookPath("codex"); err == nil {
		return path, nil
	}
	for _, path := range []string{"/Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex", "/Applications/Codex.app/Contents/Resources/codex"} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return exec.LookPath("codex")
}
func profileArgs(root string) []string {
	settings := map[string]any{
		"developer_instructions":              "",
		"project_doc_max_bytes":               0,
		"features.apps":                       false,
		"features.multi_agent":                false,
		"features.goals":                      false,
		"features.memories":                   false,
		"features.hooks":                      false,
		"features.shell_tool":                 false,
		"features.view_image":                 false,
		"features.image_generation":           false,
		"features.code_mode.enabled":          false,
		"features.code_mode_host":             true,
		"web_search":                          "disabled",
		"mcp_servers.gpt_robot.command":       filepath.Join(root, "bin", "gpt-robot"),
		"mcp_servers.gpt_robot.args":          []string{"mcp", "--root", root},
		"mcp_servers.gpt_robot.enabled_tools": []string{"bilibili_comments", "memory_search"},
	}
	args := []string{"app-server", "--stdio"}
	for name, value := range settings {
		raw, _ := json.Marshal(value)
		args = append(args, "-c", name+"="+string(raw))
	}
	return args
}

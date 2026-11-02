package app

import (
	"context"
	"encoding/json"
	"fmt"
	"gpt-robot/model"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Root默认使用可执行文件旁的数据；开发构建位于项目bin目录时使用其父目录。
func Root(override string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	root := filepath.Dir(exe)
	if filepath.Base(root) == "bin" {
		root = filepath.Dir(root)
	}
	return filepath.Abs(root)
}
func Open(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Run()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Run()
	default:
		return exec.Command("xdg-open", url).Run()
	}
}
func OpenExisting(ctx context.Context, root string) error {
	b, err := os.ReadFile(filepath.Join(root, ".private/ui-launch.json"))
	if err != nil {
		return err
	}
	var launch model.Launch
	if err = json.Unmarshal(b, &launch); err != nil {
		return err
	}
	u, err := url.Parse(launch.URL)
	if err != nil {
		return err
	}
	if u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil {
		return model.Fail("AUTH", "无效本地实例")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.Scheme+"://"+u.Host+"/", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 65536))
	if err != nil {
		return err
	}
	if res.StatusCode != 200 || !strings.Contains(string(body), "<title>栖 · GPT Robot</title>") {
		return model.Fail("HTTP", "工作台未运行")
	}
	return Open(launch.URL)
}
func MCPConfig(root string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("[mcp_servers.gpt_robot]\ncommand = %q\nargs = [\"mcp\", \"--root\", %q]\n", exe, root), nil
}

package businesshost

import (
	"gpt-robot/model"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// sandboxHarness 对DSH SDK进程施加主机文件边界；不支持的系统拒绝而非降级为无隔离。
func sandboxHarness(path, work string, args []string) (string, []string, error) {
	if runtime.GOOS != "darwin" {
		return "", nil, model.Fail("HARNESS_ISOLATION", "此系统尚未提供Harness隔离适配，请使用API接入")
	}
	if _, err := os.Stat("/usr/bin/sandbox-exec"); err != nil {
		return "", nil, model.Fail("HARNESS_ISOLATION", "本机缺少Harness隔离能力，请使用API接入")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", nil, err
	}
	native := os.Getenv("DSH_HOME")
	if native == "" {
		native = filepath.Join(home, ".dsh")
	}
	// 仅系统运行库、程序、临时运行目录和账户凭证可读；项目、文档、SSH等不在开放范围。
	reads := []string{"/System", "/usr", "/bin", "/sbin", "/Library", "/Applications", "/dev", "/private/etc", "/private/var/db", work, filepath.Join(home, "Library/Keychains"), filepath.Join(home, "Library/Application Support/DeepSeek Harness"), filepath.Join(home, "Library/Caches"), filepath.Dir(path), filepath.Join(native, ".credentials.yaml")}
	policy := `(version 1) (deny default) (allow process*) (allow sysctl-read) (allow mach-lookup) (allow ipc-posix*) (allow network*) (allow file-read-metadata) (allow file-read* (literal "/") (literal "/private"))`
	for _, p := range reads {
		resolved, e := filepath.EvalSymlinks(p)
		if e == nil {
			p = resolved
		}
		policy += " (allow file-read* (subpath " + strconv.Quote(p) + "))"
	}
	for _, p := range []string{work, os.TempDir(), filepath.Join(home, "Library/Caches"), filepath.Join(native, ".credentials.yaml")} {
		resolved, e := filepath.EvalSymlinks(p)
		if e == nil {
			p = resolved
		}
		policy += " (allow file-read* file-write* (subpath " + strconv.Quote(p) + "))"
	}
	if strings.ContainsAny(policy, "\r\n") {
		return "", nil, model.Fail("HARNESS_ISOLATION", "隔离路径无效")
	}
	profile := filepath.Join(work, "runtime.sb")
	if err := os.WriteFile(profile, []byte(policy), 0600); err != nil {
		return "", nil, err
	}
	return "/usr/bin/sandbox-exec", append([]string{"-f", profile, path}, args...), nil
}

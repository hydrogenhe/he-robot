package codex

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
)

// 只允许纯元数据筛选，不执行工具；变量名必须一致，不存储原始脚本。
var discoveryPattern = regexp.MustCompile(`(?s)^\s*const ([A-Za-z_][A-Za-z0-9_]*)\s*=\s*ALL_TOOLS\.filter\(([A-Za-z_][A-Za-z0-9_]*)\s*=>\s*/[^/\n]+/\.test\(([A-Za-z_][A-Za-z0-9_]*)\.name\)\);\s*text\(([A-Za-z_][A-Za-z0-9_]*)\);?\s*$`)

func codeModeActions(input string) ([]string, bool) {
	if p := discoveryPattern.FindStringSubmatch(input); p != nil && p[1] == p[4] && p[2] == p[3] {
		return nil, true
	}
	actions := []string{}
	rest := strings.TrimSpace(input)
	for rest != "" {
		prefix := "text(await tools."
		if !strings.HasPrefix(rest, prefix) {
			return nil, false
		}
		rest = rest[len(prefix):]
		open := strings.IndexByte(rest, '(')
		if open < 1 {
			return nil, false
		}
		action := robotAction(rest[:open], "")
		if action == "" {
			return nil, false
		}
		rest = strings.TrimSpace(rest[open+1:])
		end := objectEnd(rest)
		if end == 0 {
			return nil, false
		}
		if !json.Valid([]byte(quoteKeys(rest[:end]))) {
			return nil, false
		}
		rest = strings.TrimSpace(rest[end:])
		if !strings.HasPrefix(rest, "))") {
			return nil, false
		}
		rest = strings.TrimSpace(rest[2:])
		if strings.HasPrefix(rest, ";") {
			rest = strings.TrimSpace(rest[1:])
		} else if rest != "" {
			return nil, false
		}
		actions = append(actions, action)
		if len(actions) > 512 {
			return nil, false
		}
	}
	return actions, len(actions) > 0
}
func objectEnd(input string) int {
	if len(input) == 0 || input[0] != '{' {
		return 0
	}
	depth := 0
	quoted, escaped := false, false
	for i, ch := range input {
		if quoted {
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				quoted = false
			}
			continue
		}
		if ch == '"' {
			quoted = true
			continue
		}
		if ch == '{' || ch == '[' {
			depth++
		}
		if ch == '}' || ch == ']' {
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return 0
}
func quoteKeys(input string) string {
	var out strings.Builder
	quoted, escaped := false, false
	for i := 0; i < len(input); i++ {
		ch := input[i]
		out.WriteByte(ch)
		if quoted {
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				quoted = false
			}
			continue
		}
		if ch == '"' {
			quoted = true
			continue
		}
		if ch != '{' && ch != ',' {
			continue
		}
		j := i + 1
		for j < len(input) && unicode.IsSpace(rune(input[j])) {
			out.WriteByte(input[j])
			j++
		}
		start := j
		for j < len(input) && (input[j] >= 'a' && input[j] <= 'z' || input[j] >= 'A' && input[j] <= 'Z' || input[j] == '_' || j > start && input[j] >= '0' && input[j] <= '9') {
			j++
		}
		end := j
		for j < len(input) && unicode.IsSpace(rune(input[j])) {
			j++
		}
		if end > start && j < len(input) && input[j] == ':' {
			out.WriteByte('"')
			out.WriteString(input[start:end])
			out.WriteByte('"')
			out.WriteString(input[end:j])
			i = j - 1
		} else {
			i = start - 1
		}
	}
	return out.String()
}

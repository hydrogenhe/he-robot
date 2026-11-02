package model

import (
	"regexp"
	"strings"
)

// GenerationSafetyConfig 主人与其他用户分别限制生成调用，0仅允许主人关闭该项限制。
type GenerationSafetyConfig struct {
	OwnerDailyCalls  int   `json:"ownerDailyCalls"`
	OtherDailyCalls  int   `json:"otherDailyCalls"`
	OwnerDailyTokens int64 `json:"ownerDailyTokens"`
	OtherDailyTokens int64 `json:"otherDailyTokens"`
}

// GenerationUsage 在调用前持久记账，失败、空回复和未知用量也不返还调用次数。
type GenerationUsage struct {
	Calls   int   `json:"calls"`
	Tokens  int64 `json:"tokens"`
	Unknown int   `json:"unknown"`
	Pending int   `json:"pending"`
}
type GenerationSafetyState struct {
	Day   string          `json:"day"`
	Owner GenerationUsage `json:"owner"`
	Other GenerationUsage `json:"other"`
}
type GenerationSafetyStatus struct {
	Config GenerationSafetyConfig
	State  GenerationSafetyState
}

var credentialPattern = regexp.MustCompile(`(?i)(\b(?:sk|sess|ghp|github_pat|xox[baprs])[-_][a-z0-9_-]{12,}|\beyJ[a-z0-9_-]{8,}\.[a-z0-9_-]{8,}\.[a-z0-9_-]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z ]*PRIVATE KEY-----|$))`)
var assignedSecret = regexp.MustCompile(`(?i)((?:api[_ -]?key|openai[_-]?key|access[_-]?token|refresh[_-]?token|authorization|password|secret|SESSDATA|bili_jct|cookie|密码|密钥|口令|验证码|私钥)["']?\s*(?:[:：=]|是|为)\s*["']?)(?:Bearer\s+)?[a-z0-9_+/%=.-]{4,}`)

// RedactSensitiveText 仅处理凭证值，不因提到API/Token等词拒绝正常对话。
func RedactSensitiveText(text string, secrets ...string) string {
	for _, secret := range secrets {
		if len(secret) >= 4 {
			text = strings.ReplaceAll(text, secret, "[已隐藏凭证]")
		}
	}
	text = credentialPattern.ReplaceAllString(text, "[已隐藏凭证]")
	return assignedSecret.ReplaceAllString(text, "${1}[已隐藏凭证]")
}
func ContainsSensitiveText(text string, secrets ...string) bool {
	return RedactSensitiveText(text, secrets...) != text
}

package model

// ModelProviderConfig 仅保存在本机私有文件，不作为HTTP响应或模型上下文。
type ModelProviderConfig struct {
	CodexEffort   string `json:"codexEffort"`
	HarnessEffort string `json:"harnessEffort"`
	Mode          string `json:"mode"`
	CodexModel    string `json:"codexModel"`
	Provider      string `json:"provider"`
	DeepSeekModel string `json:"deepseekModel"`
}

type ModelProviderInput struct {
	CodexEffort   string
	HarnessEffort string
	CodexModel    string
	Mode          string
	Provider      string
	DeepSeekModel string
}

// ModelProviderStatus 只公开本机Harness选择与可用性。
type ModelProviderStatus struct {
	CodexEffort   string
	HarnessEffort string
	CodexModel    string
	Mode          string
	Provider      string
	DeepSeekModel string
	CodexFound    bool
	DeepSeekFound bool
}

type ModelChoice struct {
	ID   string
	Name string
}

type HarnessConnectInput struct {
	Provider      string
	CodexModel    string
	DeepSeekModel string
	CodexEffort   string
	HarnessEffort string
}

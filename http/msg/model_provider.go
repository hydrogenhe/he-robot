package msg

import "gpt-robot/model"

type HarnessConnectRequest struct {
	CodexEffort   string `json:"codexEffort"`
	HarnessEffort string `json:"harnessEffort"`
	Provider      string `json:"provider"`
	CodexModel    string `json:"codexModel"`
	DeepSeekModel string `json:"deepseekModel"`
}

func ModelProvider(status *model.ModelProviderStatus) any {
	result := map[string]any{"mode": status.Mode, "provider": status.Provider, "deepseekModel": status.DeepSeekModel, "codexFound": status.CodexFound, "deepseekFound": status.DeepSeekFound, "codexModel": status.CodexModel, "codexEffort": status.CodexEffort, "harnessEffort": status.HarnessEffort}
	return result
}

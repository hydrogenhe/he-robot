package core

import (
	"context"
	"gpt-robot/businesshost"
	"gpt-robot/model"
	"gpt-robot/modelprovider"
)

func (s *Service) initializeModelProvider(ctx context.Context) error {
	config := s.modelRouter.Config()
	if _, err := s.Store.Read(ctx, ".private/model-provider.json", &config); err != nil {
		return err
	}
	original := map[string]any{}
	found, err := s.Store.Read(ctx, ".private/model-provider.json", &original)
	if err != nil {
		return err
	}
	known := map[string]bool{"mode": true, "provider": true, "codexModel": true, "deepSeekModel": true, "deepseekModel": true, "codexEffort": true, "harnessEffort": true}
	cleanup := config.Mode != "local"
	for key := range original {
		if !known[key] {
			cleanup = true
		}
	}
	if config.Mode != "local" {
		config.Mode = "local"
		config.Provider = "codex"
		config.CodexModel = ""
	}
	if found && cleanup {
		var archived map[string]any
		backup, err := s.Store.Read(ctx, ".private/archive/model-provider-before-harness-only.json", &archived)
		if err != nil {
			return err
		}
		if !backup {
			if err := s.Store.Write(ctx, ".private/archive/model-provider-before-harness-only.json", original); err != nil {
				return err
			}
		}
		if err := s.Store.Write(ctx, ".private/model-provider.json", &config); err != nil {
			return err
		}
	}
	return s.modelRouter.Configure(&config)
}

func (s *Service) ModelProviderStatus(ctx context.Context) (*model.ModelProviderStatus, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	config := s.modelRouter.Config()
	_, codexErr := businesshost.LocalHarnessPath("codex")
	_, deepseekErr := businesshost.LocalHarnessPath("deepseek")
	status := &model.ModelProviderStatus{CodexModel: config.CodexModel, Mode: config.Mode, Provider: config.Provider, DeepSeekModel: config.DeepSeekModel, CodexFound: codexErr == nil, DeepSeekFound: deepseekErr == nil, CodexEffort: config.CodexEffort, HarnessEffort: config.HarnessEffort}
	return status, nil
}

// SaveModelProviderConfig 只做本地校验与原子保存；下一次生成使用新配置，无测试模型调用。
func (s *Service) SaveModelProviderConfig(ctx context.Context, input *model.ModelProviderInput) (*model.ModelProviderStatus, error) {
	if input == nil {
		return nil, model.Fail("INPUT", "缺少大模型配置")
	}
	s.modelConfigMu.Lock()
	defer s.modelConfigMu.Unlock()
	config := s.modelRouter.Config()
	config.Mode, config.Provider = input.Mode, input.Provider
	if input.Mode == "local" && input.Provider == "codex" && input.CodexEffort != "" {
		config.CodexEffort = input.CodexEffort
	}
	if input.Mode == "local" && input.Provider == "deepseek" && input.HarnessEffort != "" {
		config.HarnessEffort = input.HarnessEffort
	}
	if input.Mode != "local" {
		return nil, model.Fail("INPUT", "只支持本机Harness")
	}
	if input.Provider == "codex" {
		config.CodexModel = input.CodexModel
	}
	if input.Provider == "deepseek" && input.DeepSeekModel != "" {
		config.DeepSeekModel = input.DeepSeekModel
	}

	if err := modelprovider.Validate(&config); err != nil {
		return nil, err
	}
	if err := s.Store.Write(ctx, ".private/model-provider.json", &config); err != nil {
		return nil, err
	}
	if err := s.modelRouter.Configure(&config); err != nil {
		return nil, err
	}
	return s.ModelProviderStatus(ctx)
}

// ConnectModelHarness 只做本机协议握手后切换，从不提交生成输入或平台消息。
func (s *Service) ConnectModelHarness(ctx context.Context, selection *model.HarnessConnectInput) (*model.ModelProviderStatus, error) {
	if selection == nil {
		return nil, model.Fail("INPUT", "缺少本机Harness选择")
	}
	provider := selection.Provider
	if provider == "codex" && selection.CodexModel == "" {
		return nil, model.Fail("INPUT", "请选择明确的Codex模型，不自动选择其他档位")
	}
	if provider != "codex" && provider != "deepseek" {
		return nil, model.Fail("INPUT", "请选择Codex或DeepSeek Harness")
	}
	if provider == "codex" && selection.CodexModel != "" {
		choices, err := s.CodexModels(ctx)
		if err != nil {
			return nil, err
		}
		found := false
		for _, choice := range choices {
			if choice.ID == selection.CodexModel {
				found = true
			}
		}
		if !found {
			return nil, model.Fail("MODEL_UNAVAILABLE", "本机Codex没有提供所选模型，请重新读取模型列表")
		}
	} else {
		if err := businesshost.ProbeLocalHarness(ctx, s.Store.Root, provider, selection.DeepSeekModel, selection.HarnessEffort); err != nil {
			return nil, err
		}
	}
	input := model.ModelProviderInput{Mode: "local", Provider: provider, CodexModel: selection.CodexModel, DeepSeekModel: selection.DeepSeekModel, CodexEffort: selection.CodexEffort, HarnessEffort: selection.HarnessEffort}
	return s.SaveModelProviderConfig(ctx, &input)
}

func (s *Service) CodexModels(ctx context.Context) ([]model.ModelChoice, error) {
	return businesshost.CodexModels(ctx, s.Store.Root)
}

// generatedUsageSource 仅信任项目内已接入的供应商原生回执，不把外部上报当原生计量。
func generatedUsageSource(usage *model.TokenUsage) string {
	if usage.Provider == "deepseek" && usage.Source == "deepseek_harness" {
		return usage.Source
	}
	return "codex_app_server"
}

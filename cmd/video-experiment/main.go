// video-experiment 是显式执行的本地抽帧分析实验，不启用后台任务或平台发送。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"gpt-robot/businesshost"
	"os"
	"path/filepath"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	root := flag.String("root", ".", "项目目录")
	request := flag.String("request", "", "实验请求JSON")
	output := flag.String("output", "", "回执JSON")
	flag.Parse()
	if *request == "" || *output == "" {
		return fmt.Errorf("缺少request/output")
	}
	raw, err := os.ReadFile(*request)
	if err != nil {
		return err
	}
	var input struct {
		Instruction string          `json:"instruction"`
		Body        json.RawMessage `json:"body"`
		Images      []string        `json:"images"`
	}
	if err = json.Unmarshal(raw, &input); err != nil {
		return err
	}
	raw, err = os.ReadFile(filepath.Join(*root, ".private/model-provider.json"))
	if err != nil {
		return err
	}
	var config struct {
		Provider    string `json:"provider"`
		CodexModel  string `json:"codexModel"`
		CodexEffort string `json:"codexEffort"`
	}
	if err = json.Unmarshal(raw, &config); err != nil {
		return err
	}
	if config.Provider != "codex" || config.CodexModel == "" {
		return fmt.Errorf("本实验仅支持当前已选Codex模型，禁止自动切换")
	}
	backend := businesshost.NewCodexBackend(*root, config.CodexModel, config.CodexEffort)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	var result *businesshost.ImageGenerationResult
	var callErr error
	if len(input.Images) == 0 {
		start := time.Now()
		session, openErr := backend.OpenConversation(ctx, input.Instruction)
		result = &businesshost.ImageGenerationResult{StartupMs: time.Since(start).Milliseconds()}
		callErr = openErr
		if openErr == nil {
			defer session.Close()
			start = time.Now()
			result.Reply, callErr = session.GenerateJSON(ctx, input.Instruction, input.Body)
			result.GenerationMs = time.Since(start).Milliseconds()
		}
	} else {
		result, callErr = backend.GenerateImages(ctx, input.Instruction, input.Body, input.Images)
	}
	report := struct {
		Result *businesshost.ImageGenerationResult `json:"result"`
		Error  string                              `json:"error,omitempty"`
	}{Result: result}
	if callErr != nil {
		report.Error = callErr.Error()
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(*output, data, 0600); err != nil {
		return err
	}
	return callErr
}

package businesshost

import (
	"bytes"
	"context"
	"encoding/base64"
	"gpt-robot/model"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"time"
)

// imageExperimentTextMaxBytes 图片实验的字幕正文上限，单位字节。
const imageExperimentTextMaxBytes = 12000

// imageExperimentRequestMaxBytes 图片实验固定指令与正文合计上限，单位字节，不含图片。
const imageExperimentRequestMaxBytes = 16000

// ImageGenerationResult 保存图片实验的真实回执与分段耗时；图片用量包含在供应商输入总量中。
type ImageGenerationResult struct {
	Reply        *model.GeneratedReply `json:"reply"`
	StartupMs    int64                 `json:"startupMs"`
	GenerationMs int64                 `json:"generationMs"`
}

// GenerateImages 仅附加调用方选好的最多六张小图，不开放模型读文件或图像工具。
func (g *CodexBackend) GenerateImages(ctx context.Context, instruction string, body []byte, paths []string) (*ImageGenerationResult, error) {
	if len(paths) < 1 || len(paths) > 6 || len(body) > imageExperimentTextMaxBytes || len(instruction)+len(body) > imageExperimentRequestMaxBytes {
		return nil, model.Fail("INPUT", "图片数量应为1–6且文字不能超过预算")
	}
	inputs := []any{map[string]any{"type": "text", "text": string(body)}}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > 2*1024*1024 {
			return nil, model.Fail("INPUT", "图片必须为不超过2MB的普通文件")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if cfg.Width > 1024 || cfg.Height > 1024 || (format != "jpeg" && format != "png") {
			return nil, model.Fail("INPUT", "仅接收长边不超过1024的JPEG或PNG")
		}
		inputs = append(inputs, map[string]any{"type": "image", "url": "data:image/" + format + ";base64," + base64.StdEncoding.EncodeToString(data)})
	}
	result := &ImageGenerationResult{}
	start := time.Now()
	session, err := g.OpenConversation(ctx, instruction)
	result.StartupMs = time.Since(start).Milliseconds()
	if err != nil {
		return result, err
	}
	defer session.Close()
	start = time.Now()
	result.Reply, err = session.(*codexConversation).generateInputs(ctx, inputs)
	result.GenerationMs = time.Since(start).Milliseconds()
	return result, err
}

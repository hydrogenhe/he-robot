package core

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"gpt-robot/model"
	"os"
	"path/filepath"
)

// providerStickerLimit 限定每个供应商人工整理的可用表情最多24张。
const providerStickerLimit = 24

func providerStickerDirectory(provider string) string {
	switch provider {
	case "codex", "deepseek":
		return "assets/images/stickers/providers/" + provider
	default:
		return ""
	}
}

// loadProviderStickers 只读取人工入库的公开素材，不混入其他私聊或供应商。
func (s *Service) loadProviderStickers(ctx context.Context, provider string) ([]model.LearnedSticker, error) {
	directory := providerStickerDirectory(provider)
	if directory == "" {
		return nil, nil
	}
	library := stickerLibrary{}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// 人工公开素材目录不是机器人业务状态，不经 SQLite 文档迁移或私人学习写入。
	raw, err := os.ReadFile(filepath.Join(s.Store.Root, directory, "catalog.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, model.Internal(err)
	}
	if len(raw) > 128000 {
		return nil, model.Fail("STICKER_LIMIT", "供应商表情索引过大")
	}
	if err := json.Unmarshal(raw, &library); err != nil {
		return nil, model.Fail("STICKER_DATA", "供应商表情索引无法解析")
	}
	if len(library.Items) > providerStickerLimit {
		return nil, model.Fail("STICKER_LIMIT", "供应商表情库超过24张限制")
	}
	for _, item := range library.Items {
		id, err := hex.DecodeString(item.ID)
		if err != nil || len(id) != 32 || !model.Text(item.Label, 20) {
			return nil, model.Fail("STICKER_ID", "供应商表情索引无效")
		}
	}
	return library.Items, nil
}

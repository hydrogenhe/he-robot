package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"gpt-robot/model"
	"io"
	"os"
	"path/filepath"
	"time"
)

// privateStickerLimit 限定每个绑定双人会话最多学习24张表情。
const privateStickerLimit = 24

type stickerLibrary struct {
	Items []model.LearnedSticker `json:"items"`
}

func stickerDirectory(scope *model.ConversationScope) string {
	return "data/private/stickers/" + conversationDigest([]string{scope.Platform, scope.Account, scope.Peer, scope.Channel})
}

func (s *Service) loadStickers(ctx context.Context, scope *model.ConversationScope) ([]model.LearnedSticker, error) {
	s.stickerMu.Lock()
	defer s.stickerMu.Unlock()
	library := stickerLibrary{Items: []model.LearnedSticker{}}
	if _, err := s.Store.Read(ctx, stickerDirectory(scope)+"/index.json", &library); err != nil {
		return nil, err
	}
	if len(library.Items) > privateStickerLimit {
		return nil, model.Fail("STICKER_LIMIT", "本地表情库超过系统限制")
	}
	s.privateMu.Lock()
	withdrawn := map[string]bool{}
	for _, message := range s.privateState.Inbox {
		if message.Withdrawn {
			withdrawn[message.Key] = true
		}
	}
	s.privateMu.Unlock()
	kept := []model.LearnedSticker{}
	for _, item := range library.Items {
		if !withdrawn[item.SourceKey] {
			kept = append(kept, item)
		} else {
			id, err := hex.DecodeString(item.ID)
			if err != nil || len(id) != sha256.Size {
				return nil, model.Fail("STICKER_ID", "本地表情编号无效")
			}
			name, err := s.Store.file(stickerDirectory(scope) + "/" + item.ID + ".image")
			if err != nil {
				return nil, err
			}
			if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
				return nil, model.Internal(err)
			}
		}
	}
	if len(kept) != len(library.Items) {
		library.Items = kept
		if err := s.Store.Write(ctx, stickerDirectory(scope)+"/index.json", &library); err != nil {
			return nil, err
		}
	}
	return kept, nil
}

func (s *Service) stickerImage(ctx context.Context, scope *model.ConversationScope, item *model.LearnedSticker) (*model.ImageAttachment, error) {
	return s.readStickerImage(ctx, stickerDirectory(scope), item)
}

func (s *Service) readStickerImage(ctx context.Context, directory string, item *model.LearnedSticker) (*model.ImageAttachment, error) {
	decoded, err := hex.DecodeString(item.ID)
	if err != nil || len(decoded) != sha256.Size {
		return nil, model.Fail("STICKER_ID", "表情编号无效")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name, err := s.Store.file(directory + "/" + item.ID + ".image")
	if err != nil {
		return nil, err
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, model.Fail("STICKER_MISSING", "已保存表情文件不可用")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, model.StickerMaxBytes+1))
	if err != nil || len(data) > model.StickerMaxBytes {
		return nil, model.Fail("STICKER_SIZE", "本地表情文件无法读取或超过限制")
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != item.ID {
		return nil, model.Fail("STICKER_CHANGED", "本地表情文件校验失败")
	}
	image := item.Image
	image.Data = data
	return &image, nil
}

func (s *Service) saveLearnedSticker(ctx context.Context, round *conversationRound, imageMessage *model.PrivateMessage, label string) (string, error) {
	if !round.config.MemoryEnabled {
		return "", model.Fail("PRIVATE_MEMORY_OFF", "私聊记忆已关闭，这次没有保存图片。")
	}
	if imageMessage == nil || imageMessage.Image == nil {
		return "", model.Fail("CONTENT_SOURCE", "没有找到这张图片，这次没有保存。")
	}
	if _, err := s.loadStickers(ctx, &round.scope); err != nil {
		return "", err
	}

	image, err := s.Bili.DownloadPrivateImage(ctx, imageMessage.Image)
	if err != nil {
		return "", model.Fail("IMAGE_DOWNLOAD", "这张图没存成，需是2MiB以内、2048像素以内的PNG/JPEG/GIF，失效图片请重发。")
	}
	if image == nil || len(image.Data) == 0 || len(image.Data) > model.StickerMaxBytes {
		return "", model.Fail("IMAGE_FORMAT", "这张图没有保存成功，换一张PNG、JPEG或GIF试试吧。")
	}
	if !model.Text(label, 20) {
		label = "你教我的表情"
	}
	label = model.RedactSensitiveText(label, s.generationSecrets(ctx)...)
	sum := sha256.Sum256(image.Data)
	id := hex.EncodeToString(sum[:])
	s.stickerMu.Lock()
	defer s.stickerMu.Unlock()
	s.privateMu.Lock()
	defer s.privateMu.Unlock()
	allowed := s.privateConfig == round.config && s.privateState.Processing == round.message.Key
	if !allowed || ctx.Err() != nil {
		return "", model.Fail("PRIVATE_SCOPE", "表情学习期间会话授权已变化")
	}
	library := stickerLibrary{Items: []model.LearnedSticker{}}
	if _, err := s.Store.Read(ctx, stickerDirectory(&round.scope)+"/index.json", &library); err != nil {
		return "", err
	}
	for i, item := range library.Items {
		if item.ID == id {
			library.Items[i].Label = label
			library.Items[i].SourceKey = imageMessage.Key
			if err := s.Store.Write(ctx, stickerDirectory(&round.scope)+"/index.json", &library); err != nil {
				return "", err
			}
			return id, nil
		}
	}
	if len(library.Items) >= privateStickerLimit {
		return "", model.Fail("STICKER_LIMIT", "表情库已经收了24张啦，这次没有再保存。")
	}
	name, err := s.Store.file(stickerDirectory(&round.scope) + "/" + id + ".image")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return "", model.Internal(err)
	}
	if err := os.WriteFile(name, image.Data, 0600); err != nil {
		return "", model.Internal(err)
	}
	metadata := *image
	metadata.Data = nil
	item := model.LearnedSticker{ID: id, Label: label, Image: metadata, SourceKey: imageMessage.Key, At: time.Now().UTC().Format(time.RFC3339Nano)}
	library.Items = append(library.Items, item)
	if err := s.Store.Write(ctx, stickerDirectory(&round.scope)+"/index.json", &library); err != nil {
		return "", err
	}
	return id, nil
}

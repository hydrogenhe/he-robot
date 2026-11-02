package core

import (
	"context"
	"gpt-robot/model"
	"time"
)

// privateContentRecentSeconds 仅把最近三分钟真实收到的附件列为当前可操作内容，单位秒。
const privateContentRecentSeconds = 180

// preparePrivateContents 归一化真实输入；理解“看/学习/记住”的语义由模型完成，不使用口令正则。
func (s *Service) preparePrivateContents(round *conversationRound, prompt *model.ReplyPrompt) {
	round.contents = map[string]model.PrivateMessage{}
	s.privateMu.Lock()
	defer s.privateMu.Unlock()
	for i := len(s.privateState.Inbox) - 1; i >= 0 && len(prompt.Contents) < 6; i-- {
		message := s.privateState.Inbox[i]
		if message.Sender != round.config.PeerUID || message.Receiver != round.config.SenderUID || message.Withdrawn || message.Seq > round.message.Seq || message.Timestamp > round.message.Timestamp || round.message.Timestamp-message.Timestamp > privateContentRecentSeconds {
			continue
		}
		if message.Image == nil {
			continue
		}
		id := "image:" + message.Key
		round.contents[id] = message
		description := "主人发来的图片或表情；仅有原文件来源，尚未识图。"
		if !round.config.MemoryEnabled {
			description += "私聊记忆关闭，不能保存。"
		}
		prompt.Contents = append(prompt.Contents, model.ContentDescriptor{ID: id, Kind: "image", Description: description, At: time.Unix(message.Timestamp, 0).UTC().Format(time.RFC3339)})
	}
	if round.config.MemoryEnabled {
		prompt.Capabilities = []model.ContentCapability{{Action: "save_content", Kind: "image", Purpose: "sticker"}}
	}
}

// executeContentActions 是内容动作执行边界；新文件/视频能力在此按明确能力接入，不在输入文案中匹配关键词。
func (s *Service) executeContentActions(ctx context.Context, round *conversationRound, actions []model.ContentAction) error {
	if len(actions) == 0 {
		return nil
	}
	if len(actions) > 1 {
		return model.Fail("CONTENT_ACTION", "这次请求的内容动作太多，没有执行。")
	}
	action := actions[0]
	if action.Action != "save_content" || action.Purpose != "sticker" {
		return model.Fail("CONTENT_UNSUPPORTED", "这种内容操作还没有接入，这次没有读取或保存。")
	}
	source, ok := round.contents[action.ContentID]
	if !ok || source.Image == nil {
		return model.Fail("CONTENT_SOURCE", "没有找到你指的图片，请把它发来，这次没有保存。")
	}
	_, err := s.saveLearnedSticker(ctx, round, &source, action.Label)
	return err
}

func (s *Service) contentActionFeedback(ctx context.Context, round *conversationRound, result *model.GeneratedReply) {
	if len(result.Actions) == 0 {
		return
	}
	if err := s.executeContentActions(ctx, round, result.Actions); err != nil {
		// 不再调用模型总结失败；废弃可能已预言成功的会话，确保实际发出的内容真实。
		s.modelRouter.CloseConversations()
		text := "这次没有保存成功，稍后再试试吧。"
		if business, ok := err.(*model.Error); ok {
			switch business.Code {
			case "CONTENT_UNSUPPORTED", "CONTENT_SOURCE", "CONTENT_ACTION", "PRIVATE_MEMORY_OFF", "IMAGE_DOWNLOAD", "IMAGE_FORMAT", "STICKER_LIMIT":
				text = business.Message
			}
		}
		if len([]rune(text)) > 60 {
			text = model.Clip(text, 59)
		}
		result.Text = text
		result.Messages = []string{text}
		result.StickerID = ""
	} else if result.Text == "" {
		result.Text = "这张表情包保存好啦，下次我也可以用它。"
		result.Messages = []string{result.Text}
	}
}

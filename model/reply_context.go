package model

import (
	"encoding/json"
	"strings"
)

const (
	// ReplyContextMaxBytes 单次回复业务 JSON 的硬上限（字节），不冒充模型分词后的 Token 数。
	ReplyContextMaxBytes = 6000
	// ReplyRecentLimit 近期对话最多保留的消息条数，不随历史增长。
	ReplyRecentLimit = 6
	// ReplyRecentRunes 近期对话的总字符预算，包含说话人标签。
	ReplyRecentRunes = 480
	// ReplyMemoryLimit 单次回复最多注入的相关记忆条数。
	ReplyMemoryLimit = 3
)

// BoundReplyPrompt 在业务入口和模型边界共用预算；仅复制输入，不修改调用方对象。
func BoundReplyPrompt(input *ReplyPrompt) (*ReplyPrompt, error) {
	return BoundReplyPromptBudget(input, ReplyContextMaxBytes)
}

// BoundReplyPromptBudget 在总请求预算扣除业务指令后，沿用同一裁剪优先级。
func BoundReplyPromptBudget(input *ReplyPrompt, maxBytes int) (*ReplyPrompt, error) {
	if maxBytes <= 0 || maxBytes > ReplyContextMaxBytes {
		return nil, Fail("CONTEXT_BUDGET", "上下文预算无效")
	}
	if input == nil || !Text(input.Text, 500) {
		return nil, Fail("INPUT", "缺少消息或消息超过500字")
	}
	if input.Channel != "private-message" && input.Channel != "video-comment" {
		return nil, Fail("INPUT", "不支持的回复渠道")
	}
	if input.Channel != "private-message" && (input.CompanionContext != "" || len(input.SelfCapabilities) > 0 || input.SelfResult != nil || len(input.PrivateMemories) > 0 || len(input.Recent) > 0 || len(input.Handoff) > 0 || input.Conversation != nil || len(input.Stickers) > 0 || input.ReplyParts > 0 || input.AnalyzeSticker || input.SelectSticker || input.StickerIntent != nil || input.Draft != "") {
		return nil, Fail("PRIVATE_SCOPE", "公开生成不可携带私聊记忆或会话历史")
	}
	result := &ReplyPrompt{
		AnalyzeSticker: input.AnalyzeSticker,
		SelectSticker:  input.SelectSticker,
		Draft:          Clip(input.Draft, 60),
		StickerIntent:  BoundStickerIntent(input.StickerIntent),
		AllowRecall:    input.AllowRecall,
		RecallComplete: input.RecallComplete,
		ReplyParts:     min(3, max(0, input.ReplyParts)),
		Conversation:   input.Conversation,
		Owner:          input.Owner,
		Development:    Clip(input.Development, 240),
		Persona:        Clip(input.Persona, 800),
		State:          Clip(input.State, 240),
		Channel:        input.Channel,
		Text:           input.Text,
	}
	for _, episode := range input.Episodes[:min(3, len(input.Episodes))] {
		if episode.Private && input.Channel != "private-message" {
			return nil, Fail("PRIVATE_SCOPE", "公开回复不可携带私密经历")
		}
		item := RecalledEpisode{Private: episode.Private, Summary: Clip(episode.Summary, 160), Reaction: Clip(episode.Reaction, 60), OccurredAt: episode.OccurredAt, SourceID: episode.SourceID}
		result.Episodes = append(result.Episodes, item)
	}
	if input.Channel == "private-message" && input.Owner {
		result.CompanionContext = Clip(input.CompanionContext, 700)
		result.SelfToday = Clip(input.SelfToday, 10)
		result.SelfCapabilities = append([]string{}, input.SelfCapabilities[:min(7, len(input.SelfCapabilities))]...)
		if input.SelfResult != nil {
			result.SelfResult = &SelfQueryResult{Resource: Clip(input.SelfResult.Resource, 20), Records: []SelfRecord{}}
			for _, item := range input.SelfResult.Records[:min(5, len(input.SelfResult.Records))] {
				record := SelfRecord{Date: Clip(item.Date, 35), Title: Clip(item.Title, 60), Text: Clip(item.Text, 160), Status: Clip(item.Status, 24), Source: Clip(item.Source, 80)}
				result.SelfResult.Records = append(result.SelfResult.Records, record)
			}
		}
	}
	if input.Channel == "private-message" {
		for _, item := range input.Contents[:min(6, len(input.Contents))] {
			if Text(item.ID, 80) && Text(item.Kind, 20) && Text(item.Description, 120) {
				item.Description = Clip(item.Description, 120)
				result.Contents = append(result.Contents, item)
			}
		}
		result.Capabilities = append([]ContentCapability{}, input.Capabilities[:min(4, len(input.Capabilities))]...)
		for _, item := range input.Stickers[:min(8, len(input.Stickers))] {
			if Text(item.ID, 80) && Text(item.Label, 30) {
				item.StickerMeaning = BoundStickerMeaning(&item.StickerMeaning)
				result.Stickers = append(result.Stickers, item)
			}
		}
	}
	if input.Channel == "private-message" {
		for _, text := range input.Handoff[:min(4, len(input.Handoff))] {
			result.Handoff = append(result.Handoff, Clip(text, 160))
		}
	}
	for _, topic := range input.InterestCandidates[:min(3, len(input.InterestCandidates))] {
		if Text(topic, 20) {
			result.InterestCandidates = append(result.InterestCandidates, topic)
		}
	}
	remaining := ReplyRecentRunes
	for i := len(input.Recent) - 1; i >= 0 && len(result.Recent) < ReplyRecentLimit && remaining > 0; i-- {
		row := Clip(strings.TrimSpace(input.Recent[i]), min(120, remaining))
		if row == "" {
			continue
		}
		result.Recent = append(result.Recent, row)
		remaining -= len([]rune(row))
	}
	for i, j := 0, len(result.Recent)-1; i < j; i, j = i+1, j-1 {
		result.Recent[i], result.Recent[j] = result.Recent[j], result.Recent[i]
	}
	for _, record := range input.PrivateMemories {
		if record.Quote == "" {
			continue
		}
		item := RecalledMemory{
			Quote:      Clip(record.Quote, 160),
			OccurredAt: Clip(record.OccurredAt, 35),
			MessageKey: Clip(record.MessageKey, 32),
		}
		result.PrivateMemories = append(result.PrivateMemories, item)
		if len(result.PrivateMemories) == ReplyMemoryLimit {
			break
		}
	}
	for _, record := range input.PublicMemories {
		if record.Summary == "" {
			continue
		}
		item := RecalledPublicMemory{
			Summary:    Clip(record.Summary, 160),
			OccurredAt: Clip(record.OccurredAt, 35),
			SourceID:   Clip(record.SourceID, 36),
		}
		result.PublicMemories = append(result.PublicMemories, item)
		if len(result.PublicMemories) == ReplyMemoryLimit {
			break
		}
	}
	// JSON 转义和多字节字符也计入硬预算；优先保留当前消息和核心人设。
	for {
		body, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		if len(body) <= maxBytes {
			return result, nil
		}
		switch {
		case len(result.Stickers) > 0:
			result.Stickers = result.Stickers[:len(result.Stickers)-1]
		case len(result.Handoff) > 0:
			result.Handoff = result.Handoff[1:]
		case len(result.Recent) > 0:
			result.Recent = result.Recent[1:]
		case len(result.PublicMemories) > 0:
			result.PublicMemories = result.PublicMemories[:len(result.PublicMemories)-1]
		case len(result.PrivateMemories) > 0:
			result.PrivateMemories = result.PrivateMemories[:len(result.PrivateMemories)-1]
		case result.Development != "":
			result.Development = ""
		case result.State != "":
			result.State = ""
		case result.Persona != "":
			result.Persona = Clip(result.Persona, max(0, len([]rune(result.Persona))-100))
		default:
			return nil, Fail("CONTEXT_BUDGET", "当前消息超出上下文预算")
		}
	}
}

// BoundStickerIntent 统一限制首轮标签，防止模型输出扩大选图上下文。
func BoundStickerIntent(input *StickerIntent) *StickerIntent {
	if input == nil {
		return nil
	}
	result := &StickerIntent{Suitable: input.Suitable, ImageOnly: input.ImageOnly, Scene: Clip(input.Scene, 30)}
	for _, tag := range input.Tags[:min(6, len(input.Tags))] {
		if Text(tag, 12) {
			result.Tags = append(result.Tags, tag)
		}
	}
	return result
}

// BoundStickerMeaning 对人工与学习素材的模型描述采用同一有界协议。
func BoundStickerMeaning(input *StickerMeaning) StickerMeaning {
	if input == nil {
		return StickerMeaning{}
	}
	result := StickerMeaning{Description: Clip(input.Description, 80), Caption: Clip(input.Caption, 60), Avoid: Clip(input.Avoid, 60), Direction: Clip(input.Direction, 30)}
	for _, tag := range input.Tags[:min(6, len(input.Tags))] {
		if Text(tag, 12) {
			result.Tags = append(result.Tags, tag)
		}
	}
	for _, scene := range input.Scenes[:min(4, len(input.Scenes))] {
		if Text(scene, 30) {
			result.Scenes = append(result.Scenes, scene)
		}
	}
	return result
}

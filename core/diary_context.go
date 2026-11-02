package core

import (
	"context"
	"gpt-robot/model"
	"strings"
	"time"
)

// diaryPrompt 汇总已授权的跨平台经历，私密素材仅供本机日记；不读取旧日记或全量聊天。
func (s *Service) diaryPrompt(ctx context.Context, day string) (*model.DiaryPrompt, error) {
	persona, err := s.Store.Text(ctx, "data/character/persona.md")
	if err != nil {
		return nil, err
	}
	state, err := s.CharacterStatus(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.bilibiliDiaryExperiences(ctx, day)
	if err != nil {
		return nil, err
	}
	for _, source := range s.DiarySources {
		if source == nil {
			continue
		}
		items, err := source.DiaryExperiences(ctx, day)
		if err != nil {
			return nil, err
		}
		rows = append(rows, items[:min(200, len(items))]...)
	}
	events, err := s.Store.Events(ctx, 200)
	if err != nil {
		return nil, err
	}
	s.autoMu.Lock()
	owner := s.autoConfig.PriorityUID
	account := s.autoConfig.AccountUID
	s.autoMu.Unlock()
	seen := map[string]bool{}
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		at, err := time.Parse(time.RFC3339Nano, event.At)
		if err != nil || diaryDay(at) != day {
			continue
		}
		summary := ""
		switch event.Kind {
		case "comments":
			summary = "只读到视频评论材料，未看视频画面：" + model.Clip(event.Summary, 70)
			rows = append(rows, diaryPublicComments(&event, owner, account)...)
		case "video-browsed":
			summary = "只读取视频标题、简介与评论资料，未看画面/音轨：" + model.Clip(event.Summary, 80)
		case "sent":
			summary = "向B站提交过一条公开回复或动态，不代表已通过公开展示审核。"
		default:
			continue
		}
		key := event.Kind + strings.ToLower(summary)
		if seen[key] {
			continue
		}
		seen[key] = true
		item := model.DiaryExperience{Platform: "bilibili", Account: account, Source: event.ID, OccurredAt: event.At, Visibility: "public", Kind: event.Kind, Text: summary}
		rows = append(rows, item)
	}
	s.developmentDiaryWeights(rows)
	s.diaryMu.Lock()
	config := s.diaryConfig
	s.diaryMu.Unlock()
	prompt := &model.DiaryPrompt{Date: day, Persona: persona, State: state.Description, Facts: diaryExperienceFacts(day, rows, &config, persona)}
	return model.BoundDiaryPrompt(prompt)
}

// diaryPublicComments 只从已保存的公开评论提取内容，作者关系按稳定UID匹配，昵称不参与绑定。
func diaryPublicComments(event *model.Event, owner, account string) []model.DiaryExperience {
	result := []model.DiaryExperience{}
	comments, ok := event.Data["comments"].([]any)
	if !ok {
		return result
	}
	for _, value := range comments[:min(20, len(comments))] {
		row, ok := value.(map[string]any)
		if !ok {
			continue
		}
		id, _ := row["id"].(string)
		text, _ := row["text"].(string)
		uid, _ := row["uid"].(string)
		if !model.ValidNumber(id) || id == "0" || text == "" {
			continue
		}
		relation := ""
		speaker := "今天读到的公开评论："
		if owner != "" && uid == owner {
			relation = "owner"
			speaker = "今天读到主人公开评论说："
		}
		item := model.DiaryExperience{Platform: "bilibili", Account: account, Conversation: uid, Relationship: relation, Source: "comment:" + id, OccurredAt: event.At, Visibility: "public", Kind: "public_chat", Text: speaker + text}
		result = append(result, item)
	}
	return result
}

package core

import (
	"context"
	"encoding/json"
	"gpt-robot/model"
	"gpt-robot/storage"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// recentPrivateContext 只取当前绑定会话、目标消息之前30分钟内的最近六条。
func recentPrivateContext(inbox []model.PrivateMessage, selected *model.PrivateMessage, config *model.PrivateConfig) []string {
	recent := []string{}
	remaining := model.ReplyRecentRunes
	for i := len(inbox) - 1; i >= 0 && len(recent) < model.ReplyRecentLimit && remaining > 0; i-- {
		row := inbox[i]
		bound := row.Sender == config.SenderUID && row.Receiver == config.PeerUID || row.Sender == config.PeerUID && row.Receiver == config.SenderUID
		if !bound || row.Seq >= selected.Seq || row.Withdrawn || row.Text == "" || row.Timestamp < selected.Timestamp-1800 || row.Timestamp > selected.Timestamp {
			continue
		}
		speaker := "大号："
		if row.Sender == config.SenderUID {
			speaker = "小龙："
		}
		text := model.Clip(speaker+row.Text, min(120, remaining))
		recent = append(recent, text)
		remaining -= len([]rune(text))
	}
	slices.Reverse(recent)
	return recent
}

type scoredPublicMemory struct {
	memory model.RecalledPublicMemory
	score  int
}

// publicReplyMemories 只读公共笔记库，先相关性筛选再取最多三条；不读私聊目录或全量经历。
func (s *Service) publicReplyMemories(ctx context.Context, text string) ([]model.RecalledPublicMemory, error) {
	tokens := memoryTokens(text)
	date := memoryQueryDate(text, time.Now())
	if len(tokens) == 0 && date == "" {
		return nil, nil
	}
	notes, err := s.publicMemoryCandidates(ctx, text)
	if err != nil {
		return nil, err
	}
	matches := []scoredPublicMemory{}
	for _, note := range notes {
		if note.Kind != "memory" || len(note.Sources) == 0 {
			continue
		}
		if date != "" {
			at, err := time.Parse(time.RFC3339Nano, note.At)
			if err != nil || at.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02") != date {
				continue
			}
		}
		content := strings.ToLower(note.Title + " " + note.Text)
		score := 0
		for _, token := range tokens {
			if strings.Contains(content, token) {
				score += min(len([]rune(token)), 6)
			}
		}
		if len(tokens) > 0 && score == 0 {
			continue
		}
		memory := model.RecalledPublicMemory{
			Summary:    memoryExcerpt(note.Title+"："+note.Text, text, 160),
			OccurredAt: note.At,
			SourceID:   note.ID,
		}
		item := scoredPublicMemory{memory: memory, score: score}
		matches = append(matches, item)
		slices.SortFunc(matches, comparePublicMemory)
		if len(matches) > model.ReplyMemoryLimit {
			matches = matches[:model.ReplyMemoryLimit]
		}
	}
	result := []model.RecalledPublicMemory{}
	for _, item := range matches {
		result = append(result, item.memory)
	}
	return result, nil
}

// memoryExcerpt 围绕命中的最具体关键词截取，避免只取开头而丢掉长记录末尾的相关事实。
func memoryExcerpt(text, query string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	lower := strings.ToLower(text)
	position, specificity := 0, 0
	for _, token := range memoryTokens(query) {
		index := strings.Index(lower, token)
		size := utf8.RuneCountInString(token)
		if index >= 0 && size > specificity {
			position, specificity = utf8.RuneCountInString(lower[:index]), size
		}
	}
	start := max(0, position-limit/3)
	return string(runes[start:min(start+limit, len(runes))])
}
func comparePublicMemory(a, b scoredPublicMemory) int {
	if a.score != b.score {
		return b.score - a.score
	}
	if a.memory.OccurredAt != b.memory.OccurredAt {
		return strings.Compare(b.memory.OccurredAt, a.memory.OccurredAt)
	}
	return strings.Compare(a.memory.SourceID, b.memory.SourceID)
}

// compactPublicNote 避免工具调用把长篇笔记和全部来源反复传给宿主；完整内容保留在本地。
func compactPublicNote(note *model.Note) model.Note {
	result := *note
	result.Title = model.Clip(note.Title, 100)
	result.Text = model.Clip(note.Text, 160)
	result.Sources = append([]string{}, note.Sources[:min(3, len(note.Sources))]...)
	return result
}

// publicMemoryCandidates 将查询成本限制在数据库返回的候选内。
func (s *Service) publicMemoryCandidates(ctx context.Context, text string) ([]model.Note, error) {
	if s.Store.Database == nil {
		return s.Store.Notes(ctx)
	}
	input := storage.MemoryQuery{Namespace: "data/memory/notes.json", Terms: memoryTokens(text), Limit: 60}
	if date := memoryQueryDate(text, time.Now()); date != "" {
		if at, err := time.ParseInLocation("2006-01-02", date, time.FixedZone("Asia/Shanghai", 8*60*60)); err == nil {
			input.Since = at.UnixMilli()
			input.Until = at.AddDate(0, 0, 1).UnixMilli()
		}
	}
	rows, err := s.Store.Database.MemoryCandidates(ctx, &input)
	if err != nil {
		return nil, err
	}
	notes := []model.Note{}
	for _, raw := range rows {
		var note model.Note
		if err = json.Unmarshal(raw, &note); err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}
	return notes, nil
}

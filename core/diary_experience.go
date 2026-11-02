package core

import (
	"context"
	"crypto/sha256"
	"fmt"
	"gpt-robot/model"
	"regexp"
	"slices"
	"strings"
	"time"
)

// diaryExperienceFacts 各平台共用筛选、来源去重和预算；日记素材仅流入私有日记宿主。
func diaryExperienceFacts(day string, rows []model.DiaryExperience, config *model.DiaryConfig, persona string) []model.DiaryFact {
	topics := diaryInterestTopics(persona, config.Topics)
	slices.SortStableFunc(rows, func(a, b model.DiaryExperience) int {
		sourceScore, targetScore := diaryExperienceWeight(&a, config, topics), diaryExperienceWeight(&b, config, topics)
		if sourceScore != targetScore {
			return targetScore - sourceScore
		}
		sourceTime, _ := time.Parse(time.RFC3339Nano, a.OccurredAt)
		targetTime, _ := time.Parse(time.RFC3339Nano, b.OccurredAt)
		return targetTime.Compare(sourceTime)
	})
	facts := []model.DiaryFact{}
	seen := map[string]bool{}
	privateCount := 0
	for _, row := range rows {
		at, err := time.Parse(time.RFC3339Nano, row.OccurredAt)
		if err != nil || diaryDay(at) != day || row.Withdrawn || row.Text == "" || row.Source == "" || row.Platform == "" || row.Kind == "" {
			continue
		}
		if row.Visibility != "public" && row.Visibility != "private" && row.Visibility != "group" {
			continue
		}
		if row.Visibility != "public" && (row.Account == "" || row.Conversation == "" || privateCount >= 6) {
			continue
		}
		if privateSecret.MatchString(row.Text) {
			continue
		}
		digest := sha256.Sum256([]byte(row.Platform + "\x00" + row.Account + "\x00" + row.Conversation + "\x00" + row.Source))
		source := fmt.Sprintf("%x", digest)
		if seen[source] {
			continue
		}
		seen[source] = true
		fact := model.DiaryFact{Weight: diaryExperienceWeight(&row, config, topics), Source: source, Platform: model.Clip(row.Platform, 24), Visibility: row.Visibility, Kind: model.Clip(row.Kind, 32), Summary: model.Clip(row.Text, 100)}
		facts = append(facts, fact)
		if row.Visibility != "public" {
			privateCount++
		}
		if len(facts) == 12 {
			break
		}
	}
	return facts
}

// bilibiliDiaryExperiences 只读取当前绑定会话的缓存，遵循记忆开关、撤回及遗忘；不请求B站。
func (s *Service) bilibiliDiaryExperiences(ctx context.Context, day string) ([]model.DiaryExperience, error) {
	s.privateMu.Lock()
	config := s.privateConfig
	inbox := slices.Clone(s.privateState.Inbox)
	s.privateMu.Unlock()
	if !config.Enabled || !config.MemoryEnabled {
		return nil, nil
	}
	if err := s.privateAccount(ctx, &config); err != nil {
		return nil, err
	}
	s.privateMemoryMu.Lock()
	stored, err := s.loadPrivateMemory(ctx, &config)
	if err != nil {
		s.privateMemoryMu.Unlock()
		return nil, err
	}
	memory := clonePrivateMemoryState(stored)
	s.privateMemoryMu.Unlock()
	if !memory.Initialized || memory.Generation != config.MemoryGeneration {
		return nil, nil
	}
	excluded := map[string]bool{}
	for _, record := range memory.Records {
		for _, source := range record.Sources {
			if source.Withdrawn || record.Quote == "" {
				excluded[source.MessageKey] = true
			}
		}
	}
	s.autoMu.Lock()
	owner := s.autoConfig.AccountUID == config.SenderUID && s.autoConfig.PriorityUID == config.PeerUID
	s.autoMu.Unlock()
	result := []model.DiaryExperience{}
	for _, row := range inbox {
		bound := row.Sender == config.PeerUID && row.Receiver == config.SenderUID || row.Sender == config.SenderUID && row.Receiver == config.PeerUID
		if !bound || row.Withdrawn || excluded[row.Key] || row.Timestamp < memory.Since || row.Timestamp > s.DiaryNow().Add(5*time.Minute).Unix() || diaryDay(time.Unix(row.Timestamp, 0)) != day {
			continue
		}
		speaker := "对方说："
		if owner {
			speaker = "主人说："
		}
		if row.Sender == config.SenderUID {
			speaker = "我实际发出的私信："
		}
		relationship := ""
		if owner {
			relationship = "owner"
		}
		item := model.DiaryExperience{Relationship: relationship, Platform: "bilibili", Account: config.SenderUID, Conversation: config.PeerUID, Source: row.Key, OccurredAt: time.Unix(row.Timestamp, 0).UTC().Format(time.RFC3339Nano), Visibility: "private", Kind: "chat", Text: speaker + row.Text}
		result = append(result, item)
	}
	return result, nil
}

func validateDiaryConfig(config *model.DiaryConfig) error {
	if config == nil || config.OwnerWeight < 0 || config.OwnerWeight > 10 || config.InterestWeight < 0 || config.InterestWeight > 10 || len(config.Topics) > 10 {
		return model.Fail("INPUT", "日记权重须为0–10，兴趣关键词最多10个")
	}
	for _, topic := range config.Topics {
		if !model.Text(topic, 20) || strings.TrimSpace(topic) != topic {
			return model.Fail("INPUT", "兴趣关键词须为1–20字，不含首尾空格")
		}
	}
	return nil
}

var diaryPositiveInterest = regexp.MustCompile(`(?:喜欢|爱好|偏好)[：:是\s]*([^。！!？?\n]{1,80})`)

func diaryInterestTopics(persona string, configured []string) []string {
	topics := append([]string{}, configured...)
	for _, line := range strings.Split(persona, "\n") {
		if strings.Contains(line, "不喜欢") || strings.Contains(line, "不爱好") || strings.Contains(line, "不偏好") {
			continue
		}
		for _, match := range diaryPositiveInterest.FindAllStringSubmatch(line, -1) {
			for _, word := range strings.FieldsFunc(match[1], func(r rune) bool { return strings.ContainsRune("、，,；;和与", r) }) {
				word = strings.TrimSpace(word)
				if model.Text(word, 20) {
					topics = append(topics, word)
				}
			}
		}
	}
	return topics[:min(20, len(topics))]
}
func diaryExperienceWeight(row *model.DiaryExperience, config *model.DiaryConfig, topics []string) int {
	weight := 1 + min(6, max(0, row.LearnedWeight))
	if row.Relationship == "owner" {
		weight += config.OwnerWeight
	}
	text := strings.ToLower(row.Text)
	for _, topic := range topics {
		if strings.Contains(text, strings.ToLower(topic)) {
			weight += config.InterestWeight
			break
		}
	}
	return weight
}

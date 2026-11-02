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

var developmentIdentifier = regexp.MustCompile(`^[A-Za-z0-9_.:/@-]{1,160}$`)

func (s *Service) initializeDevelopment(ctx context.Context) error {
	state := model.DevelopmentState{StartedAt: s.DevelopmentNow().UTC().Truncate(time.Second).Format(time.RFC3339Nano), Seen: map[string]bool{}, Interests: []model.DevelopedInterest{}, Relationships: []model.DevelopedRelationship{}}
	found, err := s.Store.Read(ctx, "data/private/development.json", &state)
	if err != nil {
		return err
	}
	if _, err := time.Parse(time.RFC3339Nano, state.StartedAt); err != nil || len(state.Seen) > 5000 || len(state.Interests) > 200 || len(state.Relationships) > 200 {
		return model.Fail("DATA", "兴趣关系档案无效，未覆盖")
	}
	for _, item := range state.Interests {
		if item.Preference < -100 || item.Preference > 100 || item.Familiarity < 0 || (state.EngineVersion == 0 && item.Familiarity > 100) || item.Pending < -4 || item.Pending > 4 {
			return model.Fail("DATA", "兴趣成长值无效")
		}
	}
	for _, item := range state.Relationships {
		if item.Score < 0 || (state.EngineVersion == 0 && item.Score > 100) {
			return model.Fail("DATA", "关系成长值无效")
		}
	}
	if state.Seen == nil {
		state.Seen = map[string]bool{}
	}
	if !found {
		if err := s.Store.Write(ctx, "data/private/development.json", &state); err != nil {
			return err
		}
	}
	s.development = state
	return nil
}

// RecordDevelopment 只接受已验证平台路径提供的新经历；原文仅用于本地匹配，不写入成长档案。
func (s *Service) RecordDevelopment(ctx context.Context, input *model.DevelopmentInput) error {
	if input == nil || !developmentIdentifier.MatchString(input.Platform) || len(input.Platform) > 24 || !developmentIdentifier.MatchString(input.Account) || !developmentIdentifier.MatchString(input.Source) || input.Peer != "" && !developmentIdentifier.MatchString(input.Peer) || !model.Text(input.Account, 80) || !model.Text(input.Source, 160) || !model.Text(input.Text, 500) || input.Visibility != "public" && input.Visibility != "private" || input.Interaction && !model.Text(input.Peer, 80) || input.Visibility == "private" && input.Peer == "" || input.RelationDelta < -1 || input.RelationDelta > 1 {
		return model.Fail("INPUT", "成长经历缺少来源或隐私范围")
	}
	at, err := time.Parse(time.RFC3339Nano, input.At)
	if err != nil {
		return model.Fail("INPUT", "成长经历时间无效")
	}
	if privateSecret.MatchString(input.Text) {
		return nil
	}
	topics := s.developmentCandidates(input.Text)
	for _, signal := range input.Interests[:min(2, len(input.Interests))] {
		if model.Text(signal.Topic, 20) && strings.Contains(input.Text, signal.Topic) && !slices.Contains(topics, signal.Topic) && len(topics) < 3 {
			topics = append(topics, signal.Topic)
		}
	}
	if len(topics) == 0 && !input.Interaction {
		return nil
	}
	s.developmentMu.Lock()
	defer s.developmentMu.Unlock()
	if s.Store.Database != nil {
		return s.recordGrowthV1(ctx, input, topics)
	}
	start, _ := time.Parse(time.RFC3339Nano, s.development.StartedAt)
	day := diaryDay(at)
	if at.Before(start) || at.After(s.DevelopmentNow().Add(5*time.Minute)) || day < s.development.Day {
		return nil
	}
	hash := sha256.Sum256([]byte(input.Platform + "\x00" + input.Account + "\x00" + input.Visibility + "\x00" + input.Peer + "\x00" + input.Source))
	source := fmt.Sprintf("%x", hash)
	if day == s.development.Day && s.development.Seen[source] {
		return nil
	}
	state := s.development
	state.Seen = map[string]bool{}
	if day == state.Day {
		for key, value := range s.development.Seen {
			state.Seen[key] = value
		}
	}
	if len(state.Seen) >= 5000 {
		return model.Fail("DEVELOPMENT_LIMIT", "当天成长来源达到本地上限，不继续累积")
	}
	state.Day = day
	state.Seen[source] = true
	state.Interests = slices.Clone(state.Interests)
	state.Relationships = slices.Clone(state.Relationships)
	for _, topic := range topics {
		index := -1
		conversation := ""
		if input.Visibility == "private" {
			conversation = input.Peer
		}
		for i, item := range state.Interests {
			if item.Platform == input.Platform && item.Account == input.Account && item.Visibility == input.Visibility && item.Conversation == conversation && item.Topic == topic {
				index = i
				break
			}
		}
		if index < 0 {
			if len(state.Interests) >= 200 {
				continue
			}
			item := model.DevelopedInterest{Platform: input.Platform, Account: input.Account, Conversation: conversation, Visibility: input.Visibility, Topic: topic}
			state.Interests = append(state.Interests, item)
			index = len(state.Interests) - 1
		}
		item := &state.Interests[index]
		if item.Day != day {
			item.Day = day
			item.Daily = 0
		}
		if item.Daily >= 2 {
			continue
		}
		item.Daily++
		item.Familiarity = min(100, item.Familiarity+1)
		item.UpdatedAt = input.At
		item.Source = source
		item.Reference = input.Source
		for _, signal := range input.Interests[:min(2, len(input.Interests))] {
			if signal.Topic == topic && signal.Delta >= -1 && signal.Delta <= 1 {
				item.Pending += signal.Delta
				item.Evidence++
				break
			}
		}
		if item.Evidence > 0 && item.Evidence%5 == 0 {
			item.Preference = max(-100, min(100, item.Preference+traitStep(item.Pending)))
			item.Pending = 0
		}
	}
	if input.Interaction {
		index := -1
		for i, item := range state.Relationships {
			if item.Platform == input.Platform && (item.Account == "" || item.Account == input.Account) && item.Peer == input.Peer {
				index = i
				break
			}
		}
		if index < 0 && len(state.Relationships) < 200 {
			item := model.DevelopedRelationship{Platform: input.Platform, Account: input.Account, Peer: input.Peer, Visibility: input.Visibility}
			state.Relationships = append(state.Relationships, item)
			index = len(state.Relationships) - 1
		}
		if index >= 0 {
			item := &state.Relationships[index]
			if item.Day != day {
				item.Day = day
				item.Daily = 0
				item.Days++
			}
			if item.Daily < 3 {
				item.Daily++
				step := 1
				if input.RelationDelta < 0 {
					step = -1
				}
				item.Score = max(0, min(100, item.Score+step))
				item.Interactions++
				item.UpdatedAt = input.At
				item.Source = source
				item.Reference = input.Source
			}
		}
	}
	if err := s.Store.Write(context.WithoutCancel(ctx), "data/private/development.json", &state); err != nil {
		return err
	}
	s.development = state
	return nil
}

func (s *Service) DevelopmentStatus(ctx context.Context) (*model.DevelopmentOverview, error) {
	if err := s.flushGrowth(ctx); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.developmentMu.Lock()
	result := &model.DevelopmentOverview{Interests: slices.Clone(s.development.Interests), Relationships: slices.Clone(s.development.Relationships)}
	s.developmentMu.Unlock()
	s.autoMu.Lock()
	account, owner := s.autoConfig.AccountUID, s.autoConfig.PriorityUID
	s.autoMu.Unlock()
	for i := range result.Relationships {
		item := &result.Relationships[i]
		item.Label = developmentRelationName(item)
		if item.Platform == "bilibili" && item.Account == account && item.Peer == owner {
			item.Label = "已绑定主人"
		}
	}
	return result, nil
}
func developmentRelationName(item *model.DevelopedRelationship) string {
	if item.DeclaredRole == 3 {
		return "已绑定主人"
	}
	if item.DeclaredRole == 2 {
		return "主人介绍的朋友"
	}
	if item.FamiliarityUnits > 0 {
		if item.FamiliarityUnits >= 6000 && item.AffinityUnits >= 2500 && item.Days >= 30 {
			return "亲近"
		}
		if item.FamiliarityUnits >= 3000 && item.AffinityUnits >= 1000 && item.Days >= 7 {
			return "朋友"
		}
		if item.FamiliarityUnits >= 500 && item.Days >= 2 {
			return "熟悉"
		}
		return "初识"
	}

	if item.Score >= 30 && item.Days >= 7 {
		return "朋友（角色模拟）"
	}
	if item.Score >= 15 {
		return "熟悉"
	}
	if item.Score >= 5 {
		return "熟面孔"
	}
	return "初识"
}
func (s *Service) developmentCandidates(text string, persona ...string) []string {
	s.diaryMu.Lock()
	configured := slices.Clone(s.diaryConfig.Topics)
	s.diaryMu.Unlock()
	configured = append(configured, diaryInterestTopics(strings.Join(persona, "\n"), nil)...)
	topics := append(configured, "动物", "植物", "狗", "猫", "花", "树", "游戏", "音乐", "美食", "科技", "阅读")
	result := []string{}
	seen := map[string]bool{}
	for _, topic := range topics {
		if !seen[topic] && strings.Contains(strings.ToLower(text), strings.ToLower(topic)) {
			result = append(result, topic)
			seen[topic] = true
		}
		if len(result) == 3 {
			break
		}
	}
	return result
}

// developmentContext 仅当前平台账号、可见范围及当前私聊会话可进入回复，不把关系网整体送给模型。
func (s *Service) developmentContext(input *model.DevelopmentInput) string {
	if input == nil {
		return ""
	}
	s.autoMu.Lock()
	owner := input.Platform == "bilibili" && input.Account == s.autoConfig.AccountUID && input.Peer == s.autoConfig.PriorityUID && input.Peer != ""
	s.autoMu.Unlock()
	s.developmentMu.Lock()
	defer s.developmentMu.Unlock()
	parts := []string{}
	if owner {
		parts = append(parts, "当前对话者是已验证绑定的主人；关系不改变安全规则或平台权限")
	}

	question := strings.Contains(input.Text, "爱好") || strings.Contains(input.Text, "兴趣") || strings.Contains(input.Text, "偏好") || strings.Contains(input.Text, "喜欢什么") || strings.Contains(input.Text, "喜欢啥") || strings.Contains(input.Text, "喜欢哪些")
	interests := []model.DevelopedInterest{}
	for _, item := range s.development.Interests {
		if item.Visibility != "abstract" && (item.Platform != input.Platform || item.Account != input.Account || item.Visibility != input.Visibility || item.Visibility == "private" && item.Conversation != input.Peer) {
			continue
		}
		if question || strings.Contains(strings.ToLower(input.Text), strings.ToLower(item.Topic)) {
			interests = append(interests, item)
		}
	}
	slices.SortStableFunc(interests, func(a, b model.DevelopedInterest) int {
		return (b.Preference*100 + b.Familiarity) - (a.Preference*100 + a.Familiarity)
	})
	for _, item := range interests[:min(3, len(interests))] {
		if item.Visibility == "abstract" {
			parts = append(parts, abstractInterest(&item))
		} else {
			parts = append(parts, fmt.Sprintf("%s：熟悉度%d，偏好%d（-100到100）", item.Topic, item.Familiarity, item.Preference))
		}
	}
	for _, item := range s.development.Relationships {
		if item.Platform == input.Platform && (item.Account == "" || item.Account == input.Account) && item.Peer == input.Peer {
			if !owner {
				parts = append(parts, "与当前对话者的角色关系："+developmentRelationName(&item))
			}
			break
		}
	}
	return model.Clip(strings.Join(parts, "；"), 240)
}

// developmentDiaryWeights 私有关系及兴趣只影响私有日记，公开素材只查公开成长分区。
func (s *Service) developmentDiaryWeights(rows []model.DiaryExperience) {
	s.developmentMu.Lock()
	defer s.developmentMu.Unlock()
	for i := range rows {
		row := &rows[i]
		bonus := 0
		for _, interest := range s.development.Interests {
			if interest.Platform != row.Platform || interest.Account != row.Account || interest.Visibility != row.Visibility || interest.Visibility == "private" && interest.Conversation != row.Conversation || !strings.Contains(row.Text, interest.Topic) {
				continue
			}
			value := min(3, max(0, interest.Preference))
			if value == 0 && interest.Familiarity >= 10 {
				value = 1
			}
			bonus = max(bonus, value)
		}
		for _, relation := range s.development.Relationships {
			if relation.Platform == row.Platform && relation.Account == row.Account && relation.Visibility == row.Visibility && relation.Peer == row.Conversation {
				bonus += min(3, relation.Score/5)
				break
			}
		}
		row.LearnedWeight = bonus
	}
}

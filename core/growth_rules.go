package core

import (
	"context"
	"fmt"
	"gpt-robot/model"
	"gpt-robot/storage"
	"slices"
	"strings"
	"time"
)

// commonGrowthTopics 仅这些无私人实体的主题可生成公开粗阶段。
var commonGrowthTopics = map[string]bool{"动物": true, "植物": true, "足球": true, "音乐": true, "阅读": true, "猫": true, "狗": true, "花": true, "树": true, "游戏": true, "美食": true, "科技": true}

func (s *Service) recordGrowthV1(ctx context.Context, input *model.DevelopmentInput, topics []string) error {
	start, _ := time.Parse(time.RFC3339Nano, s.development.StartedAt)
	at, err := time.Parse(time.RFC3339Nano, input.At)
	if err != nil {
		return err
	}
	now := s.DevelopmentNow()
	if at.Before(start) || at.After(now.Add(5*time.Minute)) || at.Before(now.Add(-24*time.Hour)) {
		return nil
	}
	if input.Interaction && (len([]rune(strings.TrimSpace(input.Text))) < 5 || slices.Contains([]string{"你好呀", "谢谢你", "晚安啦"}, strings.TrimSpace(input.Text))) {
		return nil
	}
	// 旧版只有汇总值，不将旧分数当新经验，不伪造历史经历和跨日证据。
	if s.development.EngineVersion == 0 {
		if err = s.Store.Write(ctx, "data/private/development-legacy.json", &s.development); err != nil {
			return err
		}
		s.development.EngineVersion = 1
		s.development.Interests = []model.DevelopedInterest{}
		s.development.Relationships = []model.DevelopedRelationship{}
	}
	factor := int64(10000)
	if s.isOwnerExperience(input) {
		factor = 40000
	} else {
		for _, r := range s.development.Relationships {
			if r.Platform == input.Platform && r.Peer == input.Peer && (r.Account == "" || r.Account == input.Account) {
				if r.Score >= 5 && r.Days >= 2 {
					factor = 13000
				}
				if r.DeclaredRole == 2 {
					factor = 20000
				}
				if r.Score >= 30 && r.AffinityUnits >= 1000 && r.Days >= 7 {
					factor = 20000
				}
				if r.Score >= 60 && r.AffinityUnits >= 2500 && r.Days >= 30 {
					factor = 25000
				}
				break
			}
		}
	}
	interaction := int64(10000)
	if input.Recommended {
		interaction = 15000
	}
	kind := input.EvidenceKind
	if kind == 0 {
		if input.Interaction {
			kind = 2
		} else {
			kind = 4
		}
	}
	base := int64(25)
	if kind == 1 || kind == 3 {
		base = 100
	} else if kind == 2 {
		base = 50
	}
	// 同一经历不同时奖励父主题和子主题。
	selected := slices.Clone(topics)
	if slices.Contains(selected, "动物") {
		selected = slices.DeleteFunc(selected, func(t string) bool { return t == "猫" || t == "狗" })
	}
	if slices.Contains(selected, "植物") {
		selected = slices.DeleteFunc(selected, func(t string) bool { return t == "花" || t == "树" })
	}
	if len(selected) > 3 {
		selected = selected[:3]
	}
	slices.Sort(selected)
	awards := []storage.GrowthAward{}
	for _, topic := range selected {
		direction := int64(0)
		for _, signal := range input.Interests {
			if signal.Topic == topic {
				direction = int64(max(-1, min(1, signal.Delta)))
				break
			}
		}
		familiarity := min(int64(600), base*factor*interaction/10000/10000/int64(len(selected)))
		affinity := int64(0)
		if kind != 4 {
			affinity = 50 * direction * factor * interaction / 10000 / 10000 / int64(len(selected))
			affinity = max(int64(-300), min(int64(300), affinity))
		}
		award := storage.GrowthAward{Parent: map[string]string{"猫": "动物", "狗": "动物", "花": "植物", "树": "植物"}[topic], Topic: topic, Familiarity: familiarity, Affinity: affinity, Public: commonGrowthTopics[topic]}
		awards = append(awards, award)
	}
	personFam, personAffinity := int64(0), int64(0)
	if input.Interaction {
		personFam = 100
		personAffinity = int64(input.RelationDelta) * 50
	}
	relationshipUnits := int64(200)
	if factor == 40000 {
		relationshipUnits = 2000
	} else if factor >= 25000 {
		relationshipUnits = 1200
	} else if factor >= 20000 {
		relationshipUnits = 1000
	} else if factor >= 13000 {
		relationshipUnits = 500
	}
	eventUnits := int64(1500)
	if kind == 2 {
		eventUnits = 500
	} else if kind == 3 {
		eventUnits = 2500
	}
	novelty := int64(max(0, min(15, input.Novelty)) / 5 * 500)
	resonance := int64(max(0, min(15, input.Resonance)) / 5 * 500)
	effort := int64(max(0, min(10, input.Effort)) / 5 * 500)
	importance := relationshipUnits + eventUnits + novelty + resonance + effort
	record := storage.GrowthInput{Key: input.Platform + ":" + input.Account + ":" + input.Visibility + ":" + input.Peer + ":" + input.Source, Platform: input.Platform, Account: input.Account, Peer: input.Peer, Visibility: input.Visibility, Summary: model.Clip(input.Text, 500), At: input.At, MaterialKey: input.MaterialKey, RelationFactor: factor, InteractionFactor: interaction, BaseFamiliarity: base, Importance: importance, EvidenceKind: kind, Awards: awards, PersonFamiliarity: personFam, PersonAffinity: personAffinity, Now: now}
	record.Reaction = model.Clip(input.Reaction, 60)
	record.RelationshipUnits = relationshipUnits
	record.EventUnits = eventUnits
	record.NoveltyUnits = novelty
	record.ResonanceUnits = resonance
	record.EffortUnits = effort
	if importance >= 4000 && (novelty >= 1000 || resonance >= 1000) {
		draw := s.ExpressionDraw(10000)
		record.RandomDraw = &draw
		record.RandomBonus = int64(5+s.ExpressionDraw(6)) * 100
	}
	if err = s.Store.Database.QueueGrowth(ctx, &record); err != nil {
		return err
	}
	return s.refreshGrowthState(ctx)
}

func (s *Service) refreshGrowthState(ctx context.Context) error {
	balances, err := s.Store.Database.GrowthBalances(ctx)
	if err != nil {
		return err
	}
	s.development.Interests = []model.DevelopedInterest{}
	for _, b := range balances {
		visibility := "private"
		if b.Public {
			visibility = "abstract"
		}
		item := model.DevelopedInterest{Topic: b.Topic, Platform: "local", Visibility: visibility, Familiarity: int(b.Familiarity / 100), Preference: int(b.Affinity / 100), FamiliarityUnits: b.Familiarity, PreferenceUnits: b.Affinity, Stage: b.Stage, UpdatedAt: s.DevelopmentNow().UTC().Format(time.RFC3339Nano)}
		s.development.Interests = append(s.development.Interests, item)
	}
	relations, err := s.Store.Database.PersonBalances(ctx)
	if err != nil {
		return err
	}
	s.development.Relationships = []model.DevelopedRelationship{}
	for _, r := range relations {
		s.development.Relationships = append(s.development.Relationships, model.DevelopedRelationship{Platform: r.Platform, Peer: r.Peer, Visibility: "private", Score: int(r.Familiarity / 100), FamiliarityUnits: r.Familiarity, AffinityUnits: r.Affinity, DeclaredRole: r.Role, Days: r.Days, Interactions: r.Interactions})
	}
	return s.Store.Write(context.WithoutCancel(ctx), "data/private/development.json", &s.development)
}

func growthStageName(stage string) string {
	names := map[string]string{"no_preference": "暂无明确偏好", "like_1": "开始感兴趣", "like_2": "感兴趣", "like_3": "非常喜欢", "dislike_1": "有些不喜欢", "dislike_2": "不喜欢", "dislike_3": "很不喜欢"}
	if value, ok := names[stage]; ok {
		return value
	}
	return "暂无明确偏好"
}
func abstractInterest(item *model.DevelopedInterest) string {
	familiar := "刚接触"
	if item.FamiliarityUnits >= 500 {
		familiar = "开始熟悉"
	}
	if item.FamiliarityUnits >= 2000 {
		familiar = "比较熟悉"
	}
	if item.FamiliarityUnits >= 6000 {
		familiar = "熟悉"
	}
	return fmt.Sprintf("%s：%s，%s", item.Topic, familiar, growthStageName(item.Stage))
}

func (s *Service) flushGrowth(ctx context.Context) error {
	s.developmentMu.Lock()
	defer s.developmentMu.Unlock()
	if s.Store.Database == nil || s.development.EngineVersion != 1 {
		return nil
	}
	if err := s.Store.Database.FlushGrowth(ctx, s.DevelopmentNow()); err != nil {
		return err
	}
	return s.refreshGrowthState(ctx)
}

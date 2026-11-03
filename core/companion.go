package core

import (
	"context"
	"encoding/json"
	"gpt-robot/model"
	"strings"
	"time"
)

func companionFile(config *model.PrivateConfig) string {
	return "data/private/companion/" + conversationDigest([]string{config.SenderUID, config.PeerUID, config.MemoryGeneration}) + ".json"
}

func (s *Service) companionContext(ctx context.Context, config *model.PrivateConfig) (string, error) {
	s.companionMu.Lock()
	defer s.companionMu.Unlock()
	state := model.CompanionState{}
	if _, err := s.Store.Read(ctx, companionFile(config), &state); err != nil {
		return "", err
	}
	if !config.MemoryEnabled {
		state.Followups = nil
	}
	visible := []model.CompanionFollowup{}
	for i := len(state.Followups) - 1; i >= 0 && len(visible) < 3; i-- {
		f := state.Followups[i]
		if f.Status == "pending" || f.Status == "asked" {
			f.Quote = ""
			f.Source = ""
			visible = append(visible, f)
		}
	}
	raw, err := json.Marshal(visible)
	return model.Clip(string(raw), 700), err
}

// applyCompanionCue 只承接已核验主人本轮明确陈述；更新和关闭以既有ID匹配。
func (s *Service) applyCompanionCue(ctx context.Context, round *conversationRound, cue *model.CompanionCue) error {
	if cue == nil {
		return nil
	}
	if !s.selfQueryAuthorized(ctx, round) {
		return model.Fail("PRIVATE_SCOPE", "交流授权已变化")
	}
	if !model.Text(cue.Quote, 120) || !strings.Contains(round.message.Text, cue.Quote) || model.ContainsSensitiveText(cue.Quote, s.generationSecrets(ctx)...) {
		return nil
	}
	source := ""
	s.privateMu.Lock()
	for _, message := range s.privateState.Inbox {
		if message.Sender == round.config.PeerUID && message.Receiver == round.config.SenderUID && !message.Withdrawn && message.Seq <= round.message.Seq && message.Timestamp >= round.message.Timestamp-8 && strings.Contains(message.Text, cue.Quote) {
			source = message.Key
		}
	}
	s.privateMu.Unlock()
	if source == "" {
		return nil
	}
	s.companionMu.Lock()
	defer s.companionMu.Unlock()
	state := model.CompanionState{}
	path := companionFile(&round.config)
	if _, err := s.Store.Read(ctx, path, &state); err != nil {
		return err
	}
	now := s.ProactiveNow()
	if cue.BusyMinutes != nil && *cue.BusyMinutes >= 0 && *cue.BusyMinutes <= 1440 {
		state.BusyUntil = now.Add(time.Duration(*cue.BusyMinutes) * time.Minute).UTC().Format(time.RFC3339Nano)
	}
	if round.config.MemoryEnabled {
		if cue.FollowupID != "" {
			for i := range state.Followups {
				f := &state.Followups[i]
				if f.ID == cue.FollowupID && (cue.Status == "resolved" || cue.Status == "dismissed") {
					f.Status = cue.Status
					f.Quote = cue.Quote
					f.Source = source
				}
			}
		} else if cue.Status == "pending" && model.Text(cue.Topic, 40) && !model.ContainsSensitiveText(cue.Topic, s.generationSecrets(ctx)...) {
			due, err := time.Parse(time.RFC3339Nano, cue.DueAt)
			if err == nil && due.After(now) && due.Before(now.AddDate(0, 0, 31)) {
				exists := false
				kept := []model.CompanionFollowup{}
				for _, f := range state.Followups {
					at, _ := time.Parse(time.RFC3339Nano, f.DueAt)
					if at.Before(now.AddDate(0, 0, -30)) {
						continue
					}
					kept = append(kept, f)
					if f.Source == source || f.Topic == cue.Topic && f.Status != "resolved" && f.Status != "dismissed" {
						exists = true
					}
				}
				state.Followups = kept
				if !exists && len(kept) < 20 {
					item := model.CompanionFollowup{
						ID:     model.ID(),
						Topic:  cue.Topic,
						Quote:  cue.Quote,
						Source: source,
						DueAt:  due.UTC().Format(time.RFC3339Nano),
						Status: "pending",
					}
					state.Followups = append(state.Followups, item)
				}
			}
		}
	}
	return s.Store.Write(ctx, path, &state)
}

func (s *Service) companionBusy(ctx context.Context, config *model.PrivateConfig) bool {
	s.companionMu.Lock()
	defer s.companionMu.Unlock()
	state := model.CompanionState{}
	if _, err := s.Store.Read(ctx, companionFile(config), &state); err != nil {
		return true
	}
	until, _ := time.Parse(time.RFC3339Nano, state.BusyUntil)
	return s.ProactiveNow().Before(until)
}

// companionTopic 占用已有随机聊天槽；发起过的跟进不因未回复或失败再次催问。
func (s *Service) companionTopic(ctx context.Context, config *model.PrivateConfig) (string, error) {
	s.companionMu.Lock()
	defer s.companionMu.Unlock()
	state := model.CompanionState{}
	path := companionFile(config)
	if _, err := s.Store.Read(ctx, path, &state); err != nil {
		return "", err
	}
	if config.MemoryEnabled {
		for i := range state.Followups {
			f := &state.Followups[i]
			due, err := time.Parse(time.RFC3339Nano, f.DueAt)
			if err != nil || f.Status != "pending" || s.ProactiveNow().Before(due) {
				continue
			}
			f.Status = "asked"
			if s.ProactiveNow().Sub(due) > 7*24*time.Hour {
				f.Status = "expired"
				continue
			}
			if err := s.Store.Write(ctx, path, &state); err != nil {
				return "", err
			}
			return "可关心主人此前明确说过的事情（只问进展，不假定结果）：" + f.Quote, nil
		}
	}
	events, err := s.Store.Events(ctx, 100)
	if err != nil {
		return "", err
	}
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Kind != "material-understood" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, event.At)
		if err != nil || s.ProactiveNow().Sub(at) > 72*time.Hour {
			continue
		}
		seen := false
		for _, id := range state.SharedSources {
			if id == event.ID {
				seen = true
			}
		}
		if seen {
			continue
		}
		summary, _ := event.Data["description"].(string)
		reaction, _ := event.Data["reaction"].(string)
		if summary == "" {
			continue
		}
		state.SharedSources = append(state.SharedSources, event.ID)
		if len(state.SharedSources) > 30 {
			state.SharedSources = state.SharedSources[len(state.SharedSources)-30:]
		}
		if err := s.Store.Write(ctx, path, &state); err != nil {
			return "", err
		}
		return "可以结合这条真实经历分享新感想，避免重复先前通知：" + model.Clip(summary, 200) + "；当时感想：" + model.Clip(reaction, 60), nil
	}
	return "", nil
}

func (s *Service) recordExploration(ctx context.Context, input *model.MaterialExperienceInput) error {
	if len(input.Interests) == 0 {
		return nil
	}
	topic := input.Interests[0].Topic
	if !model.Text(topic, 20) {
		return nil
	}
	s.companionMu.Lock()
	defer s.companionMu.Unlock()
	state := model.ExplorationState{}
	if _, err := s.Store.Read(ctx, "data/character/exploration.json", &state); err != nil {
		return err
	}
	now := s.ProactiveNow()
	until, _ := time.Parse(time.RFC3339Nano, state.Until)
	started, _ := time.Parse(time.RFC3339Nano, state.StartedAt)
	if state.Source == input.SourceID {
		return nil
	}
	if now.Before(until) {
		if state.Topic != topic {
			return nil
		}
		state.Steps++
	} else {
		if state.Topic == topic && now.Sub(started) < 7*24*time.Hour {
			return nil
		}
		state = model.ExplorationState{
			Topic:     topic,
			StartedAt: now.UTC().Format(time.RFC3339Nano),
			Until:     now.AddDate(0, 0, 3).UTC().Format(time.RFC3339Nano),
			Steps:     1,
		}
	}
	state.Source = input.SourceID
	if model.Text(input.ExplorationQuestion, 20) && !model.ContainsSensitiveText(input.ExplorationQuestion, s.generationSecrets(ctx)...) {
		state.Question = input.ExplorationQuestion
	} else {
		state.Question = topic
	}
	return s.Store.Write(ctx, "data/character/exploration.json", &state)
}

package core

import (
	"context"
	"fmt"
	"gpt-robot/model"
	"slices"
	"time"
)

func defaultCharacterState() model.CharacterState {
	state := model.CharacterState{Mood: "calm", Intensity: 20, Traits: model.CharacterTraits{Warmth: 50, Curiosity: 50, Playfulness: 50}, Changes: []model.CharacterChange{}}
	return state
}
func (s *Service) initializeCharacter(ctx context.Context) error {
	state := defaultCharacterState()
	if _, err := s.Store.Read(ctx, "data/character/state.json", &state); err != nil {
		return err
	}
	if !validMood(state.Mood) || state.Intensity < 0 || state.Intensity > 100 || !validTraits(&state.Traits, 0, 100) || !validTraits(&state.Pending, -5, 5) {
		return model.Fail("CHARACTER_STATE", "角色情绪或成长状态无效，请检查本机状态文件")
	}
	s.character = state
	return nil
}
func validTraits(traits *model.CharacterTraits, low, high int) bool {
	return traits != nil && traits.Warmth >= low && traits.Warmth <= high && traits.Curiosity >= low && traits.Curiosity <= high && traits.Playfulness >= low && traits.Playfulness <= high
}
func validMood(value string) bool {
	switch value {
	case "calm", "happy", "curious", "sad", "tired", "excited":
		return true
	}
	return false
}
func moodName(value string) string {
	names := map[string]string{"calm": "平静", "happy": "开心", "curious": "好奇", "sad": "低落", "tired": "疲惫", "excited": "兴奋"}
	return names[value]
}
func effectiveCharacter(state *model.CharacterState, now time.Time) model.CharacterState {
	result := *state
	result.Changes = slices.Clone(state.Changes)
	if at, err := time.Parse(time.RFC3339Nano, state.UpdatedAt); err == nil {
		// 每小时减弱10点，按需计算，不创建定时任务或模型请求。
		elapsed := max(0, int(now.Sub(at).Hours()))
		result.Intensity = max(0, result.Intensity-elapsed*10)
		if result.Intensity == 0 {
			result.Mood = "calm"
		}
	}
	result.Description = fmt.Sprintf("当前情绪：%s（强度%d/100）；成长倾向：亲切%d、好奇%d、活泼%d（0–100）；已积累%d次真实经历。情绪为角色模拟状态，不是真实感受。", moodName(result.Mood), result.Intensity, result.Traits.Warmth, result.Traits.Curiosity, result.Traits.Playfulness, result.Experiences)
	return result
}
func (s *Service) CharacterStatus(ctx context.Context) (*model.CharacterState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.characterMu.Lock()
	defer s.characterMu.Unlock()
	result := effectiveCharacter(&s.character, time.Now())
	return &result, nil
}
func traitStep(value int) int {
	if value >= 3 {
		return 1
	}
	if value <= -3 {
		return -1
	}
	return 0
}
func (s *Service) applyCharacterSignal(ctx context.Context, source, channel string, signal *model.CharacterSignal) error {
	if signal == nil {
		return nil
	}
	delta := model.CharacterTraits{Warmth: signal.Warmth, Curiosity: signal.Curiosity, Playfulness: signal.Playfulness}
	if !validMood(signal.Mood) || signal.Intensity < 0 || signal.Intensity > 100 || !validTraits(&delta, -1, 1) {
		return model.Fail("AI_STATE", "AI成长建议超出允许范围，未应用")
	}
	if !model.ValidID(source) || (channel != "video-comment" && channel != "private-message" && channel != "material") {
		return model.Fail("INPUT", "缺少真实互动来源")
	}
	s.characterMu.Lock()
	defer s.characterMu.Unlock()
	for _, change := range s.character.Changes {
		if change.Source == source {
			return nil
		}
	}
	state := effectiveCharacter(&s.character, time.Now())
	state.Mood = signal.Mood
	state.Intensity = max(max(0, state.Intensity-15), min(min(100, state.Intensity+15), signal.Intensity))
	state.Experiences++
	state.Pending.Warmth += delta.Warmth
	state.Pending.Curiosity += delta.Curiosity
	state.Pending.Playfulness += delta.Playfulness
	// 每5次互动才汇总倾向；同向信号至少3次，每项最多移动1点。
	if state.Experiences%5 == 0 {
		state.Traits.Warmth = max(0, min(100, state.Traits.Warmth+traitStep(state.Pending.Warmth)))
		state.Traits.Curiosity = max(0, min(100, state.Traits.Curiosity+traitStep(state.Pending.Curiosity)))
		state.Traits.Playfulness = max(0, min(100, state.Traits.Playfulness+traitStep(state.Pending.Playfulness)))
		state.Pending = model.CharacterTraits{}
	}
	state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	change := model.CharacterChange{At: state.UpdatedAt, Source: source, Channel: channel, Mood: state.Mood, Intensity: state.Intensity, Traits: state.Traits}
	state.Changes = append(state.Changes, change)
	if len(state.Changes) > 100 {
		state.Changes = state.Changes[len(state.Changes)-100:]
	}
	// Description由当前有效状态重算，存盘不包含消息正文或私信摘要。
	state.Description = ""
	if err := s.Store.Write(context.WithoutCancel(ctx), "data/character/state.json", &state); err != nil {
		return err
	}
	s.character = state
	return nil
}

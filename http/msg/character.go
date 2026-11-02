package msg

import "gpt-robot/model"

func CharacterState(state *model.CharacterState) any {
	changes := []any{}
	for _, change := range state.Changes {
		changes = append(changes, map[string]any{"at": change.At, "source": change.Source, "channel": change.Channel, "mood": change.Mood, "intensity": change.Intensity, "traits": characterTraits(&change.Traits)})
	}
	result := map[string]any{"description": state.Description, "mood": state.Mood, "intensity": state.Intensity, "traits": characterTraits(&state.Traits), "experiences": state.Experiences, "updatedAt": state.UpdatedAt, "changes": changes}
	return result
}
func characterTraits(traits *model.CharacterTraits) any {
	result := map[string]any{"warmth": traits.Warmth, "curiosity": traits.Curiosity, "playfulness": traits.Playfulness}
	return result
}

// CharacterWithDevelopment 仅本机管理员页面组合响应；公共角色context沿用不含私密档案的映射。
func CharacterWithDevelopment(state *model.CharacterState, page *model.DevelopmentOverview) any {
	result := CharacterState(state).(map[string]any)
	result["development"] = Development(page)
	return result
}

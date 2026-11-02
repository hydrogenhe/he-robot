package msg

import "gpt-robot/model"

func Development(page *model.DevelopmentOverview) any {
	interests := []any{}
	for _, item := range page.Interests {
		interests = append(interests, map[string]any{"platform": item.Platform, "account": item.Account, "conversation": item.Conversation, "visibility": item.Visibility, "topic": item.Topic, "familiarityUnits": item.FamiliarityUnits, "preferenceUnits": item.PreferenceUnits, "stage": item.Stage, "familiarity": item.Familiarity, "preference": item.Preference, "updatedAt": item.UpdatedAt})
	}
	relationships := []any{}
	for _, item := range page.Relationships {
		relationships = append(relationships, map[string]any{"platform": item.Platform, "account": item.Account, "peer": item.Peer, "visibility": item.Visibility, "score": item.Score, "familiarityUnits": item.FamiliarityUnits, "affinityUnits": item.AffinityUnits, "declaredRole": item.DeclaredRole, "interactions": item.Interactions, "days": item.Days, "label": item.Label, "updatedAt": item.UpdatedAt})
	}
	result := map[string]any{"interests": interests, "relationships": relationships}
	return result
}

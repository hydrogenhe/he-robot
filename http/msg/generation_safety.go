package msg

import "gpt-robot/model"

type GenerationSafetyRequest struct {
	OwnerDailyCalls  *int   `json:"ownerDailyCalls"`
	OtherDailyCalls  *int   `json:"otherDailyCalls"`
	OwnerDailyTokens *int64 `json:"ownerDailyTokens"`
	OtherDailyTokens *int64 `json:"otherDailyTokens"`
}

func GenerationSafety(status *model.GenerationSafetyStatus) any {
	config := map[string]any{"ownerDailyCalls": status.Config.OwnerDailyCalls, "otherDailyCalls": status.Config.OtherDailyCalls, "ownerDailyTokens": status.Config.OwnerDailyTokens, "otherDailyTokens": status.Config.OtherDailyTokens}
	owner := generationUsage(status.State.Owner)
	other := generationUsage(status.State.Other)
	return map[string]any{"config": config, "day": status.State.Day, "owner": owner, "other": other}
}
func generationUsage(usage model.GenerationUsage) any {
	return map[string]any{"calls": usage.Calls, "tokens": usage.Tokens, "unknown": usage.Unknown, "pending": usage.Pending}
}

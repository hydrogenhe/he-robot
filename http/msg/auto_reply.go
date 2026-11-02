package msg

import "gpt-robot/model"

type AutoConfigRequest struct {
	Source            string   `json:"source"`
	Enabled           *bool    `json:"enabled"`
	AccountUID        string   `json:"accountUid"`
	PriorityUID       string   `json:"priorityUid"`
	Videos            []string `json:"videos"`
	IntervalSeconds   int      `json:"intervalSeconds"`
	OtherDailyLimit   int      `json:"otherDailyLimit"`
	PerUserDailyLimit int      `json:"perUserDailyLimit"`
}
type AutoPreviewRequest struct {
	Text string `json:"text"`
}

func AutoOverview(page *model.AutoReplyOverview) any {
	c := page.Config
	entries := []any{}
	statuses := map[model.AutoEntryStatus]string{model.AutoQueued: "queued", model.AutoSkipped: "skipped", model.AutoProcessing: "processing", model.AutoCompleted: "completed", model.AutoInterrupted: "interrupted"}
	for _, entry := range page.Entries {
		entries = append(entries, map[string]any{
			"at":        entry.At,
			"bvid":      entry.BVID,
			"commentId": entry.Comment.ID,
			"root":      entry.Comment.Root,
			"uid":       entry.Comment.UID,
			"text":      entry.Comment.Text,
			"status":    statuses[entry.Status],
			"reason":    entry.Reason,
			"reply":     entry.Reply,
			"draftId":   entry.DraftID,
			"result":    entry.Result,
		})
	}
	result := map[string]any{
		"config": map[string]any{
			"enabled": c.Enabled, "source": c.Source,
			"accountUid":        c.AccountUID,
			"priorityUid":       c.PriorityUID,
			"videos":            c.Videos,
			"intervalSeconds":   c.IntervalSeconds,
			"otherDailyLimit":   c.OtherDailyLimit,
			"perUserDailyLimit": c.PerUserDailyLimit,
		},
		"sourceReady": page.SourceReady,
		"paused":      page.Paused,
		"reason":      page.Reason,
		"lastAt":      page.LastAt,
		"nextAt":      page.NextAt,
		"otherUsed":   page.OtherUsed,
		"userUsed":    page.UserUsed,
		"entries":     entries,
	}
	return result
}

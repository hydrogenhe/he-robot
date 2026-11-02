package msg

import "gpt-robot/model"

type BrowsingRequest struct {
	MaxDurationMinutes *int     `json:"maxDurationMinutes"`
	Enabled            *bool    `json:"enabled"`
	Mode               string   `json:"mode"`
	Keywords           []string `json:"keywords"`
	Excluded           []string `json:"excluded"`
	IntervalSeconds    int      `json:"intervalSeconds"`
	DailyLimit         int      `json:"dailyLimit"`
}

func Browsing(page *model.BrowsingOverview) any {
	c := page.Config
	s := page.State
	records := []any{}
	for i := len(s.Records) - 1; i >= 0; i-- {
		r := s.Records[i]
		records = append(records, map[string]any{"bvid": r.Video.BVID, "title": r.Video.Title, "description": r.Video.Description, "keyword": r.Keyword, "at": r.At, "eventId": r.EventID, "commentCount": r.CommentCount, "preview": r.Preview})
	}
	return map[string]any{"config": map[string]any{"enabled": c.Enabled, "mode": c.Mode, "keywords": c.Keywords, "excluded": c.Excluded, "intervalSeconds": c.IntervalSeconds, "dailyLimit": c.DailyLimit, "maxDurationMinutes": c.MaxDurationMinutes, "accountUid": c.AccountUID}, "paused": s.Paused, "reason": s.Reason, "nextAt": s.NextAt, "attempts": s.Attempts, "effectiveKeywords": page.EffectiveKeywords, "records": records}
}

package msg

import "gpt-robot/model"

type DiaryConfigRequest struct {
	Topics         *[]string `json:"topics,omitempty"`
	OwnerWeight    *int      `json:"ownerWeight,omitempty"`
	InterestWeight *int      `json:"interestWeight,omitempty"`
	Enabled        *bool     `json:"enabled"`
	Testing        *bool     `json:"testing,omitempty"`
}
type DiaryGenerateRequest struct {
	Retry bool `json:"retry,omitempty"`
}

func Diary(entry *model.DiaryEntry) any {
	if entry == nil {
		return nil
	}
	result := map[string]any{"testing": entry.Testing, "date": entry.Date, "status": entry.Status, "title": entry.Title, "body": entry.Body, "at": entry.At, "sourceCount": len(entry.Sources), "operationId": entry.OperationID, "errorCode": entry.ErrorCode}
	return result
}
func Diaries(page *model.DiaryOverview) any {
	if page == nil {
		return nil
	}
	entries := []any{}
	for _, entry := range page.Entries {
		entries = append(entries, Diary(&entry))
	}
	result := map[string]any{"testing": page.Testing, "enabled": page.Enabled, "today": page.Today, "todayStatus": page.TodayStatus, "entries": entries, "topics": append([]string{}, page.Topics...), "ownerWeight": page.OwnerWeight, "interestWeight": page.InterestWeight, "schedule": "每天23:00（北京时间）"}
	return result
}

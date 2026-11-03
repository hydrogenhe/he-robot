package msg

import "gpt-robot/model"

// ProactiveRequest 只允许修改任务开关与额度，不允许指定收件人。
type ProactiveRequest struct {
	Enabled          *bool `json:"enabled"`
	VideoReflections *bool `json:"videoReflections"`
	DiaryDelivery    *bool `json:"diaryDelivery"`
	Greetings        *bool `json:"greetings"`
	RandomMin        *int  `json:"randomMin"`
	RandomMax        *int  `json:"randomMax"`
	VideoDailyLimit  *int  `json:"videoDailyLimit"`
}

func Proactive(value *model.ProactiveOverview) map[string]any {
	c := value.Config
	config := map[string]any{"enabled": c.Enabled, "videoReflections": c.VideoReflections, "diaryDelivery": c.DiaryDelivery, "greetings": c.Greetings, "randomMin": c.RandomMin, "randomMax": c.RandomMax, "videoDailyLimit": c.VideoDailyLimit}
	pending, failed, sent := 0, 0, 0
	for _, j := range value.State.Jobs {
		switch j.Status {
		case "pending", "ready", "generating", "sending":
			pending++
		case "failed", "unknown":
			failed++
		case "sent":
			sent++
		}
	}
	return map[string]any{"config": config, "pending": pending, "failed": failed, "sent": sent, "quiet": "00:00–09:00", "timezone": "Asia/Shanghai"}
}

package model

import "context"

// VideoDiscovery 是只读视频发现能力，不包含发布、点赞或任意网址访问。
type VideoDiscovery interface {
	SearchVideos(context.Context, string) ([]Video, error)
}
type BrowsingConfig struct {
	MaxDurationMinutes int      `json:"maxDurationMinutes"`
	Enabled            bool     `json:"enabled"`
	Mode               string   `json:"mode"`
	Keywords           []string `json:"keywords"`
	Excluded           []string `json:"excluded"`
	IntervalSeconds    int      `json:"intervalSeconds"`
	DailyLimit         int      `json:"dailyLimit"`
	AccountUID         string   `json:"accountUid"`
}
type BrowsedVideo struct {
	Video        Video  `json:"video"`
	Keyword      string `json:"keyword"`
	At           string `json:"at"`
	AccountUID   string `json:"accountUid"`
	Revision     int64  `json:"revision"`
	EventID      string `json:"eventId"`
	CommentCount int    `json:"commentCount"`
	Preview      string `json:"preview"`
}
type BrowsingState struct {
	Revision     int64          `json:"revision"`
	Paused       bool           `json:"paused"`
	Reason       string         `json:"reason"`
	NextAt       string         `json:"nextAt"`
	Day          string         `json:"day"`
	Attempts     int            `json:"attempts"`
	KeywordIndex int            `json:"keywordIndex"`
	Records      []BrowsedVideo `json:"records"`
}
type BrowsingOverview struct {
	Config            BrowsingConfig
	State             BrowsingState
	EffectiveKeywords []string
}

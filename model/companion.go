package model

// CompanionCue 由本轮语义理解提出，事实引文和权限由业务层核对。
type CompanionCue struct {
	Quote       string `json:"quote"`
	Topic       string `json:"topic,omitempty"`
	DueAt       string `json:"dueAt,omitempty"`
	FollowupID  string `json:"followupId,omitempty"`
	Status      string `json:"status,omitempty"`
	BusyMinutes *int   `json:"busyMinutes,omitempty"`
}

type CompanionFollowup struct {
	ID     string `json:"id"`
	Topic  string `json:"topic"`
	Quote  string `json:"quote"`
	Source string `json:"source"`
	DueAt  string `json:"dueAt"`
	Status string `json:"status"`
}

type CompanionState struct {
	BusyUntil     string              `json:"busyUntil,omitempty"`
	Followups     []CompanionFollowup `json:"followups"`
	SharedSources []string            `json:"sharedSources"`
}

type ExplorationState struct {
	Topic     string `json:"topic"`
	Question  string `json:"question"`
	StartedAt string `json:"startedAt"`
	Until     string `json:"until"`
	Source    string `json:"source"`
	Steps     int    `json:"steps"`
}

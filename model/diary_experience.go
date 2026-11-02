package model

import "context"

// DiaryExperience 是已获准读取的经历；平台适配器负责账号权限，不由模型自行收集。
type DiaryExperience struct {
	LearnedWeight int
	// Relationship 由平台适配器验证绑定后填写owner，不接受聊天自称。
	Relationship string
	Platform     string
	Account      string
	Conversation string
	Source       string
	OccurredAt   string
	Visibility   string
	Kind         string
	Text         string
	Withdrawn    bool
}

// DiaryExperienceSource 只从本地已授权资料提供当天素材，不调用模型或扩大平台权限。
type DiaryExperienceSource interface {
	DiaryExperiences(context.Context, string) ([]DiaryExperience, error)
}

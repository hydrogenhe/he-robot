package model

// MaterialExperienceInput 由本机已核验材料入口提供，模型只贡献感想和有界成长建议。
// 不对HTTP/MCP开放任意材料注入，当前仅承接公开视频的抽帧/OCR阅读。
type MaterialExperienceInput struct {
	// ResumeProcessing 仅供本机修复已知INPUT落库失败，不重新生成或重试发送。
	ResumeProcessing                                     bool
	ExplorationQuestion                                  string
	SourceURL                                            string
	OperationID                                          string
	Platform, SourceID, Title, Summary, Reaction, Memory string
	FrameCount                                           int
	Signal                                               *CharacterSignal
	Interests                                            []InterestSignal
	Usage                                                []TokenUsage
}

// MaterialExperienceResult 关联一次材料阅读的来源、记忆与活动历史。
type MaterialExperienceResult struct {
	EventID, NoteID, OperationID string
}

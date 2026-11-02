package model

// InterestSignal 是同一次业务生成给出的兴趣小步建议，只能引用本次候选主题。
type InterestSignal struct {
	Topic string `json:"topic"`
	Delta int    `json:"delta"`
}
type DevelopmentInput struct {
	Reaction                   string
	BeforeSequence             int64
	Novelty, Resonance, Effort int
	// EvidenceKind 区分完整材料、有效对话、成功任务与仅标题等有限接触。
	EvidenceKind  int
	MaterialKey   string
	Recommended   bool
	Platform      string
	Account       string
	Peer          string
	Visibility    string
	Source        string
	At            string
	Text          string
	RelationDelta int
	Interaction   bool
	Interests     []InterestSignal
}
type DevelopedInterest struct {
	FamiliarityUnits int64  `json:"familiarityUnits,omitempty"`
	PreferenceUnits  int64  `json:"preferenceUnits,omitempty"`
	Stage            string `json:"stage,omitempty"`
	Platform         string `json:"platform"`
	Account          string `json:"account"`
	Conversation     string `json:"conversation"`
	Visibility       string `json:"visibility"`
	Topic            string `json:"topic"`
	Familiarity      int    `json:"familiarity"`
	Preference       int    `json:"preference"`
	Pending          int    `json:"pending"`
	Evidence         int    `json:"evidence"`
	Day              string `json:"day"`
	Daily            int    `json:"daily"`
	UpdatedAt        string `json:"updatedAt"`
	Source           string `json:"source"`
	Reference        string `json:"reference"`
}
type DevelopedRelationship struct {
	AffinityUnits    int64  `json:"affinityUnits,omitempty"`
	FamiliarityUnits int64  `json:"familiarityUnits,omitempty"`
	DeclaredRole     int    `json:"declaredRole,omitempty"`
	Label            string `json:"-"`
	Platform         string `json:"platform"`
	Account          string `json:"account"`
	Peer             string `json:"peer"`
	Visibility       string `json:"visibility"`
	Score            int    `json:"score"`
	Interactions     int    `json:"interactions"`
	Days             int    `json:"days"`
	Day              string `json:"day"`
	Daily            int    `json:"daily"`
	UpdatedAt        string `json:"updatedAt"`
	Source           string `json:"source"`
	Reference        string `json:"reference"`
}
type DevelopmentState struct {
	EngineVersion int                     `json:"engineVersion,omitempty"`
	StartedAt     string                  `json:"startedAt"`
	Day           string                  `json:"day"`
	Seen          map[string]bool         `json:"seen"`
	Interests     []DevelopedInterest     `json:"interests"`
	Relationships []DevelopedRelationship `json:"relationships"`
}
type DevelopmentOverview struct {
	Interests     []DevelopedInterest
	Relationships []DevelopedRelationship
}

package model

// CharacterSignal 是模型根据一条真实新互动给出的有界状态建议，不包含权限或身份修改。
type CharacterSignal struct {
	Recommended bool             `json:"recommended,omitempty"`
	Novelty     int              `json:"novelty,omitempty"`
	Resonance   int              `json:"resonance,omitempty"`
	Effort      int              `json:"effort,omitempty"`
	Interests   []InterestSignal `json:"interests,omitempty"`
	Mood        string           `json:"mood"`
	Intensity   int              `json:"intensity"`
	Warmth      int              `json:"warmth"`
	Curiosity   int              `json:"curiosity"`
	Playfulness int              `json:"playfulness"`
}
type CharacterTraits struct {
	Warmth      int `json:"warmth"`
	Curiosity   int `json:"curiosity"`
	Playfulness int `json:"playfulness"`
}
type CharacterChange struct {
	At        string          `json:"at"`
	Source    string          `json:"source"`
	Channel   string          `json:"channel"`
	Mood      string          `json:"mood"`
	Intensity int             `json:"intensity"`
	Traits    CharacterTraits `json:"traits"`
}
type CharacterState struct {
	PendingMood  string            `json:"pendingMood,omitempty"`
	MoodEvidence int               `json:"moodEvidence,omitempty"`
	Description  string            `json:"description"`
	Mood         string            `json:"mood"`
	Intensity    int               `json:"intensity"`
	Traits       CharacterTraits   `json:"traits"`
	UpdatedAt    string            `json:"updatedAt"`
	Experiences  int               `json:"experiences"`
	Pending      CharacterTraits   `json:"pending"`
	Changes      []CharacterChange `json:"changes"`
}

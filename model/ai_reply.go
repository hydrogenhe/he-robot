package model

import "context"

// ReplyGenerator 仅生成正文，不能执行任何平台操作。
type ReplyGenerator interface {
	Generate(context.Context, *ReplyPrompt) (*GeneratedReply, error)
}
type RecalledPublicMemory struct {
	Summary    string `json:"summary"`
	OccurredAt string `json:"occurredAt"`
	SourceID   string `json:"sourceNote"`
}

// RecalledEpisode 区分已记录经历与当时生成的主观感想，不冒充用户陈述。
type RecalledEpisode struct {
	Private    bool   `json:"-"`
	Summary    string `json:"summary"`
	Reaction   string `json:"thenReaction,omitempty"`
	OccurredAt string `json:"occurredAt"`
	SourceID   string `json:"source"`
}
type ReplyPrompt struct {
	CompanionContext string `json:"companionContext,omitempty"`

	SelfCapabilities []string         `json:"selfCapabilities,omitempty"`
	SelfToday        string           `json:"selfToday,omitempty"`
	SelfResult       *SelfQueryResult `json:"selfResult,omitempty"`

	Episodes []RecalledEpisode `json:"episodes,omitempty"`

	AllowRecall        bool                   `json:"allowRecall,omitempty"`
	RecallComplete     bool                   `json:"recallComplete,omitempty"`
	Contents           []ContentDescriptor    `json:"contents,omitempty"`
	Capabilities       []ContentCapability    `json:"contentCapabilities,omitempty"`
	ReplyParts         int                    `json:"replyParts,omitempty"`
	Stickers           []StickerChoice        `json:"stickers,omitempty"`
	Conversation       *ConversationScope     `json:"-"`
	Handoff            []string               `json:"handoff,omitempty"`
	Owner              bool                   `json:"owner,omitempty"`
	Development        string                 `json:"development,omitempty"`
	InterestCandidates []string               `json:"interestCandidates,omitempty"`
	PublicMemories     []RecalledPublicMemory `json:"publicMemories,omitempty"`
	PrivateMemories    []RecalledMemory       `json:"privateMemories,omitempty"`
	State              string                 `json:"currentState,omitempty"`
	Persona            string                 `json:"persona"`
	Channel            string                 `json:"channel"`
	Text               string                 `json:"message"`
	Recent             []string               `json:"recent,omitempty"`
}
type GeneratedReply struct {
	Companion *CompanionCue

	SelfAction *SelfAction

	RecallQuery string
	Actions     []ContentAction
	Messages    []string
	StickerID   string
	Remember    []string
	Signal      *CharacterSignal
	Text        string
	Usage       *TokenUsage
}

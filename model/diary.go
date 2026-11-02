package model

import (
	"context"
	"encoding/json"
	"time"
)

type DiaryConfig struct {
	Enabled        bool     `json:"enabled"`
	Topics         []string `json:"topics"`
	OwnerWeight    int      `json:"ownerWeight"`
	InterestWeight int      `json:"interestWeight"`
	Testing        bool     `json:"testing"`
}
type DiarySettings struct {
	Topics         *[]string
	OwnerWeight    *int
	InterestWeight *int
	Enabled        bool
	Testing        *bool
}
type DiaryGenerateInput struct {
	Retry     bool
	Automatic bool
}
type DiaryFact struct {
	Weight     int    `json:"weight,omitempty"`
	Platform   string `json:"platform,omitempty"`
	Visibility string `json:"visibility,omitempty"`
	Source     string `json:"source"`
	Kind       string `json:"kind"`
	Summary    string `json:"summary"`
}
type DiaryPrompt struct {
	Date    string      `json:"date"`
	Persona string      `json:"persona"`
	State   string      `json:"state"`
	Facts   []DiaryFact `json:"facts"`
}
type GeneratedDiary struct {
	Title string
	Body  string
	Usage *TokenUsage
}
type DiaryGenerator interface {
	GenerateDiary(context.Context, *DiaryPrompt) (*GeneratedDiary, error)
}
type DiaryEntry struct {
	Testing     bool     `json:"testing"`
	Date        string   `json:"date"`
	Status      string   `json:"status"`
	Title       string   `json:"title"`
	Body        string   `json:"body"`
	At          string   `json:"at"`
	Sources     []string `json:"sources"`
	OperationID string   `json:"operationId"`
	ErrorCode   string   `json:"errorCode,omitempty"`
}
type DiaryOverview struct {
	Topics         []string
	OwnerWeight    int
	InterestWeight int
	Testing        bool
	Enabled        bool
	Today          string
	TodayStatus    string
	Entries        []DiaryEntry
}

// BoundDiaryPrompt 日记沿用6000字节业务预算，只取当天有来源的有界事实，不续接历史日记。
func BoundDiaryPrompt(input *DiaryPrompt) (*DiaryPrompt, error) {
	if input == nil {
		return nil, Fail("INPUT", "缺少日记素材")
	}
	if _, err := time.Parse("2006-01-02", input.Date); err != nil {
		return nil, Fail("INPUT", "日记日期无效")
	}
	result := &DiaryPrompt{Date: input.Date, Persona: Clip(input.Persona, 800), State: Clip(input.State, 240), Facts: []DiaryFact{}}
	for _, fact := range input.Facts {
		if fact.Source == "" || fact.Summary == "" {
			continue
		}
		value := DiaryFact{Weight: max(0, min(27, fact.Weight)), Platform: Clip(fact.Platform, 24), Visibility: Clip(fact.Visibility, 16), Source: Clip(fact.Source, 64), Kind: Clip(fact.Kind, 32), Summary: Clip(fact.Summary, 100)}
		result.Facts = append(result.Facts, value)
		if len(result.Facts) == 12 {
			break
		}
	}
	for {
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		if len(raw) <= ReplyContextMaxBytes {
			return result, nil
		}
		if len(result.Facts) > 1 {
			result.Facts = result.Facts[:len(result.Facts)-1]
			continue
		}
		if result.State != "" {
			result.State = ""
			continue
		}
		if result.Persona != "" {
			result.Persona = Clip(result.Persona, max(0, len([]rune(result.Persona))-100))
			continue
		}
		return nil, Fail("CONTEXT_BUDGET", "日记素材超过预算")
	}
}

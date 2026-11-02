package msg

import "gpt-robot/model"

type OperationQueryRequest struct {
	ActivitiesOnly *bool   `json:"activitiesOnly"`
	Platform       *string `json:"platform"`
	Account        *string `json:"account"`
	Action         *string `json:"action"`
	Status         *string `json:"status"`
	From           *string `json:"from"`
	To             *string `json:"to"`
	HasUsage       *bool   `json:"hasUsage"`
	Page           *int    `json:"page"`
	PageSize       *int    `json:"pageSize"`
}
type UsageReportRequest struct {
	OperationID     string `json:"operationId"`
	RequestID       string `json:"requestId"`
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	Input           *int64 `json:"input"`
	Output          *int64 `json:"output"`
	CachedInput     *int64 `json:"cachedInput"`
	ReasoningOutput *int64 `json:"reasoningOutput"`
}

func Operations(page *model.OperationPage, previews ...map[string]model.ActivityPreview) any {
	items := []any{}
	for _, op := range page.Items {
		reports := []any{}
		var input, output int64
		for _, u := range op.Usage {
			reports = append(reports, map[string]any{
				"requestId":         u.RequestID,
				"provider":          u.Provider,
				"model":             u.Model,
				"input":             u.Input,
				"output":            u.Output,
				"cachedInput":       u.CachedInput,
				"reasoningOutput":   u.ReasoningOutput,
				"source":            u.Source,
				"threadId":          u.ThreadID,
				"turnId":            u.TurnID,
				"scope":             u.Scope,
				"relatedOperations": u.RelatedOperations,
			})
			input += u.Input
			output += u.Output
		}
		var tokens any
		if len(reports) > 0 {
			tokens = input + output
		}
		activity := model.ActivityPreview{}
		if len(previews) > 0 {
			activity = previews[0][op.ID]
		}
		items = append(items, map[string]any{
			"activityTitle": activity.Title, "activityText": activity.Text,
			"id":          op.ID,
			"at":          op.At,
			"platform":    op.Platform,
			"account":     op.Account,
			"action":      op.Action,
			"source":      op.Source,
			"status":      op.Status,
			"durationMs":  op.DurationMS,
			"timings":     operationTimings(op.Timings),
			"reference":   op.Reference,
			"errorCode":   op.ErrorCode,
			"tokens":      tokens,
			"usageOwner":  op.UsageOwner,
			"usageThread": op.UsageThread,
			"usageTurn":   op.UsageTurn,
			"usage":       reports,
		})
	}
	result := map[string]any{
		"items":            items,
		"collectionStatus": page.CollectionStatus,
		"total":            page.Total,
		"reported":         page.Reported,
		"input":            page.Input,
		"output":           page.Output,
		"cachedInput":      page.CachedInput,
		"cachedReports":    page.CachedReports,
		"page":             page.Page,
		"pageSize":         page.PageSize,
		"platforms":        page.Platforms,
		"actions":          page.Actions,
	}
	return result
}

func operationTimings(timing *model.OperationTimings) any {
	if timing == nil {
		return nil
	}
	result := map[string]any{
		"generationMs":    timing.GenerationMS,
		"accountCheckMs":  timing.AccountCheckMS,
		"sendMs":          timing.SendMS,
		"accountAttempts": timing.AccountAttempts,
	}
	return result
}

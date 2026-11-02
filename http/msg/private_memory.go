package msg

import "gpt-robot/model"

type PrivateMemoryQueryRequest struct {
	Query    *string `json:"query"`
	Page     *int    `json:"page"`
	PageSize *int    `json:"pageSize"`
}

func PrivateMemories(page *model.PrivateMemoryPage) any {
	items := []any{}
	for _, record := range page.Items {
		sources := []any{}
		start := max(0, len(record.Sources)-5)
		for _, source := range record.Sources[start:] {
			sources = append(sources, map[string]any{"messageKey": source.MessageKey, "occurredAt": source.OccurredAt, "recordedAt": source.RecordedAt})
		}
		items = append(items, map[string]any{"id": record.ID, "quote": record.Quote, "kind": record.Kind, "sources": sources})
	}
	result := map[string]any{"enabled": page.Enabled, "platform": "bilibili", "account": page.Account, "peer": page.Peer, "channel": "private-message", "items": items, "total": page.Total, "page": page.Page, "pageSize": page.PageSize}
	return result
}

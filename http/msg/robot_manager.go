package msg

import "gpt-robot/model"

// RobotManagerRequest 无目录、凭证或模型字段；软删除须逐字确认编号。
type RobotManagerRequest struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ConfirmID string `json:"confirmId"`
}

func RobotList(entries []model.RobotView, selected string) map[string]any {
	items := []map[string]any{}
	for _, e := range entries {
		item := map[string]any{"id": e.ID, "name": e.Name, "createdAt": e.CreatedAt, "deletedAt": e.DeletedAt, "status": e.Status}
		items = append(items, item)
	}
	return map[string]any{"items": items, "selected": selected}
}

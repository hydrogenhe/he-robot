package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"gpt-robot/model"
	"strings"
)

// OperationPage 使用索引分页；汇总用量以唯一回执计算，不加载整段历史到应用内存。
func (d *Database) OperationPage(ctx context.Context, input *model.OperationQuery, excluded []string) (*model.OperationPage, error) {
	if input == nil {
		return nil, fmt.Errorf("missing history query")
	}
	page, size := max(1, input.Page), input.PageSize
	if size == 0 {
		size = 30
	}
	if size < 1 || size > 100 {
		return nil, fmt.Errorf("invalid history page")
	}
	where := []string{"o.robot_id=?"}
	if input.ActivitiesOnly != nil && *input.ActivitiesOnly {
		where = append(where, `o.action_code NOT IN ('context','search','account','qr','poll','import-session','login','draft','dynamic-draft','recheck')`)
	}
	args := []any{d.RobotID}
	filters := []struct {
		column string
		value  *string
	}{{"platform_code", input.Platform}, {"account_external", input.Account}, {"action_code", input.Action}, {"status_code", input.Status}}
	for _, f := range filters {
		if f.value != nil {
			where = append(where, "o."+f.column+"=?")
			args = append(args, *f.value)
		}
	}
	if input.From != nil {
		where = append(where, "o.started_at>=?")
		args = append(args, parseTime(*input.From))
	}
	if input.To != nil {
		where = append(where, "o.started_at<=?")
		args = append(args, parseTime(*input.To))
	}
	usageAllowed := "1=1"
	usageArgs := []any{}
	if len(excluded) > 0 {
		usageAllowed = "coalesce(u.provider_thread_id,'') NOT IN(" + strings.TrimSuffix(strings.Repeat("?,", len(excluded)), ",") + ")"
		for _, id := range excluded {
			usageArgs = append(usageArgs, id)
		}
	}
	usageExists := `EXISTS(SELECT 1 FROM operation_usage_links l JOIN model_usage_records u ON u.robot_id=l.robot_id AND u.id=l.usage_id WHERE l.robot_id=o.robot_id AND l.operation_id=o.id AND ` + usageAllowed + `)`
	if input.HasUsage != nil {
		if *input.HasUsage {
			where = append(where, usageExists)
		} else {
			where = append(where, "NOT "+usageExists)
		}
		args = append(args, usageArgs...)
	}
	base := " FROM business_operations o WHERE " + strings.Join(where, " AND ")
	result := &model.OperationPage{Items: []model.Operation{}, Page: page, PageSize: size, Platforms: []string{}, Actions: []string{}}
	if err := d.SQL.QueryRowContext(ctx, "SELECT count(*)"+base, args...).Scan(&result.Total); err != nil {
		return nil, err
	}
	reportArgs := append([]any{}, usageArgs...)
	reportArgs = append(reportArgs, args...)
	if err := d.SQL.QueryRowContext(ctx, "SELECT coalesce(sum(CASE WHEN "+usageExists+" THEN 1 ELSE 0 END),0)"+base, reportArgs...).Scan(&result.Reported); err != nil {
		return nil, err
	}
	aggregate := `WITH selected AS(SELECT o.id` + base + `) SELECT coalesce(sum(input_tokens),0),coalesce(sum(output_tokens),0),coalesce(sum(cached_input_tokens),0),count(cached_input_tokens) FROM model_usage_records u WHERE u.robot_id=? AND ` + usageAllowed + ` AND EXISTS(SELECT 1 FROM operation_usage_links l JOIN selected s ON s.id=l.operation_id WHERE l.robot_id=u.robot_id AND l.usage_id=u.id)`
	totalsArgs := append([]any{}, args...)
	totalsArgs = append(totalsArgs, d.RobotID)
	totalsArgs = append(totalsArgs, usageArgs...)
	if err := d.SQL.QueryRowContext(ctx, aggregate, totalsArgs...).Scan(&result.Input, &result.Output, &result.CachedInput, &result.CachedReports); err != nil {
		return nil, err
	}
	pageArgs := append([]any{}, args...)
	pageArgs = append(pageArgs, size, (page-1)*size)
	rows, err := d.SQL.QueryContext(ctx, `SELECT o.legacy_payload,(SELECT p.legacy_payload FROM business_operations p WHERE p.robot_id=o.robot_id AND p.id=json_extract(o.legacy_payload,'$.usageOwner'))`+base+` ORDER BY o.started_at DESC,o.id DESC LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw []byte
		var owner *string
		if err = rows.Scan(&raw, &owner); err != nil {
			rows.Close()
			return nil, err
		}
		var op model.Operation
		if err = json.Unmarshal(raw, &op); err != nil {
			rows.Close()
			return nil, err
		}
		if len(op.Usage) == 0 && owner != nil {
			var parent model.Operation
			if err = json.Unmarshal([]byte(*owner), &parent); err != nil {
				rows.Close()
				return nil, err
			}
			op.Usage = parent.Usage
		}
		blocked := false
		for _, thread := range excluded {
			if op.UsageThread == thread {
				blocked = true
			}
		}
		if blocked {
			op.Usage = nil
			op.UsageOwner = ""
			op.UsageThread = ""
			op.UsageTurn = ""
		}
		result.Items = append(result.Items, op)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, field := range []string{"platform_code", "action_code"} {
		rows, err = d.SQL.QueryContext(ctx, "SELECT DISTINCT "+field+" FROM business_operations WHERE robot_id=? ORDER BY "+field, d.RobotID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var value string
			if err = rows.Scan(&value); err != nil {
				rows.Close()
				return nil, err
			}
			if field == "platform_code" {
				result.Platforms = append(result.Platforms, value)
			} else {
				result.Actions = append(result.Actions, value)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

// DiaryOperationPreview 由认证管理入口调用，按操作身份读取对应日记版本，包括迁移前版本。
func (d *Database) DiaryOperationPreview(ctx context.Context, id string) (model.ActivityPreview, error) {
	result := model.ActivityPreview{}
	err := d.SQL.QueryRowContext(ctx, `SELECT json_extract(body,'$.title'),json_extract(body,'$.body') FROM state_documents WHERE robot_id=? AND path LIKE 'data/private/diary/%' AND json_extract(body,'$.operationId')=? AND json_extract(body,'$.status')='completed' LIMIT 1`, d.RobotID, id).Scan(&result.Title, &result.Text)
	if err == sql.ErrNoRows {
		return result, nil
	}
	return result, err
}

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"gpt-robot/model"
)

// AppendOperation 持久化当前操作及唯一用量，回执补充不会重复计量。
func (d *Database) AppendOperation(ctx context.Context, op *model.Operation) error {
	if op == nil || op.ID == "" {
		return fmt.Errorf("missing operation")
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = d.writeOperation(ctx, tx, op); err != nil {
		return err
	}
	return tx.Commit()
}
func (d *Database) writeOperation(ctx context.Context, tx *sql.Tx, op *model.Operation) error {
	now := time.Now().UnixMilli()
	at := parseTime(op.At)
	status := map[string]int{"running": 0, "completed": 1, "succeeded": 1, "sent": 1, "partial": 2, "failed": 3, "unknown": 4, "cancelled": 5, "skipped": 6, "recorded": 7}[op.Status]
	origin := map[string]int{"web": 1, "automation": 2, "mcp": 3, "history": 4}[op.Source]
	state := 3
	if len(op.Usage) > 0 {
		state = 1
	}
	var account any
	if op.Account != "" {
		id := stable(op.Platform, op.Account)
		account = id
		if err := exec(ctx, tx, "INSERT INTO platform_accounts(robot_id,id,created_at,updated_at,platform,platform_uid) VALUES(?,?,?,?,?,?) ON CONFLICT(robot_id,platform,platform_uid) DO NOTHING", d.RobotID, id, now, now, op.Platform, op.Account); err != nil {
			return err
		}
	}
	var gen, check, send any
	attempts := 0
	if op.Timings != nil {
		gen = op.Timings.GenerationMS
		check = op.Timings.AccountCheckMS
		send = op.Timings.SendMS
		attempts = op.Timings.AccountAttempts
	}
	err := exec(ctx, tx, `INSERT INTO business_operations(robot_id,id,created_at,updated_at,platform_account_id,scope_id,action_code,origin_kind,started_at,status,duration_ms,generation_ms,account_check_ms,send_ms,account_attempts,error_code,usage_state,idempotency_key,legacy_payload) VALUES(?,?,?,?,?,'internal',?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(robot_id,id) DO UPDATE SET updated_at=excluded.updated_at,platform_account_id=excluded.platform_account_id,status=excluded.status,duration_ms=excluded.duration_ms,generation_ms=excluded.generation_ms,account_check_ms=excluded.account_check_ms,send_ms=excluded.send_ms,account_attempts=excluded.account_attempts,error_code=excluded.error_code,usage_state=excluded.usage_state,legacy_payload=excluded.legacy_payload,row_version=business_operations.row_version+1`, d.RobotID, op.ID, now, now, account, op.Action, origin, at, status, op.DurationMS, gen, check, send, attempts, nullable(op.ErrorCode), state, op.ID, encode(op))
	if err != nil {
		return err
	}
	if err = exec(ctx, tx, "UPDATE business_operations SET platform_code=?,account_external=?,status_code=?,reference_id=? WHERE robot_id=? AND id=?", op.Platform, op.Account, op.Status, op.Reference, d.RobotID, op.ID); err != nil {
		return err
	}
	for _, u := range op.Usage {
		key := u.RequestID
		if u.ThreadID != "" && u.TurnID != "" {
			key = u.ThreadID + ":" + u.TurnID
		}
		if key == "" {
			return fmt.Errorf("missing usage identity")
		}
		id := stable(u.Provider, key)
		scope := 1
		attribution := 1
		if len(u.RelatedOperations) > 1 {
			scope = 2
			attribution = 2
		}
		source := 1
		if u.Source == "host_reported" {
			source = 3
		} else if u.ThreadID != "" {
			source = 2
		}
		err = exec(ctx, tx, `INSERT INTO model_usage_records(robot_id,id,created_at,updated_at,provider_code,model_name,meter_key,meter_scope,provider_request_id,provider_thread_id,provider_turn_id,occurred_at,reported_at,usage_source,input_tokens,output_tokens,cached_input_tokens,reasoning_output_tokens,completeness) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1) ON CONFLICT(robot_id,provider_code,meter_key) DO UPDATE SET updated_at=excluded.updated_at,input_tokens=excluded.input_tokens,output_tokens=excluded.output_tokens,cached_input_tokens=excluded.cached_input_tokens,reasoning_output_tokens=excluded.reasoning_output_tokens,receipt_revision=model_usage_records.receipt_revision+1`, d.RobotID, id, now, now, u.Provider, u.Model, key, scope, nullable(u.RequestID), nullable(u.ThreadID), nullable(u.TurnID), at, now, source, u.Input, u.Output, u.CachedInput, u.ReasoningOutput)
		if err != nil {
			return err
		}
		if err = exec(ctx, tx, "INSERT INTO operation_usage_links(robot_id,created_at,operation_id,usage_id,attribution_kind) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING", d.RobotID, now, op.ID, id, attribution); err != nil {
			return err
		}
	}
	if op.UsageOwner != "" {
		if err = exec(ctx, tx, `INSERT INTO operation_usage_links(robot_id,created_at,operation_id,usage_id,attribution_kind) SELECT robot_id,?,?,usage_id,2 FROM operation_usage_links WHERE robot_id=? AND operation_id=? ON CONFLICT DO NOTHING`, now, op.ID, d.RobotID, op.UsageOwner); err != nil {
			return err
		}
	}
	// 先导入共享操作、后补到拥有者的回执也能建立关联。
	if err = exec(ctx, tx, `INSERT INTO operation_usage_links(robot_id,created_at,operation_id,usage_id,attribution_kind) SELECT o.robot_id,?,o.id,l.usage_id,2 FROM business_operations o JOIN operation_usage_links l ON l.robot_id=o.robot_id AND l.operation_id=? WHERE o.robot_id=? AND json_extract(o.legacy_payload,'$.usageOwner')=? ON CONFLICT DO NOTHING`, now, op.ID, d.RobotID, op.ID); err != nil {
		return err
	}
	return nil
}

// Operations 保持现有业务恢复协议，所有行来自SQLite而非旧JSONL。
func (d *Database) Operations(ctx context.Context) ([]model.Operation, error) {
	rows, err := d.SQL.QueryContext(ctx, "SELECT legacy_payload FROM business_operations WHERE robot_id=? ORDER BY started_at,id", d.RobotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Operation{}
	for rows.Next() {
		var raw string
		var op model.Operation
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &op); err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, rows.Err()
}

// AppendEvent 保存有来源的动作事实，历史读取不依赖近期缓存。
func (d *Database) AppendEvent(ctx context.Context, event *model.Event) error {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = d.writeEvent(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit()
}
func (d *Database) writeEvent(ctx context.Context, tx *sql.Tx, event *model.Event) error {
	now := time.Now().UnixMilli()
	return exec(ctx, tx, "INSERT INTO activity_events(robot_id,id,created_at,updated_at,kind,summary,occurred_at,scope_id,data_json) VALUES(?,?,?,?,?,?,?,'public',?) ON CONFLICT(robot_id,id) DO NOTHING", d.RobotID, event.ID, now, now, event.Kind, event.Summary, parseTime(event.At), encode(event.Data))
}

// Events 以索引读取限定数量的事实事件。
func (d *Database) Events(ctx context.Context, limit int) ([]model.Event, error) {
	if limit < 0 {
		return nil, fmt.Errorf("invalid event limit")
	}
	if limit == 0 {
		limit = -1
	}
	rows, err := d.SQL.QueryContext(ctx, "SELECT id,occurred_at,kind,summary,data_json FROM activity_events WHERE robot_id=? ORDER BY occurred_at DESC,id DESC LIMIT ?", d.RobotID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Event{}
	for rows.Next() {
		var e model.Event
		var at int64
		var raw string
		if err = rows.Scan(&e.ID, &at, &e.Kind, &e.Summary, &raw); err != nil {
			return nil, err
		}
		e.At = time.UnixMilli(at).UTC().Format(time.RFC3339Nano)
		if err = json.Unmarshal([]byte(raw), &e.Data); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	slices.Reverse(out)
	return out, rows.Err()
}

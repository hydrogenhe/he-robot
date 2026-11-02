package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// withdrawGrowthSource 撤回来源时取消整段派生经历；保留冲销账和已消费的日额度。
func (d *Database) withdrawGrowthSource(ctx context.Context, tx *sql.Tx, platform, account, peer, key string) error {
	sourceKey := platform + ":" + account + ":private:" + peer + ":message:" + key
	scope := stable("private", stable(platform, account, peer))
	now := time.Now().UnixMilli()
	source := stable("growth-source", sourceKey)
	if err := exec(ctx, tx, `INSERT INTO sources(robot_id,id,created_at,updated_at,scope_id,kind,dedupe_key,result,availability,is_del) VALUES(?,?,?,?,?,1,?,1,3,1) ON CONFLICT(robot_id,id) DO UPDATE SET content_text=NULL,availability=3,is_del=1`, d.RobotID, source, now, now, scope, sourceKey); err != nil {
		return err
	}
	var window string
	err := tx.QueryRowContext(ctx, "SELECT window_id FROM growth_observations WHERE robot_id=? AND source_key=?", d.RobotID, sourceKey).Scan(&window)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	keys := []string{sourceKey}
	if window != "" {
		keys = append(keys, "conversation:"+window)
		if err = exec(ctx, tx, "UPDATE growth_windows SET status=2,input_json='{}' WHERE robot_id=? AND id=?", d.RobotID, window); err != nil {
			return err
		}
		if err = exec(ctx, tx, "UPDATE growth_observations SET summary='' WHERE robot_id=? AND window_id=?", d.RobotID, window); err != nil {
			return err
		}
	}
	for _, key := range keys {
		episode := stable("episode", key)
		rows, err := tx.QueryContext(ctx, `SELECT id,dimension,coalesce(topic_id,''),coalesce(person_id,''),rule_version_id FROM growth_entries e WHERE robot_id=? AND episode_id=? AND operation_kind=1 AND NOT EXISTS(SELECT 1 FROM growth_entries r WHERE r.robot_id=e.robot_id AND r.reverses_entry_id=e.id)`, d.RobotID, episode)
		if err != nil {
			return err
		}
		type affected struct {
			id                  string
			dimension           int
			topic, person, rule string
		}
		list := []affected{}
		for rows.Next() {
			var item affected
			if err = rows.Scan(&item.id, &item.dimension, &item.topic, &item.person, &item.rule); err != nil {
				rows.Close()
				return err
			}
			list = append(list, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, item := range list {
			id := stable("withdrawal", item.id)
			if err = exec(ctx, tx, `INSERT INTO growth_entries(robot_id,id,created_at,updated_at,episode_id,source_memory_id,episode_modifier_id,scope_id,dimension,topic_id,person_id,rule_version_id,period_id,operation_kind,reverses_entry_id,idempotency_key,base_units,signal_direction,relationship_factor_bp,interaction_factor_bp,allocation_bp,repeat_discount_bp,uncapped_delta,applied_delta,evidence_kind,occurred_at) SELECT robot_id,?,?,?,episode_id,source_memory_id,episode_modifier_id,scope_id,dimension,topic_id,person_id,rule_version_id,period_id,2,id,?,base_units,-signal_direction,relationship_factor_bp,interaction_factor_bp,allocation_bp,repeat_discount_bp,-uncapped_delta,-applied_delta,evidence_kind,? FROM growth_entries WHERE robot_id=? AND id=? ON CONFLICT DO NOTHING`, id, now, now, id, now, d.RobotID, item.id); err != nil {
				return err
			}
			if err = d.rebuildGrowth(ctx, tx, item.dimension, item.topic, item.person, item.rule, id, now); err != nil {
				return err
			}
		}
		if err = exec(ctx, tx, "DELETE FROM memory_search_documents WHERE robot_id=? AND memory_id=?", d.RobotID, episode); err != nil {
			return err
		}
		if err = exec(ctx, tx, "UPDATE memories SET summary='',title=NULL,is_del=1,storage_state=3 WHERE robot_id=? AND id IN(SELECT from_memory_id FROM memory_links WHERE robot_id=? AND to_memory_id=?)", d.RobotID, d.RobotID, episode); err != nil {
			return err
		}
		if err = exec(ctx, tx, "UPDATE memories SET summary='',title=NULL,is_del=1,storage_state=3 WHERE robot_id=? AND id=?", d.RobotID, episode); err != nil {
			return err
		}
		if err = exec(ctx, tx, "UPDATE sources SET content_text=NULL,is_del=1,availability=3 WHERE robot_id=? AND id=?", d.RobotID, stable("growth-source", key)); err != nil {
			return err
		}
	}
	return nil
}

// growthWindowActive 在事务内复核封段来源，防止并发遗忘后重新产生经验。
func (d *Database) growthWindowActive(ctx context.Context, tx *sql.Tx, key string) (bool, error) {
	var state int
	err := tx.QueryRowContext(ctx, "SELECT status FROM growth_windows WHERE robot_id=? AND id=?", d.RobotID, key).Scan(&state)
	if err != nil {
		return false, fmt.Errorf("growth window missing: %w", err)
	}
	return state != 2, nil
}

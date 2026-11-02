package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// QueueGrowth 对话按五分钟空闲/二十分钟硬上限封段，单条消息不直接发经验。
func (d *Database) QueueGrowth(ctx context.Context, input *GrowthInput) error {
	if input == nil {
		return fmt.Errorf("missing growth evidence")
	}
	if input.EvidenceKind != 2 {
		return d.ApplyGrowth(ctx, input)
	}
	if err := d.FlushGrowth(ctx, input.Now); err != nil {
		return err
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var deleted int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM sources WHERE robot_id=? AND id=? AND is_del=1", d.RobotID, stable("growth-source", input.Key)).Scan(&deleted); err != nil {
		return err
	}
	if deleted > 0 {
		return nil
	}
	var seen int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM growth_observations WHERE robot_id=? AND source_key=?", d.RobotID, input.Key).Scan(&seen); err != nil {
		return err
	}
	if seen > 0 {
		return nil
	}
	key := stable(input.Platform, input.Account, input.Peer, input.Visibility)
	now := input.Now.UnixMilli()
	var id, raw string
	var started int64
	err = tx.QueryRowContext(ctx, "SELECT id,input_json,started_at FROM growth_windows WHERE robot_id=? AND scope_key=? AND status=0 AND seal_due_at>? ORDER BY started_at DESC LIMIT 1", d.RobotID, key, now).Scan(&id, &raw, &started)
	record := *input
	record.Awards = append([]GrowthAward(nil), input.Awards...)
	if err == sql.ErrNoRows {
		id = stable("window", input.Key)
		started = now
		if err = exec(ctx, tx, "INSERT INTO growth_windows(robot_id,id,scope_key,started_at,last_activity_at,seal_due_at,input_json) VALUES(?,?,?,?,?,?,?)", d.RobotID, id, key, started, now, now+int64(5*time.Minute/time.Millisecond), encode(record)); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		if err = json.Unmarshal([]byte(raw), &record); err != nil {
			return err
		}
		// 同一段内保留首次来源身份/倍率，主题信号更新，不把每条建议相加刷经验。
		for _, award := range input.Awards {
			found := false
			for i := range record.Awards {
				if record.Awards[i].Topic == award.Topic {
					record.Awards[i] = award
					found = true
					break
				}
			}
			if !found && len(record.Awards) < 3 {
				record.Awards = append(record.Awards, award)
			}
		}
		record.PersonAffinity = input.PersonAffinity
		record.Reaction = input.Reaction
		due := min(now+int64(5*time.Minute/time.Millisecond), started+int64(20*time.Minute/time.Millisecond))
		if err = exec(ctx, tx, "UPDATE growth_windows SET last_activity_at=?,seal_due_at=?,input_json=? WHERE robot_id=? AND id=? AND status=0", now, due, encode(record), d.RobotID, id); err != nil {
			return err
		}
	}
	if err = exec(ctx, tx, "INSERT INTO growth_observations(robot_id,source_key,window_id,summary,occurred_at) VALUES(?,?,?,?,?)", d.RobotID, input.Key, id, input.Summary, parseTime(input.At)); err != nil {
		return err
	}
	return tx.Commit()
}

// FlushGrowth 只处理已封段的持久候选，不调用模型；断电重放由episode唯一键防重复。
func (d *Database) FlushGrowth(ctx context.Context, now time.Time) error {
	rows, err := d.SQL.QueryContext(ctx, "SELECT id,input_json,seal_due_at FROM growth_windows WHERE robot_id=? AND status=0 AND seal_due_at<=? ORDER BY seal_due_at LIMIT 100", d.RobotID, now.UnixMilli())
	if err != nil {
		return err
	}
	type pending struct {
		id, raw string
		due     int64
	}
	items := []pending{}
	for rows.Next() {
		var p pending
		if err = rows.Scan(&p.id, &p.raw, &p.due); err != nil {
			rows.Close()
			return err
		}
		items = append(items, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range items {
		var input GrowthInput
		if err = json.Unmarshal([]byte(p.raw), &input); err != nil {
			return err
		}
		input.Key = "conversation:" + p.id
		input.Now = time.UnixMilli(p.due)
		// 合并多个主题时分摊单段总奖励，不能借轮换主题扩大发奖上限。
		var totalFam, totalAffinity int64
		for _, a := range input.Awards {
			totalFam += a.Familiarity
			v := a.Affinity
			if v < 0 {
				v = -v
			}
			totalAffinity += v
		}
		maxFam := input.BaseFamiliarity * input.RelationFactor * input.InteractionFactor / 10000 / 10000
		maxAffinity := int64(50) * input.RelationFactor * input.InteractionFactor / 10000 / 10000
		for i := range input.Awards {
			if totalFam > maxFam {
				input.Awards[i].Familiarity = input.Awards[i].Familiarity * maxFam / totalFam
			}
			if totalAffinity > maxAffinity {
				input.Awards[i].Affinity = input.Awards[i].Affinity * maxAffinity / totalAffinity
			}
		}
		if err = d.ApplyGrowth(ctx, &input); err != nil {
			return err
		}
		if _, err = d.SQL.ExecContext(ctx, "UPDATE growth_windows SET status=1 WHERE robot_id=? AND id=? AND status=0", d.RobotID, p.id); err != nil {
			return err
		}
	}
	return nil
}

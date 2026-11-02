package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"gpt-robot/model"
	"time"
)

func (d *Database) diarySources(ctx context.Context, tx *sql.Tx, version string, entry *model.DiaryEntry) error {
	now := time.Now().UnixMilli()
	for _, key := range entry.Sources {
		id := stable("diary-source", key)
		var deleted int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sources WHERE robot_id=? AND id=? AND is_del=1", d.RobotID, id).Scan(&deleted); err != nil {
			return err
		}
		if deleted > 0 {
			return model.Fail("SOURCE_WITHDRAWN", "日记素材已撤回，未保存生成内容")
		}
		if err := exec(ctx, tx, `INSERT INTO sources(robot_id,id,created_at,updated_at,scope_id,kind,external_object_id,dedupe_key,result,observation_scope) VALUES(?,?,?,?,'owner-private',6,?,?,1,'日记素材引用；原始读取范围以来源为准') ON CONFLICT DO NOTHING`, d.RobotID, id, now, now, key, id); err != nil {
			return err
		}
		if err := exec(ctx, tx, "INSERT INTO diary_inputs(robot_id,created_at,version_id,source_id,source_version) VALUES(?,?,?,?,1) ON CONFLICT DO NOTHING", d.RobotID, now, version, id); err != nil {
			return err
		}
	}
	return nil
}

func (d *Database) withdrawDiarySource(ctx context.Context, tx *sql.Tx, platform, account, peer, key string) error {
	hash := stable(platform, account, peer, key)
	source := stable("diary-source", hash)
	now := time.Now().UnixMilli()
	if err := exec(ctx, tx, `INSERT INTO sources(robot_id,id,created_at,updated_at,scope_id,kind,external_object_id,dedupe_key,result,availability,is_del) VALUES(?,?,?,?,'owner-private',6,?,?,1,3,1) ON CONFLICT(robot_id,id) DO UPDATE SET content_text=NULL,availability=3,is_del=1`, d.RobotID, source, now, now, hash, source); err != nil {
		return err
	}
	if err := exec(ctx, tx, "UPDATE diary_versions SET title='素材已撤回',body='',status=1 WHERE robot_id=? AND id IN(SELECT version_id FROM diary_inputs WHERE robot_id=? AND source_id=?)", d.RobotID, d.RobotID, source); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,body FROM state_documents WHERE robot_id=? AND path LIKE 'data/private/diary/%' AND EXISTS(SELECT 1 FROM json_each(state_documents.body,'$.sources') WHERE value=?)", d.RobotID, hash)
	if err != nil {
		return err
	}
	type document struct{ id, body string }
	list := []document{}
	for rows.Next() {
		var item document
		if err = rows.Scan(&item.id, &item.body); err != nil {
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
		var entry model.DiaryEntry
		if err = json.Unmarshal([]byte(item.body), &entry); err != nil {
			return err
		}
		entry.Title = "素材已撤回"
		entry.Body = ""
		entry.Status = "failed"
		entry.ErrorCode = "SOURCE_WITHDRAWN"
		raw := encode(entry)
		if err = exec(ctx, tx, "UPDATE state_documents SET body=?,content_hash=?,updated_at=?,row_version=row_version+1 WHERE robot_id=? AND id=?", raw, digest([]byte(raw)), now, d.RobotID, item.id); err != nil {
			return err
		}
	}
	return nil
}

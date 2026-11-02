package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"gpt-robot/model"
)

func (d *Database) projectExecution(ctx context.Context, tx *sql.Tx, path string, raw []byte) error {
	now := time.Now().UnixMilli()
	switch path {
	case "data/events/drafts.json":
		var drafts []model.Draft
		if err := json.Unmarshal(raw, &drafts); err != nil {
			return err
		}
		for _, v := range drafts {
			if err := d.execution(ctx, tx, path, v.ID, v.Kind, v.Status, parseTime(v.CreatedAt), "internal", v.Text, v.TargetUser, "", 0, v.PlatformMessage, encode(v)); err != nil {
				return err
			}
		}
	case "data/private/state.json":
		var state model.PrivateState
		if err := json.Unmarshal(raw, &state); err != nil {
			return err
		}
		for _, v := range state.Inbox {
			status := "received"
			body := v.Text
			if v.Withdrawn {
				status = "withdrawn"
				body = ""
				v.Text = ""
				v.Image = nil
			}
			if err := d.execution(ctx, tx, path+":inbox", v.Key, "message", status, v.Timestamp*1000, "owner-private", body, v.Sender, "", 0, v.Key, encode(v)); err != nil {
				return err
			}
		}
		for _, v := range state.Outbox {
			if err := d.execution(ctx, tx, path+":outbox", v.RequestID, "reply", v.Status, parseTime(v.At), "owner-private", v.Text, "owner", v.GroupID, v.Part, v.MessageKey, encode(v)); err != nil {
				return err
			}
		}
		return exec(ctx, tx, `INSERT INTO collection_states(robot_id,id,kind,cursor,since_at,paused,updated_at,payload) VALUES(?,?,?, ?,?,?,?,?) ON CONFLICT(robot_id,id) DO UPDATE SET cursor=excluded.cursor,since_at=excluded.since_at,paused=excluded.paused,updated_at=excluded.updated_at,payload=excluded.payload`, d.RobotID, stable(path), "private-message", state.Cursor, state.Since, state.ReadPaused, now, encode(struct {
			Initialized bool
			Revision    int64
			Reason      string
		}{state.Initialized, state.Revision, state.Reason}))
	case "data/character/state.json":
		var state model.CharacterState
		if err := json.Unmarshal(raw, &state); err != nil {
			return err
		}
		for _, v := range state.Changes {
			values := []struct {
				key   string
				value int
			}{{"mood:" + v.Mood, v.Intensity}, {"warmth", v.Traits.Warmth}, {"curiosity", v.Traits.Curiosity}, {"playfulness", v.Traits.Playfulness}}
			for _, x := range values {
				if err := exec(ctx, tx, `INSERT INTO state_changes(robot_id,id,created_at,updated_at,scope_id,state_key,previous_units,next_units,occurred_at) VALUES(?,?,?,?,'internal',?,coalesce((SELECT next_units FROM state_changes WHERE robot_id=? AND state_key=? ORDER BY occurred_at DESC LIMIT 1),?),?,?) ON CONFLICT DO NOTHING`, d.RobotID, stable(v.Source, x.key), now, now, x.key, d.RobotID, x.key, x.value*100, x.value*100, parseTime(v.At)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (d *Database) execution(ctx context.Context, tx *sql.Tx, namespace, key, kind, status string, at int64, scope, body, target, group string, part int, message, payload string) error {
	if key == "" {
		return nil
	}
	return exec(ctx, tx, `INSERT INTO execution_records(robot_id,id,namespace,external_id,kind,status,occurred_at,scope_id,body,target,group_id,part,message_key,payload) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(robot_id,namespace,external_id) DO UPDATE SET status=excluded.status,body=excluded.body,message_key=excluded.message_key,payload=excluded.payload`, d.RobotID, stable(namespace, key), namespace, key, kind, status, at, scope, body, target, nullable(group), part, nullable(message), payload)
}

func (d *Database) projectStickers(ctx context.Context, tx *sql.Tx, path string, raw []byte) error {
	var library struct {
		Items []model.LearnedSticker `json:"items"`
	}
	if err := json.Unmarshal(raw, &library); err != nil {
		return err
	}
	scope := stable("sticker-scope", path)
	now := time.Now().UnixMilli()
	if err := exec(ctx, tx, "INSERT INTO access_scopes(robot_id,id,created_at,updated_at,kind) VALUES(?,?,?,?,4) ON CONFLICT DO NOTHING", d.RobotID, scope, now, now); err != nil {
		return err
	}
	if err := exec(ctx, tx, "UPDATE stickers SET status=1,label='' WHERE robot_id=? AND scope_id=?", d.RobotID, scope); err != nil {
		return err
	}
	for _, v := range library.Items {
		sid := stable(path, v.SourceKey)
		asset := stable(path, v.ID)
		if err := exec(ctx, tx, `INSERT INTO sources(robot_id,id,created_at,updated_at,scope_id,kind,external_object_id,dedupe_key,observed_at,result) VALUES(?,?,?,?,?,1,?,?,?,1) ON CONFLICT DO NOTHING`, d.RobotID, sid, now, now, scope, v.SourceKey, sid, parseTime(v.At)); err != nil {
			return err
		}
		relative := filepath.ToSlash(filepath.Join(filepath.Dir(path), v.ID+".image"))
		// 文件仍由受控素材服务校验；大小未知时不冒充已下载的新文件。
		if err := exec(ctx, tx, `INSERT INTO source_assets(robot_id,id,created_at,updated_at,source_id,asset_kind,relative_path,content_hash,mime_type,size_bytes) VALUES(?,?,?,?,?,2,?,?,?,0) ON CONFLICT DO NOTHING`, d.RobotID, asset, now, now, sid, relative, v.ID, "image/"+strings.ToLower(v.Image.Format)); err != nil {
			return err
		}
		if err := exec(ctx, tx, `INSERT INTO stickers(robot_id,id,created_at,updated_at,scope_id,asset_id,label,image_source_id) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(robot_id,id) DO UPDATE SET label=excluded.label,status=0,updated_at=excluded.updated_at`, d.RobotID, asset, now, now, scope, asset, v.Label, sid); err != nil {
			return err
		}
	}
	return nil
}

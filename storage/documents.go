package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"gpt-robot/model"
)

func (d *Database) projectDocument(ctx context.Context, tx *sql.Tx, path string, raw []byte) error {
	if err := d.projectExecution(ctx, tx, path, raw); err != nil {
		return err
	}
	switch {
	case strings.HasPrefix(path, "data/private/stickers/") && strings.HasSuffix(path, "/index.json"):
		return d.projectStickers(ctx, tx, path, raw)
	case path == "data/character/persona.md":
		return exec(ctx, tx, "UPDATE robots SET core_persona=?,core_version=core_version+CASE WHEN core_persona<>? THEN 1 ELSE 0 END,updated_at=? WHERE robot_id=?", string(raw), string(raw), time.Now().UnixMilli(), d.RobotID)
	case path == "data/memory/notes.json":
		var values []model.Note
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		for _, n := range values {
			if n.Kind == "diary" {
				continue
			}
			if err := d.saveMemory(ctx, tx, path, n.ID, "public", n.Title, n.Text, parseTime(n.At), n.Sources); err != nil {
				return err
			}
		}
		return d.replaceRecords(ctx, tx, path, raw, "public")
	case strings.HasPrefix(path, "data/private/memory/"):
		var state model.PrivateMemoryState
		if err := json.Unmarshal(raw, &state); err != nil {
			return err
		}
		scope, err := d.privateScope(ctx, tx, state.Platform, state.Account, state.Peer)
		if err != nil {
			return err
		}
		ids := map[string]bool{}
		for _, m := range state.Records {
			refs := []string{}
			at := int64(0)
			for _, source := range m.Sources {
				if source.Withdrawn {
					if err = d.withdrawDiarySource(ctx, tx, state.Platform, state.Account, state.Peer, source.MessageKey); err != nil {
						return err
					}
					if err = d.withdrawGrowthSource(ctx, tx, state.Platform, state.Account, state.Peer, source.MessageKey); err != nil {
						return err
					}
				}
				if !source.Withdrawn {
					refs = append(refs, source.MessageKey)
					at = max(at, parseTime(source.OccurredAt))
				}
			}
			if len(refs) == 0 {
				continue
			}
			id := stable(path, m.ID)
			ids[id] = true
			if err = d.saveMemory(ctx, tx, path, m.ID, scope, "用户陈述", m.Quote, at, refs); err != nil {
				return err
			}
		}
		rows, err := tx.QueryContext(ctx, "SELECT id FROM memories WHERE robot_id=? AND scope_id=? AND extractor_version=? AND is_del=0", d.RobotID, scope, path)
		if err != nil {
			return err
		}
		removed := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			if !ids[id] {
				removed = append(removed, id)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range removed {
			if err = exec(ctx, tx, "DELETE FROM memory_search_documents WHERE robot_id=? AND memory_id=?", d.RobotID, id); err != nil {
				return err
			}
			if err = exec(ctx, tx, "UPDATE memories SET summary='',title=NULL,storage_state=3,is_del=1,row_version=row_version+1 WHERE robot_id=? AND id=?", d.RobotID, id); err != nil {
				return err
			}
			if err = exec(ctx, tx, "UPDATE sources SET content_text=NULL,availability=3,is_del=1 WHERE robot_id=? AND id IN(SELECT source_id FROM memory_sources WHERE robot_id=? AND memory_id=?)", d.RobotID, d.RobotID, id); err != nil {
				return err
			}
		}
		if len(removed) > 0 {
			if err = exec(ctx, tx, "UPDATE robots SET auth_epoch=auth_epoch+1 WHERE robot_id=?", d.RobotID); err != nil {
				return err
			}
		}
		return d.replaceRecords(ctx, tx, path, []byte(encode(state.Records)), scope)
	case strings.HasPrefix(path, "data/private/diary/"):
		var entry model.DiaryEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return err
		}
		return d.saveDiary(ctx, tx, path, &entry)
	case path == ".private/character-name.json":
		var state struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &state); err != nil {
			return err
		}
		return exec(ctx, tx, "UPDATE robots SET display_name=?,updated_at=? WHERE robot_id=?", state.Name, time.Now().UnixMilli(), d.RobotID)
	default:
		return nil
	}
}

func (d *Database) replaceRecords(ctx context.Context, tx *sql.Tx, path string, raw []byte, scope string) error {
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return err
	}
	if err := exec(ctx, tx, "DELETE FROM legacy_records WHERE robot_id=? AND namespace=?", d.RobotID, path); err != nil {
		return err
	}
	for index, value := range list {
		var key struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(value, &key); err != nil {
			return err
		}
		if key.ID == "" {
			key.ID = digest(value)
		}
		now := time.Now().UnixMilli()
		if err := exec(ctx, tx, "INSERT INTO legacy_records(robot_id,id,created_at,updated_at,namespace,record_key,position,payload,scope_id) VALUES(?,?,?,?,?,?,?,?,?)", d.RobotID, stable(path, key.ID), now, now, path, key.ID, index, string(value), scope); err != nil {
			return err
		}
	}
	return nil
}

func (d *Database) privateScope(ctx context.Context, tx *sql.Tx, platform, account, peer string) (string, error) {
	if platform == "" || account == "" || peer == "" {
		return "", fmt.Errorf("missing private identity")
	}
	now := time.Now().UnixMilli()
	aid := stable(platform, account)
	pid := stable(platform, peer)
	cid := stable(platform, account, peer)
	scope := stable("private", cid)
	statements := []struct {
		q string
		a []any
	}{
		{"INSERT INTO platform_accounts(robot_id,id,created_at,updated_at,platform,platform_uid) VALUES(?,?,?,?,?,?) ON CONFLICT DO NOTHING", []any{d.RobotID, aid, now, now, platform, account}},
		{"INSERT INTO persons(robot_id,id,created_at,updated_at) VALUES(?,?,?,?) ON CONFLICT DO NOTHING", []any{d.RobotID, pid, now, now}},
		{"INSERT INTO person_accounts(robot_id,id,created_at,updated_at,person_id,platform,platform_uid,verification_kind) VALUES(?,?,?,?,?,?,?,1) ON CONFLICT DO NOTHING", []any{d.RobotID, stable("person-account", pid), now, now, pid, platform, peer}},
		{"INSERT INTO conversations(robot_id,id,created_at,updated_at,platform_account_id,external_conversation_id,kind) VALUES(?,?,?,?,?,?,1) ON CONFLICT DO NOTHING", []any{d.RobotID, cid, now, now, aid, peer}},
		{"INSERT INTO access_scopes(robot_id,id,created_at,updated_at,kind,conversation_id) VALUES(?,?,?,?,2,?) ON CONFLICT DO NOTHING", []any{d.RobotID, scope, now, now, cid}},
		{"INSERT INTO scope_grants(robot_id,id,created_at,updated_at,scope_id,person_id,conversation_id,valid_from,grant_kind) VALUES(?,?,?,?,?,?,?,?,1) ON CONFLICT DO NOTHING", []any{d.RobotID, stable("grant", scope, pid), now, now, scope, pid, cid, now}},
	}
	for _, s := range statements {
		if err := exec(ctx, tx, s.q, s.a...); err != nil {
			return "", err
		}
	}
	return scope, nil
}
func (d *Database) saveMemory(ctx context.Context, tx *sql.Tx, namespace, key, scope, title, body string, at int64, refs []string) error {
	now := time.Now().UnixMilli()
	id := stable(namespace, key)
	var deleted int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM memories WHERE robot_id=? AND id=? AND is_del=1", d.RobotID, id).Scan(&deleted); err != nil {
		return err
	}
	if deleted != 0 {
		return nil
	}
	summary := model.Clip(body, 2000)
	if err := exec(ctx, tx, `INSERT INTO memories(robot_id,id,created_at,updated_at,scope_id,kind,title,summary,occurred_at,fact_state,extractor_version) VALUES(?,?,?,?,?,2,?,?,?,1,?) ON CONFLICT(robot_id,id) DO UPDATE SET summary=excluded.summary,title=excluded.title,updated_at=excluded.updated_at,occurred_at=excluded.occurred_at,row_version=memories.row_version+1`, d.RobotID, id, now, now, scope, title, summary, at, namespace); err != nil {
		return err
	}
	if len(refs) == 0 {
		refs = []string{key}
	}
	for _, ref := range refs {
		sid := stable(namespace, ref)
		if err := exec(ctx, tx, `INSERT INTO sources(robot_id,id,created_at,updated_at,scope_id,kind,external_object_id,dedupe_key,content_text,observed_at,result) VALUES(?,?,?,?,?,1,?,?,?,?,1) ON CONFLICT(robot_id,id) DO UPDATE SET content_text=CASE WHEN sources.is_del=0 THEN excluded.content_text ELSE NULL END`, d.RobotID, sid, now, now, scope, ref, sid, body, at); err != nil {
			return err
		}
		if err := exec(ctx, tx, "INSERT INTO memory_sources(robot_id,created_at,memory_id,source_id,evidence_role) VALUES(?,?,?,?,1) ON CONFLICT DO NOTHING", d.RobotID, now, id, sid); err != nil {
			return err
		}
	}
	tokenized := tokenize(title + " " + model.Clip(body, 4000))
	return exec(ctx, tx, `INSERT INTO memory_search_documents(robot_id,id,created_at,updated_at,memory_id,source_version,scope_id,tokenized_text,tokenizer_version,storage_state,fact_state,search_rowid) VALUES(?,?,?,?,?,1,?,?,'bigram-v1',0,1,(SELECT coalesce(max(search_rowid),0)+1 FROM memory_search_documents)) ON CONFLICT(robot_id,memory_id) DO UPDATE SET tokenized_text=excluded.tokenized_text,source_version=memory_search_documents.source_version+1,updated_at=excluded.updated_at`, d.RobotID, id, now, now, id, scope, tokenized)
}

func tokenize(text string) string {
	out := []string{}
	word := []rune{}
	var previous rune
	for _, r := range []rune(strings.ToLower(text)) {
		if unicode.Is(unicode.Han, r) {
			if len(word) > 0 {
				out = append(out, string(word))
				word = nil
			}
			out = append(out, string(r))
			if previous != 0 {
				out = append(out, string([]rune{previous, r}))
			}
			previous = r
			continue
		}
		previous = 0
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word = append(word, r)
		} else if len(word) > 0 {
			out = append(out, string(word))
			word = nil
		}
	}
	if len(word) > 0 {
		out = append(out, string(word))
	}
	return strings.Join(out, " ")
}

func (d *Database) saveDiary(ctx context.Context, tx *sql.Tx, path string, entry *model.DiaryEntry) error {
	if _, err := time.Parse("2006-01-02", entry.Date); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	id := stable("diary", entry.Date)
	if err := exec(ctx, tx, "INSERT INTO diaries(robot_id,id,created_at,updated_at,scope_id,local_day,timezone) VALUES(?,?,?,?,'owner-private',?,'Asia/Shanghai') ON CONFLICT DO NOTHING", d.RobotID, id, now, now, entry.Date); err != nil {
		return err
	}
	version := stable(id, entry.OperationID, entry.Title, entry.Body)
	if entry.Body != "" {
		if err := exec(ctx, tx, `INSERT INTO diary_versions(robot_id,id,created_at,updated_at,diary_id,version_no,title,body,source_date) VALUES(?,?,?,?,?,(SELECT coalesce(max(version_no),0)+1 FROM diary_versions WHERE robot_id=? AND diary_id=?),?,?,?) ON CONFLICT(robot_id,id) DO NOTHING`, d.RobotID, version, now, now, id, d.RobotID, id, entry.Title, entry.Body, entry.Date); err != nil {
			return err
		}
		if err := d.diarySources(ctx, tx, version, entry); err != nil {
			return err
		}
		if !strings.Contains(path, "/revisions/") && entry.Status == "completed" {
			if err := exec(ctx, tx, "UPDATE diaries SET current_version_id=?,updated_at=? WHERE robot_id=? AND id=?", version, now, d.RobotID, id); err != nil {
				return err
			}
		}
	}
	var operation any
	if entry.OperationID != "" {
		var found int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM business_operations WHERE robot_id=? AND id=?", d.RobotID, entry.OperationID).Scan(&found); err != nil {
			return err
		}
		if found > 0 {
			operation = entry.OperationID
		}
	}
	if entry.Body != "" && operation != nil {
		if err := exec(ctx, tx, "UPDATE diary_versions SET operation_id=? WHERE robot_id=? AND id=?", operation, d.RobotID, version); err != nil {
			return err
		}
	}
	if entry.OperationID != "" {
		state := map[string]int{"generating": 0, "completed": 1, "failed": 2, "interrupted": 3, "empty": 4}[entry.Status]
		var finished any
		if entry.Status != "generating" {
			finished = now
		}
		var vid any
		if entry.Body != "" {
			vid = version
		}
		if err := exec(ctx, tx, `INSERT INTO diary_attempts(robot_id,id,created_at,updated_at,diary_id,version_id,operation_id,trigger_kind,status,started_at,finished_at,error_code) VALUES(?,?,?,?,?,?,?,1,?,?,?,?) ON CONFLICT(robot_id,id) DO UPDATE SET updated_at=excluded.updated_at,version_id=excluded.version_id,operation_id=excluded.operation_id,status=excluded.status,finished_at=excluded.finished_at,error_code=excluded.error_code`, d.RobotID, stable("diary-attempt", entry.OperationID), now, now, id, vid, operation, state, parseTime(entry.At), finished, nullable(entry.ErrorCode)); err != nil {
			return err
		}
	}
	return nil
}

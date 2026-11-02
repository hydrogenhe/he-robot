package storage

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gpt-robot/model"
)

// ManagedDocument 指定由SQLite唯一写入的业务资料，凭据与开关配置仍保留受控文件。
func ManagedDocument(path string) bool {
	if path == ".private/character-name.json" {
		return true
	}
	if !strings.HasPrefix(path, "data/") || strings.HasPrefix(path, "data/robots/") {
		return false
	}
	return strings.HasSuffix(path, ".json") || path == "data/character/persona.md"
}

type legacyFile struct {
	path string
	raw  []byte
}

// ImportLegacy 先逐文件校验并私有备份，再原子导入；完成后不读取旧文件作为业务事实源。
func (d *Database) ImportLegacy(ctx context.Context, root string) error {
	var done int
	if err := d.SQL.QueryRowContext(ctx, "SELECT count(*) FROM migration_batches WHERE robot_id=? AND legacy_path='__initial__' AND status=1", d.RobotID).Scan(&done); err != nil {
		return err
	}
	if done > 0 {
		return nil
	}
	files := []legacyFile{}
	err := filepath.WalkDir(filepath.Join(root, "data"), func(path string, entry fs.DirEntry, walkErr error) error {
		if os.IsNotExist(walkErr) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel == "data/robots" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse symlink during migration: %s", rel)
		}
		if !ManagedDocument(rel) && !(strings.HasPrefix(rel, "data/events/") && strings.HasSuffix(rel, ".jsonl")) && rel != "data/usage/operations.jsonl" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(raw) > 128<<20 {
			return fmt.Errorf("legacy file exceeds migration budget: %s", rel)
		}
		if strings.HasSuffix(rel, ".json") && !json.Valid(raw) {
			return fmt.Errorf("invalid legacy JSON: %s", rel)
		}
		if strings.HasSuffix(rel, ".jsonl") && len(raw) > 0 && raw[len(raw)-1] != '\n' {
			return fmt.Errorf("incomplete legacy ledger: %s", rel)
		}
		files = append(files, legacyFile{path: rel, raw: raw})
		return nil
	})
	if err != nil {
		return err
	}
	name := ".private/character-name.json"
	if raw, err := os.ReadFile(filepath.Join(root, name)); err == nil {
		if !json.Valid(raw) {
			return fmt.Errorf("invalid character identity")
		}
		files = append(files, legacyFile{path: name, raw: raw})
	} else if !os.IsNotExist(err) {
		return err
	}
	backup := filepath.Join(root, "data", "robots", d.RobotID, "backups", "legacy-"+time.Now().UTC().Format("20060102T150405.000000000"))
	for _, file := range files {
		path := filepath.Join(backup, file.path)
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		if err = os.WriteFile(path, file.raw, 0600); err != nil {
			return err
		}
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, file := range files {
		if strings.HasSuffix(file.path, ".jsonl") {
			scan := bufio.NewScanner(bytes.NewReader(file.raw))
			scan.Buffer(make([]byte, 4096), 4<<20)
			for scan.Scan() {
				if len(bytes.TrimSpace(scan.Bytes())) == 0 {
					continue
				}
				if file.path == "data/usage/operations.jsonl" {
					var op model.Operation
					if err = json.Unmarshal(scan.Bytes(), &op); err == nil {
						err = d.writeOperation(ctx, tx, &op)
					}
				} else {
					var event model.Event
					if err = json.Unmarshal(scan.Bytes(), &event); err == nil {
						err = d.writeEvent(ctx, tx, &event)
					}
				}
				if err != nil {
					return fmt.Errorf("import %s: %w", file.path, err)
				}
			}
			if err = scan.Err(); err != nil {
				return err
			}
		} else if err = d.writeDocument(ctx, tx, file.path, file.raw); err != nil {
			return fmt.Errorf("import %s: %w", file.path, err)
		}
		now := time.Now().UnixMilli()
		_, err = tx.ExecContext(ctx, "INSERT INTO migration_batches(robot_id,id,created_at,updated_at,legacy_path,content_hash,status,imported_count,completed_at) VALUES(?,?,?,?,?,?,1,1,?)", d.RobotID, stable(file.path), now, now, file.path, digest(file.raw), now)
		if err != nil {
			return err
		}
	}
	now := time.Now().UnixMilli()
	_, err = tx.ExecContext(ctx, "INSERT INTO migration_batches(robot_id,id,created_at,updated_at,legacy_path,content_hash,status,imported_count,completed_at) VALUES(?,?,?,?,?,'initial-v1',1,?,?)", d.RobotID, "initial", now, now, "__initial__", len(files), now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func parseTime(value string) int64 {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}
func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func encode(value any) string { b, _ := json.Marshal(value); return string(b) }
func exec(ctx context.Context, tx *sql.Tx, query string, args ...any) error {
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

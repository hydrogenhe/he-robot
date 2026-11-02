// Package storage owns the robot-local SQLite database and versioned migrations.
package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"gpt-robot/model"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// migrationFiles 随程序发布数据库结构，运行时不依赖外部数据库服务。
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

// DatabaseInterface 集中声明机器人数据库的持久化入口。
type DatabaseInterface interface {
	DiaryOperationPreview(context.Context, string) (model.ActivityPreview, error)
	AuthEpoch(context.Context) (int64, error)
	RecallEpisodes(context.Context, *MemoryQuery) ([]model.RecalledEpisode, error)
	OperationPage(context.Context, *model.OperationQuery, []string) (*model.OperationPage, error)
	MemoryCandidates(context.Context, *MemoryQuery) ([][]byte, error)
	ApplyGrowth(context.Context, *GrowthInput) error
	QueueGrowth(context.Context, *GrowthInput) error
	FlushGrowth(context.Context, time.Time) error
	GrowthBalances(context.Context) ([]GrowthBalance, error)
	PersonBalances(context.Context) ([]PersonBalance, error)
	AppendOperation(context.Context, *model.Operation) error
	Operations(context.Context) ([]model.Operation, error)
	AppendEvent(context.Context, *model.Event) error
	Events(context.Context, int) ([]model.Event, error)
	Close() error
	ReadDocument(context.Context, string) ([]byte, bool, error)
	WriteDocument(context.Context, string, []byte) error
	ListDocuments(context.Context, string, int) ([][]byte, error)
	ImportLegacy(context.Context, string) error
}

// Database 的身份由宿主目录确定，不能由模型或网络请求选择。
type Database struct {
	SQL     *sql.DB
	RobotID string
	Root    string
}

var _ DatabaseInterface = (*Database)(nil)

// Open 打开独立机器人库，每个连接强制启用外键，初始化失败不回退到旧文件写入。
func Open(ctx context.Context, root, robotID string) (*Database, error) {
	if !safeID(robotID) {
		return nil, fmt.Errorf("invalid robot identity")
	}
	dir := filepath.Join(root, "data", "robots", robotID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "memory.sqlite")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	value := &Database{SQL: db, RobotID: robotID, Root: root}
	if err = value.initialize(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return value, nil
}

func safeID(id string) bool {
	if len(id) < 1 || len(id) > 80 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func (d *Database) initialize(ctx context.Context) error {
	var exists int
	if err := d.SQL.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'").Scan(&exists); err != nil {
		return err
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return err
	}
	backedUp := false
	for index, entry := range entries {
		version := index + 1
		raw, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		sum := digest(raw)
		if exists > 0 {
			var current string
			err = d.SQL.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE version=?", version).Scan(&current)
			if err == nil {
				if current != sum {
					return fmt.Errorf("schema checksum mismatch at version %d", version)
				}
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if !backedUp {
				backup := filepath.Join(d.Root, "data", "robots", d.RobotID, "backups", "schema-"+time.Now().UTC().Format("20060102T150405.000000000")+".sqlite")
				if err = os.MkdirAll(filepath.Dir(backup), 0700); err != nil {
					return err
				}
				if _, err = d.SQL.ExecContext(ctx, "VACUUM INTO ?", backup); err != nil {
					return err
				}
				if err = os.Chmod(backup, 0600); err != nil {
					return err
				}
				backedUp = true
			}
		}
		tx, err := d.SQL.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		script := strings.ReplaceAll(string(raw), "PRAGMA foreign_keys=ON;", "")
		script = strings.ReplaceAll(script, "BEGIN IMMEDIATE;", "")
		script = strings.ReplaceAll(script, "COMMIT;", "")
		if _, err = tx.ExecContext(ctx, script); err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version,checksum,applied_at) VALUES(?,?,?)", version, sum, time.Now().UnixMilli())
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("apply schema %d: %w", version, err)
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	var existing string
	err = d.SQL.QueryRowContext(ctx, "SELECT robot_id FROM robots WHERE singleton=1").Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && existing != d.RobotID {
		return fmt.Errorf("robot database ownership mismatch")
	}
	now := time.Now().UnixMilli()
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT INTO robots(singleton,robot_id,display_name,core_persona,created_at,updated_at) VALUES(1,?,'机器人','',?,?) ON CONFLICT(singleton) DO NOTHING", d.RobotID, now, now); err != nil {
		return err
	}
	for _, scope := range []struct {
		id   string
		kind int
	}{{"public", 1}, {"internal", 4}, {"owner-private", 3}} {
		if _, err = tx.ExecContext(ctx, "INSERT INTO access_scopes(robot_id,id,created_at,updated_at,kind) VALUES(?,?,?,?,?) ON CONFLICT(robot_id,id) DO NOTHING", d.RobotID, scope.id, now, now, scope.kind); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	var fk int
	if err = d.SQL.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		return err
	}
	if fk != 1 {
		return fmt.Errorf("foreign keys disabled")
	}
	return nil
}

// Close 关闭连接；调用方应先停止业务写入。
func (d *Database) Close() error { return d.SQL.Close() }

func digest(raw []byte) string      { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func stable(parts ...string) string { return digest([]byte(strings.Join(parts, "\x00"))) }

// ReadDocument 兼容现有有界配置/状态协议；业务实体同时由事务内结构化投影维护。
func (d *Database) ReadDocument(ctx context.Context, path string) ([]byte, bool, error) {
	var body string
	err := d.SQL.QueryRowContext(ctx, "SELECT body FROM state_documents WHERE robot_id=? AND path=?", d.RobotID, path).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return []byte(body), err == nil, err
}

// WriteDocument 在一个事务内保存状态和结构化实体，不再写旧JSON文件。
func (d *Database) WriteDocument(ctx context.Context, path string, raw []byte) error {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = d.writeDocument(ctx, tx, path, raw); err != nil {
		return err
	}
	return tx.Commit()
}
func (d *Database) writeDocument(ctx context.Context, tx *sql.Tx, path string, raw []byte) error {
	var previous string
	if err := tx.QueryRowContext(ctx, "SELECT content_hash FROM state_documents WHERE robot_id=? AND path=?", d.RobotID, path).Scan(&previous); err == nil && previous == digest(raw) {
		return nil
	} else if err != nil && err != sql.ErrNoRows {
		return err
	}
	now := time.Now().UnixMilli()
	_, err := tx.ExecContext(ctx, `INSERT INTO state_documents(robot_id,id,created_at,updated_at,path,body,content_hash) VALUES(?,?,?,?,?,?,?) ON CONFLICT(robot_id,path) DO UPDATE SET body=excluded.body,content_hash=excluded.content_hash,updated_at=excluded.updated_at,row_version=state_documents.row_version+1`, d.RobotID, stable(path), now, now, path, string(raw), digest(raw))
	if err != nil {
		return err
	}
	return d.projectDocument(ctx, tx, path, raw)
}

// ListDocuments 对日记等有界列表按稳定路径倒序读取，不扫描文件目录。
func (d *Database) ListDocuments(ctx context.Context, prefix string, limit int) ([][]byte, error) {
	if limit < 1 || limit > 200 {
		return nil, fmt.Errorf("invalid document limit")
	}
	rows, err := d.SQL.QueryContext(ctx, "SELECT body FROM state_documents WHERE robot_id=? AND substr(path,1,?)=? AND instr(substr(path,?),'/')=0 ORDER BY path DESC LIMIT ?", d.RobotID, len(prefix), prefix, len(prefix)+1, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := [][]byte{}
	for rows.Next() {
		var b string
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, []byte(b))
	}
	return out, rows.Err()
}

// AuthEpoch 返回当前隐私版本，生成前后用于阻止使用已失效的上下文。
func (d *Database) AuthEpoch(ctx context.Context) (int64, error) {
	var epoch int64
	err := d.SQL.QueryRowContext(ctx, "SELECT auth_epoch FROM robots WHERE robot_id=?", d.RobotID).Scan(&epoch)
	return epoch, err
}

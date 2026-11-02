package storage

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MemoryQuery 只接收服务端授权后确定的命名空间和范围，不接受模型指定的 SQL。
type MemoryQuery struct {
	BeforeSequence int64
	Namespace      string
	Platform       string
	Account        string
	Peer           string
	Terms          []string
	Since          int64
	Until          int64
	Limit          int
}

// MemoryCandidates 在机器人和会话范围内使用全文索引取有界候选，正文从事实记录回查。
func (d *Database) MemoryCandidates(ctx context.Context, input *MemoryQuery) ([][]byte, error) {
	if input == nil || input.Limit < 1 || input.Limit > 60 {
		return nil, fmt.Errorf("invalid memory candidate bound")
	}
	scope := "public"
	if input.Namespace != "data/memory/notes.json" {
		if input.Platform == "" || input.Account == "" || input.Peer == "" {
			return nil, fmt.Errorf("missing private identity")
		}
		scope = stable("private", stable(input.Platform, input.Account, input.Peer))
	}
	query := `SELECT r.payload FROM memories m JOIN legacy_records r ON r.robot_id=m.robot_id AND r.id=m.id JOIN memory_search_documents s ON s.robot_id=m.robot_id AND s.memory_id=m.id`
	terms := []string{}
	seen := map[string]bool{}
	for _, term := range input.Terms {
		for _, token := range strings.Fields(tokenize(term)) {
			if !seen[token] {
				terms = append(terms, `"`+strings.ReplaceAll(token, `"`, `""`)+`"`)
				seen[token] = true
			}
		}
	}
	args := []any{}
	if len(terms) > 0 {
		query += ` JOIN memory_fts ON memory_fts.rowid=s.search_rowid`
	}
	query += ` WHERE m.robot_id=? AND m.scope_id=? AND r.scope_id=? AND r.namespace=? AND m.is_del=0 AND m.storage_state<>3`
	args = append(args, d.RobotID, scope, scope, input.Namespace)
	if len(terms) > 0 {
		query += ` AND memory_fts MATCH ?`
		args = append(args, strings.Join(terms, " OR "))
	}
	if scope != "public" {
		query += ` AND EXISTS(SELECT 1 FROM json_each(r.payload,'$.sources') j WHERE coalesce(json_extract(j.value,'$.withdrawn'),0)=0`
		if input.BeforeSequence > 0 {
			query += ` AND json_extract(j.value,'$.sequence')<?`
			args = append(args, input.BeforeSequence)
		}
		if input.Since > 0 {
			query += ` AND json_extract(j.value,'$.occurredAt')>=?`
			args = append(args, time.UnixMilli(input.Since).UTC().Format(time.RFC3339))
		}
		if input.Until > 0 {
			query += ` AND json_extract(j.value,'$.occurredAt')<?`
			args = append(args, time.UnixMilli(input.Until).UTC().Format(time.RFC3339))
		}
		query += `)`
	} else {
		if input.Since > 0 {
			query += ` AND m.occurred_at>=?`
			args = append(args, input.Since)
		}
		if input.Until > 0 {
			query += ` AND m.occurred_at<?`
			args = append(args, input.Until)
		}
	}
	if len(terms) > 0 {
		query += ` ORDER BY bm25(memory_fts),m.occurred_at DESC,m.id`
	} else {
		query += ` ORDER BY m.occurred_at DESC,m.id`
	}
	query += ` LIMIT ?`
	args = append(args, input.Limit)
	rows, err := d.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := [][]byte{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		result = append(result, raw)
	}
	return result, rows.Err()
}

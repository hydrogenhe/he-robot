package storage

import (
	"context"
	"database/sql"
	"fmt"
	"gpt-robot/model"
	"strings"
	"time"
)

func (d *Database) indexEpisode(ctx context.Context, tx *sql.Tx, id, scope string, input *GrowthInput) error {
	now := input.Now.UnixMilli()
	if input.Reaction != "" {
		reaction := stable("reaction", id)
		if err := exec(ctx, tx, `INSERT INTO memories(robot_id,id,created_at,updated_at,scope_id,kind,title,summary,occurred_at,epistemic_kind) VALUES(?,?,?,?,?,3,'当时生成的感想',?,?,3)`, d.RobotID, reaction, now, now, scope, input.Reaction, parseTime(input.At)); err != nil {
			return err
		}
		if err := exec(ctx, tx, "INSERT INTO memory_links(robot_id,created_at,from_memory_id,to_memory_id,relation_kind) VALUES(?,?,?,?,2)", d.RobotID, now, reaction, id); err != nil {
			return err
		}
	}
	return exec(ctx, tx, `INSERT INTO memory_search_documents(robot_id,id,created_at,updated_at,memory_id,source_version,scope_id,tokenized_text,tokenizer_version,storage_state,fact_state,search_rowid) VALUES(?,?,?,?,?,1,?,?,'bigram-v1',0,0,(SELECT coalesce(max(search_rowid),0)+1 FROM memory_search_documents))`, d.RobotID, id, now, now, id, scope, tokenize(input.Summary))
}

// RecallEpisodes 只允许公开范围和调用方已经核验的当前双人会话，不读取其他私人关系。
func (d *Database) RecallEpisodes(ctx context.Context, input *MemoryQuery) ([]model.RecalledEpisode, error) {
	if input == nil || input.Limit < 1 || input.Limit > 3 {
		return nil, fmt.Errorf("invalid episode bound")
	}
	scopes := []string{"public"}
	if input.Platform != "" && input.Account != "" && input.Peer != "" {
		scopes = append(scopes, stable("private", stable(input.Platform, input.Account, input.Peer)))
	}
	terms := []string{}
	seen := map[string]bool{}
	for _, term := range input.Terms {
		for _, word := range strings.Fields(tokenize(term)) {
			if !seen[word] {
				terms = append(terms, `"`+word+`"`)
				seen[word] = true
			}
		}
	}
	if len(terms) == 0 {
		return []model.RecalledEpisode{}, nil
	}
	query := `SELECT m.id,m.summary,m.occurred_at,m.scope_id,coalesce((SELECT r.summary FROM memory_links l JOIN memories r ON r.robot_id=l.robot_id AND r.id=l.from_memory_id WHERE l.robot_id=m.robot_id AND l.to_memory_id=m.id AND r.kind=3 AND r.is_del=0 LIMIT 1),'') FROM memory_fts JOIN memory_search_documents s ON s.search_rowid=memory_fts.rowid JOIN memories m ON m.robot_id=s.robot_id AND m.id=s.memory_id WHERE m.robot_id=? AND m.kind=1 AND m.is_del=0 AND m.storage_state<>3 AND m.scope_id IN(` + strings.TrimSuffix(strings.Repeat("?,", len(scopes)), ",") + `) AND memory_fts MATCH ?`
	args := []any{d.RobotID}
	for _, scope := range scopes {
		args = append(args, scope)
	}
	args = append(args, strings.Join(terms, " OR "))
	if input.Since > 0 {
		query += " AND m.occurred_at>=?"
		args = append(args, input.Since)
	}
	if input.Until > 0 {
		query += " AND m.occurred_at<?"
		args = append(args, input.Until)
	}
	query += " ORDER BY bm25(memory_fts)*(1+coalesce((SELECT importance_units FROM weight_evaluations w WHERE w.robot_id=m.robot_id AND w.memory_id=m.id ORDER BY evaluation_seq DESC LIMIT 1),0)/10000.0),m.occurred_at DESC LIMIT ?"
	args = append(args, input.Limit)
	rows, err := d.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.RecalledEpisode{}
	for rows.Next() {
		var item model.RecalledEpisode
		var at int64
		var scope string
		if err = rows.Scan(&item.SourceID, &item.Summary, &at, &scope, &item.Reaction); err != nil {
			return nil, err
		}
		item.Private = scope != "public"
		item.OccurredAt = time.UnixMilli(at).UTC().Format(time.RFC3339Nano)
		result = append(result, item)
	}
	return result, rows.Err()
}

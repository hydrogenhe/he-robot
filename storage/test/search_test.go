package storage_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"gpt-robot/model"
	"gpt-robot/storage"
)

func TestIndexedCandidatesBoundedAndLongTailSearch(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, t.TempDir(), "pet")
	notes := []model.Note{}
	for i := 0; i < 75; i++ {
		note := model.Note{ID: fmt.Sprint(i), Kind: "memory", At: "2026-11-02T01:00:00Z", Title: "植物记录", Text: "植物的日常观察", Sources: []string{fmt.Sprint("source", i)}}
		notes = append(notes, note)
	}
	notes[0].Text = strings.Repeat("普通内容", 500) + "蓝鲸迁徙"
	raw, err := json.Marshal(notes)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.WriteDocument(ctx, "data/memory/notes.json", raw); err != nil {
		t.Fatal(err)
	}
	query := storage.MemoryQuery{Namespace: "data/memory/notes.json", Terms: []string{"蓝鲸"}, Limit: 60}
	rows, err := db.MemoryCandidates(ctx, &query)
	if err != nil || len(rows) != 1 || !strings.Contains(string(rows[0]), "蓝鲸迁徙") {
		t.Fatal(len(rows), err)
	}
	query.Terms = []string{"植物"}
	rows, err = db.MemoryCandidates(ctx, &query)
	if err != nil || len(rows) != 60 {
		t.Fatal(len(rows), err)
	}
	query.Namespace = "data/private/memory/bilibili-a-b.json"
	if _, err = db.MemoryCandidates(ctx, &query); err == nil {
		t.Fatal("missing identity allowed")
	}
}

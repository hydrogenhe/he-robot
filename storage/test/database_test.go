package storage_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gpt-robot/model"
	"gpt-robot/storage"
)

func openDB(t *testing.T, root, id string) *storage.Database {
	t.Helper()
	db, err := storage.Open(context.Background(), root, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestIsolationForeignKeysFTSAndSingleWriter(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	a := openDB(t, root, "a")
	b := openDB(t, root, "b")
	note := model.Note{ID: "note1", At: "2026-11-02T01:00:00Z", Kind: "memory", Title: "动物", Text: "小猫在草地上玩耍", Sources: []string{"source1"}}
	raw, _ := json.Marshal([]model.Note{note})
	if err := a.WriteDocument(ctx, "data/memory/notes.json", raw); err != nil {
		t.Fatal(err)
	}
	if _, found, err := b.ReadDocument(ctx, "data/memory/notes.json"); err != nil || found {
		t.Fatalf("cross robot read: %v %v", found, err)
	}
	var hits int
	if err := a.SQL.QueryRow("SELECT count(*) FROM memory_fts WHERE memory_fts MATCH ?", "小猫").Scan(&hits); err != nil || hits != 1 {
		t.Fatalf("Chinese FTS: %d %v", hits, err)
	}
	_, err := a.SQL.Exec("INSERT INTO persons(robot_id,id,created_at,updated_at) VALUES('b','p',1,1)")
	if err == nil {
		t.Fatal("cross-robot foreign key accepted")
	}
	if _, err = os.Stat(filepath.Join(root, "data/memory/notes.json")); !os.IsNotExist(err) {
		t.Fatalf("legacy writer active: %v", err)
	}
}

func TestMigrationBackupIdempotencyAndRestart(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "data/character/persona.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("原始人设"), 0600); err != nil {
		t.Fatal(err)
	}
	db := openDB(t, root, "pet")
	if err := db.ImportLegacy(ctx, root); err != nil {
		t.Fatal(err)
	}
	if err := db.WriteDocument(ctx, "data/character/persona.md", []byte("新的人设")); err != nil {
		t.Fatal(err)
	}
	if err := db.ImportLegacy(ctx, root); err != nil {
		t.Fatal(err)
	}
	raw, found, err := db.ReadDocument(ctx, "data/character/persona.md")
	if err != nil || !found || string(raw) != "新的人设" {
		t.Fatalf("reimport reverted state: %s %v", raw, err)
	}
	backups, _ := filepath.Glob(filepath.Join(root, "data/robots/pet/backups/*/data/character/persona.md"))
	if len(backups) != 1 {
		t.Fatalf("backup missing: %v", backups)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openDB(t, root, "pet")
	raw, _, err = reopened.ReadDocument(ctx, "data/character/persona.md")
	if err != nil || string(raw) != "新的人设" {
		t.Fatalf("restart: %s %v", raw, err)
	}
}

func TestPrivateWithdrawalRemovesSearchText(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, t.TempDir(), "pet")
	state := model.PrivateMemoryState{Platform: "bilibili", Account: "1", Peer: "2", Records: []model.PrivateMemory{{ID: "m", Quote: "秘密测试词", Sources: []model.PrivateMemorySource{{MessageKey: "1", OccurredAt: "2026-11-02T01:00:00Z"}}}}}
	path := "data/private/memory/bilibili-1-2.json"
	raw, _ := json.Marshal(state)
	if err := db.WriteDocument(ctx, path, raw); err != nil {
		t.Fatal(err)
	}
	state.Records[0].Sources[0].Withdrawn = true
	state.Records[0].Quote = ""
	raw, _ = json.Marshal(state)
	if err := db.WriteDocument(ctx, path, raw); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.SQL.QueryRow("SELECT count(*) FROM memory_fts WHERE memory_fts MATCH '秘密'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("withdrawn index: %d %v", count, err)
	}
	rows, err := db.SQL.Query("SELECT coalesce(content_text,'') FROM sources")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var body string
		rows.Scan(&body)
		if strings.Contains(body, "秘密") {
			t.Fatal("source text survived withdrawal")
		}
	}
}

func TestSharedUsageAndReceiptUpdateAreNotDuplicated(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, t.TempDir(), "pet")
	usage := model.TokenUsage{Provider: "fake", Model: "test", RequestID: "r", ThreadID: "t", TurnID: "u", Input: 100, Output: 20, RelatedOperations: []string{"a", "b"}}
	for _, id := range []string{"a", "b", "a"} {
		op := model.Operation{ID: id, At: "2026-11-02T01:00:00Z", Platform: "local", Action: "diary-generate", Source: "web", Status: "succeeded", Usage: []model.TokenUsage{usage}}
		if err := db.AppendOperation(ctx, &op); err != nil {
			t.Fatal(err)
		}
	}
	var count, total int
	if err := db.SQL.QueryRow("SELECT count(*),sum(input_tokens+output_tokens) FROM model_usage_records").Scan(&count, &total); err != nil || count != 1 || total != 120 {
		t.Fatalf("double counted: %d %d %v", count, total, err)
	}
}

func TestGrowthOwnerDailyCapAndIndependentDays(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, t.TempDir(), "pet")
	start, _ := time.Parse(time.RFC3339, "2026-11-02T02:00:00Z")
	for index := 0; index < 3; index++ {
		at := start
		if index == 2 {
			at = at.Add(24 * time.Hour)
		}
		input := storage.GrowthInput{Key: fmt.Sprint(index), Platform: "bilibili", Account: "1", Peer: "2", Visibility: "private", Summary: "主人推荐的动物视频", At: at.Format(time.RFC3339), RelationFactor: 40000, InteractionFactor: 15000, BaseFamiliarity: 100, Importance: 3500, EvidenceKind: 1, Awards: []storage.GrowthAward{{Topic: "动物", Familiarity: 600, Affinity: 300, Public: true}}, Now: at}
		if err := db.ApplyGrowth(ctx, &input); err != nil {
			t.Fatal(err)
		}
		if err := db.ApplyGrowth(ctx, &input); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.GrowthBalances(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("balance: %v %v", rows, err)
	}
	if rows[0].Familiarity != 1600 || rows[0].Affinity != 700 || rows[0].Stage != "like_1" || rows[0].PositiveEpisodes != 3 || rows[0].PositiveDays != 2 {
		t.Fatalf("wrong growth: %+v", rows[0])
	}
}

func TestWithdrawConversationCompensatesWithoutRefundingQuota(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, t.TempDir(), "pet")
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	award := storage.GrowthAward{Topic: "动物", Familiarity: 200, Affinity: 200, Public: true}
	input := storage.GrowthInput{Key: "bilibili:1:private:2:message:8", Platform: "bilibili", Account: "1", Peer: "2", Visibility: "private", Summary: "私密动物经历", At: now.Format(time.RFC3339), RelationFactor: 40000, InteractionFactor: 10000, BaseFamiliarity: 50, Importance: 2500, EvidenceKind: 2, Awards: []storage.GrowthAward{award}, Now: now}
	if err := db.QueueGrowth(ctx, &input); err != nil {
		t.Fatal(err)
	}
	if err := db.FlushGrowth(ctx, now.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	before, err := db.GrowthBalances(ctx)
	if err != nil || len(before) != 1 || before[0].Familiarity != 200 {
		t.Fatal(before, err)
	}
	source := model.PrivateMemorySource{MessageKey: "8", Withdrawn: true}
	memory := model.PrivateMemory{ID: "m", Sources: []model.PrivateMemorySource{source}}
	state := model.PrivateMemoryState{Platform: "bilibili", Account: "1", Peer: "2", Records: []model.PrivateMemory{memory}}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.WriteDocument(ctx, "data/private/memory/bilibili-1-2.json", raw); err != nil {
		t.Fatal(err)
	}
	after, err := db.GrowthBalances(ctx)
	if err != nil || len(after) != 1 || after[0].Familiarity != 0 || after[0].Affinity != 0 {
		t.Fatal(after, err)
	}
	var charged int
	if err = db.SQL.QueryRow("SELECT sum(charged_units) FROM growth_quota_usage").Scan(&charged); err != nil || charged != 800 {
		t.Fatal(charged, err)
	}
	if err = db.QueueGrowth(ctx, &input); err != nil {
		t.Fatal(err)
	}
	if err = db.FlushGrowth(ctx, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	after, err = db.GrowthBalances(ctx)
	if err != nil || after[0].Familiarity != 0 {
		t.Fatal("withdrawal resurrected", after, err)
	}
}

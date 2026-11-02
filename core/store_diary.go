package core

import (
	"context"
	"encoding/json"
	"gpt-robot/model"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Diaries 只读取私有日记目录的最近最多30天，不被公共记忆检索调用。
func (s *Store) Diaries(ctx context.Context, limit int) ([]model.DiaryEntry, error) {
	if limit < 1 || limit > 30 {
		return nil, model.Fail("INPUT", "日记列表最多30天")
	}
	if s.Database != nil {
		rows, err := s.Database.ListDocuments(ctx, "data/private/diary/", limit)
		if err != nil {
			return nil, err
		}
		result := []model.DiaryEntry{}
		for _, raw := range rows {
			var value model.DiaryEntry
			if err = json.Unmarshal(raw, &value); err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		return result, nil
	}
	entries, err := os.ReadDir(filepath.Join(s.Root, "data/private/diary"))
	if os.IsNotExist(err) {
		return []model.DiaryEntry{}, nil
	}
	if err != nil {
		return nil, model.Internal(err)
	}
	names := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		if _, err := time.Parse("2006-01-02", strings.TrimSuffix(name, ".json")); err == nil {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	slices.Reverse(names)
	result := []model.DiaryEntry{}
	for _, name := range names[:min(limit, len(names))] {
		value := model.DiaryEntry{}
		if _, err := s.Read(ctx, "data/private/diary/"+name, &value); err != nil {
			return nil, err
		}
		if value.Date != strings.TrimSuffix(name, ".json") {
			return nil, model.Fail("DATA", "日记文件日期与内容不一致")
		}
		result = append(result, value)
	}
	return result, nil
}

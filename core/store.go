package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"gpt-robot/model"
	"gpt-robot/storage"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

type StoreInterface interface {
	InitializeDatabase(context.Context) error
	Diaries(context.Context, int) ([]model.DiaryEntry, error)
	AppendOperation(context.Context, *model.Operation) error
	Operations(context.Context) ([]model.Operation, error)
	Read(context.Context, string, any) (bool, error)
	Text(context.Context, string) (string, error)
	Write(context.Context, string, any) error
	WriteText(context.Context, string, string) error
	Event(context.Context, string, string, map[string]any) (model.Event, error)
	Events(context.Context, int) ([]model.Event, error)
	Notes(context.Context) ([]model.Note, error)
}
type Store struct {
	Root     string
	Database *storage.Database
}

var _ StoreInterface = (*Store)(nil)
var eventFilePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.jsonl$`)

func NewStore(root string) *Store { s := &Store{Root: root}; return s }
func (s *Store) file(name string) (string, error) {
	p := filepath.Join(s.Root, filepath.Clean(name))
	rel, err := filepath.Rel(s.Root, p)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(name) {
		return "", model.Fail("PATH", "无效数据路径")
	}
	return p, nil
}
func (s *Store) Read(ctx context.Context, name string, out any) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	p, err := s.file(name)
	if err != nil {
		return false, err
	}
	var b []byte
	if s.Database != nil && storage.ManagedDocument(name) {
		var found bool
		b, found, err = s.Database.ReadDocument(ctx, name)
		if err != nil || !found {
			return found, err
		}
	} else {
		b, err = os.ReadFile(p)
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, model.Internal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err = dec.Decode(out); err != nil {
		return false, model.Fail("DATA", "本地数据无法解析，未覆盖原文件")
	}
	if err = dec.Decode(new(any)); err != io.EOF {
		return false, model.Fail("DATA", "本地数据存在多余内容，未覆盖原文件")
	}
	return true, nil
}
func (s *Store) Text(ctx context.Context, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.Database != nil && storage.ManagedDocument(name) {
		b, _, err := s.Database.ReadDocument(ctx, name)
		return string(b), err
	}
	p, err := s.file(name)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", model.Internal(err)
	}
	return string(b), nil
}
func (s *Store) Write(ctx context.Context, name string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return model.Internal(err)
	}
	return s.WriteText(ctx, name, string(b)+"\n")
}
func (s *Store) WriteText(ctx context.Context, name, text string) error {
	if s.Database != nil && storage.ManagedDocument(name) {
		return s.Database.WriteDocument(ctx, name, []byte(text))
	}
	p, err := s.file(name)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return s.writeError(name, err)
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".robot-*.tmp")
	if err != nil {
		return s.writeError(name, err)
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.WriteString(text)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), p)
	}
	if err == nil {
		err = os.Chmod(p, 0600)
	}
	if err != nil {
		return s.writeError(name, err)
	}
	return nil
}
func (s *Store) writeError(name string, err error) error {
	slog.Error("本地资料写入失败", "file", name, "error", err)
	return model.Internal(err)
}
func (s *Store) Event(ctx context.Context, kind, summary string, data map[string]any) (model.Event, error) {
	if data == nil {
		data = map[string]any{}
	}
	e := model.Event{ID: model.ID(), At: time.Now().UTC().Format(time.RFC3339Nano), Kind: kind, Summary: summary, Data: data}
	if s.Database != nil {
		return e, s.Database.AppendEvent(ctx, &e)
	}
	p, err := s.file("data/events/" + e.At[:10] + ".jsonl")
	if err != nil {
		return e, err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return e, s.writeError("events", err)
	}
	b, err := json.Marshal(e)
	if err != nil {
		return e, model.Internal(err)
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return e, s.writeError("events", err)
	}
	_, err = f.Write(append(b, '\n'))
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return e, s.writeError("events", err)
	}
	return e, nil
}
func (s *Store) Events(ctx context.Context, limit int) ([]model.Event, error) {
	if s.Database != nil {
		return s.Database.Events(ctx, limit)
	}
	dir, err := s.file("data/events")
	if err != nil {
		return nil, err
	}
	files, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []model.Event{}, nil
	}
	if err != nil {
		return nil, model.Internal(err)
	}
	names := []string{}
	for _, f := range files {
		if eventFilePattern.MatchString(f.Name()) {
			names = append(names, f.Name())
		}
	}
	slices.Sort(names)
	slices.Reverse(names)
	rows := []model.Event{}
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		b, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			return nil, model.Internal(readErr)
		}
		lines := bytes.Split(b, []byte{'\n'})
		for i := len(lines) - 1; i >= 0; i-- {
			if len(bytes.TrimSpace(lines[i])) == 0 {
				continue
			}
			var e model.Event
			dec := json.NewDecoder(bufio.NewReader(bytes.NewReader(lines[i])))
			dec.UseNumber()
			if err = dec.Decode(&e); err != nil {
				return nil, model.Fail("DATA", "经历日志损坏，请检查 "+name)
			}
			rows = append(rows, e)
			if limit > 0 && len(rows) >= limit {
				slices.Reverse(rows)
				return rows, nil
			}
		}
	}
	slices.Reverse(rows)
	return rows, nil
}
func (s *Store) Notes(ctx context.Context) ([]model.Note, error) {
	notes := []model.Note{}
	_, err := s.Read(ctx, "data/memory/notes.json", &notes)
	return notes, err
}

package core

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"gpt-robot/model"
	"os"
	"path/filepath"
)

// AppendOperation 将状态变更顺序追加；高频路径不重读历史、不重写整份账本。
func (s *Store) AppendOperation(ctx context.Context, op *model.Operation) error {
	if s.Database != nil {
		return s.Database.AppendOperation(ctx, op)
	}
	if op == nil {
		return model.Fail("INPUT", "缺少业务流水记录")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p, err := s.file("data/usage/operations.jsonl")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return s.writeError("usage", err)
	}
	b, err := json.Marshal(op)
	if err != nil {
		return model.Internal(err)
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return s.writeError("usage", err)
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
		return s.writeError("usage", err)
	}
	return nil
}

// Operations 仅启动加载；遇到损坏记录停止启动，避免覆盖事实。
func (s *Store) Operations(ctx context.Context) ([]model.Operation, error) {
	if s.Database != nil {
		return s.Database.Operations(ctx)
	}
	p, err := s.file("data/usage/operations.jsonl")
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if errors.Is(err, os.ErrNotExist) {
		return []model.Operation{}, nil
	}
	if err != nil {
		return nil, model.Internal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, model.Internal(err)
	}
	if info.Size() > 0 {
		var last [1]byte
		if _, err := f.ReadAt(last[:], info.Size()-1); err != nil {
			return nil, model.Internal(err)
		}
		if last[0] != '\n' {
			return nil, model.Fail("DATA", "用量账本尾行未完整提交，未覆盖原文件")
		}
	}
	items := []model.Operation{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var op model.Operation
		if err := json.Unmarshal(scanner.Bytes(), &op); err != nil {
			return nil, model.Fail("DATA", "用量账本无法解析，未覆盖原文件")
		}
		items = append(items, op)
	}
	return items, scanner.Err()
}

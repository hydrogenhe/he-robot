package core

import (
	"context"
	"gpt-robot/model"
	"gpt-robot/storage"
)

// InitializeDatabase 创建独立身份、备份并迁移旧业务资料；失败不启用旧写入端。
func (s *Store) InitializeDatabase(ctx context.Context) error {
	if s.Database != nil {
		return nil
	}
	var identity struct {
		RobotID string `json:"robotId"`
	}
	found, err := s.Read(ctx, ".private/robot-identity.json", &identity)
	if err != nil {
		return err
	}
	if !found {
		identity.RobotID = model.ID()
		if err = s.Write(ctx, ".private/robot-identity.json", &identity); err != nil {
			return err
		}
	}
	db, err := storage.Open(ctx, s.Root, identity.RobotID)
	if err != nil {
		return model.Internal(err)
	}
	if err = db.ImportLegacy(ctx, s.Root); err != nil {
		db.Close()
		return model.Internal(err)
	}
	s.Database = db
	return nil
}

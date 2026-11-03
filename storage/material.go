package storage

import "context"

// HasMaterial 通过已理解材料的隔离标识防止浏览窗口淘汰后重复阅读；标题接触不命中此标识。
func (d *Database) HasMaterial(ctx context.Context, platform, key string) (bool, error) {
	var exists bool
	err := d.SQL.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM materials WHERE robot_id=? AND platform=? AND external_object_id=? AND is_del=0)", d.RobotID, platform, key).Scan(&exists)
	return exists, err
}

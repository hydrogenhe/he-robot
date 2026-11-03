package model

// RobotEntry 的编号是工作台展示身份，数据库仍使用不可混用的独立UUID。
type RobotEntry struct{ ID, Name, CreatedAt, DeletedAt string }
type RobotView struct{ ID, Name, CreatedAt, DeletedAt, Status string }
type RobotManagerInput struct{ ID, Name, ConfirmID string }

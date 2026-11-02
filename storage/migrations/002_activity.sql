-- 管理历史筛选和现有执行状态的规范化承接。
ALTER TABLE business_operations ADD COLUMN platform_code TEXT NOT NULL DEFAULT 'local';
ALTER TABLE business_operations ADD COLUMN account_external TEXT NOT NULL DEFAULT '';
ALTER TABLE business_operations ADD COLUMN status_code TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE business_operations ADD COLUMN reference_id TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_operations_platform_time ON business_operations(robot_id,platform_code,started_at,id);
CREATE INDEX idx_operations_account_time ON business_operations(robot_id,account_external,started_at,id);
CREATE INDEX idx_operations_reference ON business_operations(robot_id,reference_id);
CREATE TABLE execution_records (
 robot_id TEXT NOT NULL, id TEXT NOT NULL, namespace TEXT NOT NULL,
 external_id TEXT NOT NULL, kind TEXT NOT NULL, status TEXT NOT NULL,
 occurred_at INTEGER NOT NULL, scope_id TEXT NOT NULL,
 body TEXT NOT NULL, target TEXT NOT NULL DEFAULT '',
 group_id TEXT, part INTEGER, message_key TEXT, payload TEXT NOT NULL CHECK(json_valid(payload)),
 PRIMARY KEY(robot_id,id), UNIQUE(robot_id,namespace,external_id),
 FOREIGN KEY(robot_id,scope_id) REFERENCES access_scopes(robot_id,id)
) STRICT;
CREATE INDEX idx_execution_group ON execution_records(robot_id,group_id,part);
CREATE INDEX idx_execution_namespace_time ON execution_records(robot_id,namespace,occurred_at,id);
CREATE TABLE collection_states (
 robot_id TEXT NOT NULL,id TEXT NOT NULL,kind TEXT NOT NULL,
 generation TEXT NOT NULL DEFAULT '',cursor INTEGER NOT NULL DEFAULT 0,
 since_at INTEGER NOT NULL DEFAULT 0,paused INTEGER NOT NULL DEFAULT 0 CHECK(paused IN(0,1)),
 updated_at INTEGER NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)),
 PRIMARY KEY(robot_id,id),FOREIGN KEY(robot_id) REFERENCES robots(robot_id)
) STRICT;

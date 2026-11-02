CREATE TABLE growth_windows (
 robot_id TEXT NOT NULL,id TEXT NOT NULL,scope_key TEXT NOT NULL,
 started_at INTEGER NOT NULL,last_activity_at INTEGER NOT NULL,seal_due_at INTEGER NOT NULL,
 status INTEGER NOT NULL DEFAULT 0 CHECK(status IN(0,1,2)),
 input_json TEXT NOT NULL CHECK(json_valid(input_json)),
 PRIMARY KEY(robot_id,id),FOREIGN KEY(robot_id) REFERENCES robots(robot_id)
) STRICT;
CREATE INDEX idx_growth_due ON growth_windows(robot_id,status,seal_due_at);
CREATE INDEX idx_growth_scope ON growth_windows(robot_id,scope_key,status,last_activity_at);
CREATE TABLE growth_observations (
 robot_id TEXT NOT NULL,source_key TEXT NOT NULL,window_id TEXT NOT NULL,
 summary TEXT NOT NULL,occurred_at INTEGER NOT NULL,
 PRIMARY KEY(robot_id,source_key),
 FOREIGN KEY(robot_id,window_id) REFERENCES growth_windows(robot_id,id)
) STRICT;

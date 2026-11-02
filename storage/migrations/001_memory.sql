-- 每机器人独立库；所有业务外键包含机器人归属。

PRAGMA foreign_keys=ON;

BEGIN IMMEDIATE;

CREATE TABLE robots (
  "singleton" INTEGER NOT NULL DEFAULT 1,
  "robot_id" TEXT NOT NULL,
  "display_name" TEXT NOT NULL,
  "core_persona" TEXT NOT NULL,
  "core_version" INTEGER NOT NULL DEFAULT 1,
  "active_rule_version_id" TEXT,
  "scoring_timezone" TEXT NOT NULL DEFAULT 'Asia/Shanghai',
  "pending_timezone" TEXT,
  "auth_epoch" INTEGER NOT NULL DEFAULT 1,
  "status" INTEGER NOT NULL DEFAULT 0,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  PRIMARY KEY(singleton),
  UNIQUE(robot_id),
  FOREIGN KEY(robot_id,"active_rule_version_id") REFERENCES rule_versions(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  CHECK(singleton=1),
  CHECK(core_version>=1),
  CHECK(auth_epoch>=1)
) STRICT;

CREATE INDEX idx_robots_active_rule_version_id ON robots(robot_id,"active_rule_version_id");

CREATE TABLE platform_accounts (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "platform" TEXT NOT NULL,
  "platform_uid" TEXT NOT NULL,
  "display_name" TEXT,
  "binding_epoch" INTEGER NOT NULL DEFAULT 1,
  "status" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  UNIQUE(robot_id,platform,platform_uid)
) STRICT;

CREATE TABLE persons (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "display_name" TEXT,
  "status" INTEGER NOT NULL DEFAULT 0,
  "is_del" INTEGER NOT NULL DEFAULT 0 CHECK ("is_del" IN (0,1)),
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT
) STRICT;

CREATE TABLE person_accounts (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "person_id" TEXT NOT NULL,
  "platform" TEXT NOT NULL,
  "platform_uid" TEXT NOT NULL,
  "verification_kind" INTEGER NOT NULL,
  "verified_at" INTEGER,
  "evidence_source_id" TEXT,
  "status" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"person_id") REFERENCES persons(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"evidence_source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,platform,platform_uid)
) STRICT;

CREATE INDEX idx_person_accounts_person_id ON person_accounts(robot_id,"person_id");

CREATE INDEX idx_person_accounts_evidence_source_id ON person_accounts(robot_id,"evidence_source_id");

CREATE TABLE conversations (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "platform_account_id" TEXT NOT NULL,
  "external_conversation_id" TEXT NOT NULL,
  "kind" INTEGER NOT NULL,
  "auth_epoch" INTEGER NOT NULL DEFAULT 1,
  "status" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"platform_account_id") REFERENCES platform_accounts(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,platform_account_id,kind,external_conversation_id)
) STRICT;

CREATE INDEX idx_conversations_platform_account_id ON conversations(robot_id,"platform_account_id");

CREATE TABLE conversation_members (
  "robot_id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "conversation_id" TEXT NOT NULL,
  "person_id" TEXT NOT NULL,
  "status" INTEGER NOT NULL DEFAULT 0,
  "joined_at" INTEGER,
  "left_at" INTEGER,
  PRIMARY KEY(robot_id,conversation_id,person_id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"conversation_id") REFERENCES conversations(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"person_id") REFERENCES persons(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX idx_conversation_members_conversation_id ON conversation_members(robot_id,"conversation_id");

CREATE INDEX idx_conversation_members_person_id ON conversation_members(robot_id,"person_id");

CREATE TABLE access_scopes (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "kind" INTEGER NOT NULL,
  "conversation_id" TEXT,
  "auth_epoch" INTEGER NOT NULL DEFAULT 1,
  "status" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"conversation_id") REFERENCES conversations(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  CHECK(kind BETWEEN 1 AND 4),
  CHECK(kind<>2 OR conversation_id IS NOT NULL),
  CHECK(kind<>1 OR conversation_id IS NULL)
) STRICT;

CREATE INDEX idx_access_scopes_conversation_id ON access_scopes(robot_id,"conversation_id");

CREATE TABLE scope_grants (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "scope_id" TEXT NOT NULL,
  "person_id" TEXT NOT NULL,
  "conversation_id" TEXT,
  "valid_from" INTEGER NOT NULL,
  "valid_to" INTEGER,
  "evidence_source_id" TEXT,
  "grant_kind" INTEGER NOT NULL,
  "status" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"person_id") REFERENCES persons(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"conversation_id") REFERENCES conversations(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"evidence_source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  CHECK(valid_to IS NULL OR valid_to>valid_from)
) STRICT;

CREATE INDEX idx_scope_grants_scope_id ON scope_grants(robot_id,"scope_id");

CREATE INDEX idx_scope_grants_person_id ON scope_grants(robot_id,"person_id");

CREATE INDEX idx_scope_grants_conversation_id ON scope_grants(robot_id,"conversation_id");

CREATE INDEX idx_scope_grants_evidence_source_id ON scope_grants(robot_id,"evidence_source_id");

CREATE TABLE relationships (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "person_id" TEXT NOT NULL,
  "declared_role" INTEGER NOT NULL DEFAULT 1,
  "role_origin" INTEGER NOT NULL,
  "introduced_by_person_id" TEXT,
  "role_source_id" TEXT,
  "role_confirmed_at" INTEGER,
  "status" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"person_id") REFERENCES persons(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"introduced_by_person_id") REFERENCES persons(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"role_source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,person_id)
) STRICT;

CREATE INDEX idx_relationships_person_id ON relationships(robot_id,"person_id");

CREATE INDEX idx_relationships_introduced_by_person_id ON relationships(robot_id,"introduced_by_person_id");

CREATE INDEX idx_relationships_role_source_id ON relationships(robot_id,"role_source_id");

CREATE TABLE relation_events (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "relationship_id" TEXT NOT NULL,
  "source_id" TEXT,
  "previous_role" INTEGER NOT NULL,
  "next_role" INTEGER NOT NULL,
  "change_kind" INTEGER NOT NULL,
  "actor_person_id" TEXT,
  "occurred_at" INTEGER NOT NULL,
  "reason_code" TEXT NOT NULL,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"relationship_id") REFERENCES relationships(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"actor_person_id") REFERENCES persons(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX idx_relation_events_relationship_id ON relation_events(robot_id,"relationship_id");

CREATE INDEX idx_relation_events_source_id ON relation_events(robot_id,"source_id");

CREATE INDEX idx_relation_events_actor_person_id ON relation_events(robot_id,"actor_person_id");

CREATE TABLE materials (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "scope_id" TEXT NOT NULL,
  "kind" INTEGER NOT NULL,
  "canonical_key" TEXT NOT NULL,
  "platform" TEXT,
  "external_object_id" TEXT,
  "canonical_url" TEXT,
  "title" TEXT,
  "content_version" TEXT,
  "status" INTEGER NOT NULL DEFAULT 0,
  "is_del" INTEGER NOT NULL DEFAULT 0 CHECK ("is_del" IN (0,1)),
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,canonical_key)
) STRICT;

CREATE INDEX idx_materials_scope_id ON materials(robot_id,"scope_id");

CREATE TABLE sources (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "scope_id" TEXT NOT NULL,
  "conversation_id" TEXT,
  "actor_person_id" TEXT,
  "platform_account_id" TEXT,
  "material_id" TEXT,
  "parent_source_id" TEXT,
  "kind" INTEGER NOT NULL,
  "external_object_id" TEXT,
  "external_revision" TEXT,
  "dedupe_key" TEXT NOT NULL,
  "content_text" TEXT,
  "content_hash" TEXT,
  "observed_at" INTEGER,
  "source_timezone" TEXT,
  "result" INTEGER NOT NULL,
  "availability" INTEGER NOT NULL DEFAULT 0,
  "observation_scope" TEXT,
  "is_del" INTEGER NOT NULL DEFAULT 0 CHECK ("is_del" IN (0,1)),
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"conversation_id") REFERENCES conversations(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"actor_person_id") REFERENCES persons(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"platform_account_id") REFERENCES platform_accounts(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"material_id") REFERENCES materials(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"parent_source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,dedupe_key),
  CHECK(kind BETWEEN 1 AND 6),
  CHECK(result BETWEEN 0 AND 4),
  CHECK(availability BETWEEN 0 AND 3),
  CHECK(parent_source_id IS NULL OR parent_source_id<>id),
  CHECK(kind<>2 OR (material_id IS NOT NULL AND observation_scope IS NOT NULL))
) STRICT;

CREATE INDEX idx_sources_scope_id ON sources(robot_id,"scope_id");

CREATE INDEX idx_sources_conversation_id ON sources(robot_id,"conversation_id");

CREATE INDEX idx_sources_actor_person_id ON sources(robot_id,"actor_person_id");

CREATE INDEX idx_sources_platform_account_id ON sources(robot_id,"platform_account_id");

CREATE INDEX idx_sources_material_id ON sources(robot_id,"material_id");

CREATE INDEX idx_sources_parent_source_id ON sources(robot_id,"parent_source_id");

CREATE TABLE source_assets (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "source_id" TEXT NOT NULL,
  "asset_kind" INTEGER NOT NULL,
  "relative_path" TEXT NOT NULL,
  "content_hash" TEXT NOT NULL,
  "mime_type" TEXT NOT NULL,
  "size_bytes" INTEGER NOT NULL,
  "availability" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,relative_path),
  CHECK(size_bytes>=0),
  CHECK(relative_path NOT LIKE '/%'),
  CHECK(relative_path NOT LIKE '%../%')
) STRICT;

CREATE INDEX idx_source_assets_source_id ON source_assets(robot_id,"source_id");

CREATE TABLE memories (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "scope_id" TEXT NOT NULL,
  "kind" INTEGER NOT NULL,
  "subject_person_id" TEXT,
  "episode_key" TEXT,
  "title" TEXT,
  "summary" TEXT NOT NULL,
  "occurred_at" INTEGER,
  "valid_from" INTEGER,
  "valid_to" INTEGER,
  "confidence_bp" INTEGER,
  "fact_state" INTEGER NOT NULL DEFAULT 0,
  "storage_state" INTEGER NOT NULL DEFAULT 0,
  "archived_at" INTEGER,
  "archive_reason" TEXT,
  "task_status" INTEGER,
  "due_at" INTEGER,
  "extractor_version" TEXT,
  "is_del" INTEGER NOT NULL DEFAULT 0 CHECK ("is_del" IN (0,1)),
  "episode_status" INTEGER,
  "last_activity_at" INTEGER,
  "seal_due_at" INTEGER,
  "epistemic_kind" INTEGER NOT NULL DEFAULT 1,
  "verification_state" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"subject_person_id") REFERENCES persons(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  CHECK(kind BETWEEN 1 AND 4),
  CHECK(fact_state BETWEEN 0 AND 4),
  CHECK(storage_state BETWEEN 0 AND 3),
  CHECK((kind=1 AND episode_key IS NOT NULL AND episode_status BETWEEN 0 AND 3) OR (kind<>1 AND episode_key IS NULL AND episode_status IS NULL)),
  CHECK((kind=2 AND fact_state BETWEEN 1 AND 4) OR (kind<>2 AND fact_state=0)),
  CHECK((kind=4 AND task_status BETWEEN 0 AND 2) OR (kind<>4 AND task_status IS NULL)),
  CHECK(valid_to IS NULL OR valid_to>valid_from),
  CHECK(length(summary)<=2000)
) STRICT;

CREATE INDEX idx_memories_scope_id ON memories(robot_id,"scope_id");

CREATE INDEX idx_memories_subject_person_id ON memories(robot_id,"subject_person_id");

CREATE TABLE memory_sources (
  "robot_id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "memory_id" TEXT NOT NULL,
  "source_id" TEXT NOT NULL,
  "evidence_role" INTEGER NOT NULL,
  "locator" TEXT,
  PRIMARY KEY(robot_id,memory_id,source_id,evidence_role),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"memory_id") REFERENCES memories(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX idx_memory_sources_memory_id ON memory_sources(robot_id,"memory_id");

CREATE INDEX idx_memory_sources_source_id ON memory_sources(robot_id,"source_id");

CREATE TABLE memory_links (
  "robot_id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "from_memory_id" TEXT NOT NULL,
  "to_memory_id" TEXT NOT NULL,
  "relation_kind" INTEGER NOT NULL,
  PRIMARY KEY(robot_id,from_memory_id,to_memory_id,relation_kind),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"from_memory_id") REFERENCES memories(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"to_memory_id") REFERENCES memories(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  CHECK(from_memory_id<>to_memory_id),
  CHECK(relation_kind BETWEEN 1 AND 5)
) STRICT;

CREATE INDEX idx_memory_links_from_memory_id ON memory_links(robot_id,"from_memory_id");

CREATE INDEX idx_memory_links_to_memory_id ON memory_links(robot_id,"to_memory_id");

CREATE TABLE memory_protections (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "memory_id" TEXT NOT NULL,
  "protection_kind" INTEGER NOT NULL,
  "source_id" TEXT,
  "reason_code" TEXT NOT NULL,
  "valid_from" INTEGER NOT NULL,
  "valid_to" INTEGER,
  "status" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"memory_id") REFERENCES memories(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX idx_memory_protections_memory_id ON memory_protections(robot_id,"memory_id");

CREATE INDEX idx_memory_protections_source_id ON memory_protections(robot_id,"source_id");

CREATE TABLE topics (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "scope_id" TEXT NOT NULL,
  "key" TEXT NOT NULL,
  "label" TEXT NOT NULL,
  "parent_topic_id" TEXT,
  "public_eligible" INTEGER NOT NULL DEFAULT 0 CHECK ("public_eligible" IN (0,1)),
  "status" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"parent_topic_id") REFERENCES topics(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,key),
  CHECK(parent_topic_id IS NULL OR parent_topic_id<>id)
) STRICT;

CREATE INDEX idx_topics_scope_id ON topics(robot_id,"scope_id");

CREATE INDEX idx_topics_parent_topic_id ON topics(robot_id,"parent_topic_id");

CREATE TABLE memory_topics (
  "robot_id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "memory_id" TEXT NOT NULL,
  "topic_id" TEXT NOT NULL,
  "confidence_bp" INTEGER,
  "allocation_bp" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(robot_id,memory_id,topic_id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"memory_id") REFERENCES memories(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"topic_id") REFERENCES topics(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX idx_memory_topics_memory_id ON memory_topics(robot_id,"memory_id");

CREATE INDEX idx_memory_topics_topic_id ON memory_topics(robot_id,"topic_id");

CREATE TABLE rule_versions (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "version_no" INTEGER NOT NULL,
  "version_key" TEXT NOT NULL,
  "schema_version" INTEGER NOT NULL,
  "config_json" TEXT NOT NULL CHECK ("config_json" IS NULL OR json_valid("config_json")),
  "config_hash" TEXT NOT NULL,
  "created_by_kind" INTEGER NOT NULL,
  "status" INTEGER NOT NULL DEFAULT 0,
  "published_at" INTEGER,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  UNIQUE(robot_id,version_no),
  UNIQUE(robot_id,version_key)
) STRICT;

CREATE TABLE scoring_periods (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "sequence_no" INTEGER NOT NULL,
  "local_day" TEXT NOT NULL,
  "timezone" TEXT NOT NULL,
  "starts_at" INTEGER NOT NULL,
  "ends_at" INTEGER NOT NULL,
  "random_bonus_used" INTEGER NOT NULL DEFAULT 0,
  "status" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  UNIQUE(robot_id,sequence_no),
  CHECK(starts_at<ends_at),
  CHECK(random_bonus_used BETWEEN 0 AND 1),
  CHECK(status BETWEEN 0 AND 1)
) STRICT;

CREATE TABLE episode_modifiers (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "episode_id" TEXT NOT NULL,
  "rule_version_id" TEXT NOT NULL,
  "period_id" TEXT NOT NULL,
  "origin_kind" INTEGER NOT NULL,
  "source_relation_id" TEXT,
  "role_snapshot" INTEGER,
  "relationship_factor_bp" INTEGER NOT NULL,
  "interaction_factor_bp" INTEGER NOT NULL,
  "repeat_discount_bp" INTEGER NOT NULL,
  "novelty_bp" INTEGER NOT NULL,
  "resonance_bp" INTEGER NOT NULL,
  "effort_bp" INTEGER NOT NULL,
  "random_eligible" INTEGER NOT NULL CHECK ("random_eligible" IN (0,1)),
  "random_draw" INTEGER,
  "random_bonus_units" INTEGER NOT NULL DEFAULT 0,
  "random_activated" INTEGER NOT NULL DEFAULT 0 CHECK ("random_activated" IN (0,1)),
  "random_skip_reason" TEXT,
  "draw_algorithm_version" TEXT NOT NULL,
  "evaluated_at" INTEGER NOT NULL,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"episode_id") REFERENCES memories(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"rule_version_id") REFERENCES rule_versions(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"period_id") REFERENCES scoring_periods(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"source_relation_id") REFERENCES relationships(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,episode_id),
  CHECK(random_draw IS NULL OR random_draw BETWEEN 0 AND 9999),
  CHECK(random_bonus_units BETWEEN 0 AND 1000),
  CHECK(random_bonus_units%100=0)
) STRICT;

CREATE INDEX idx_episode_modifiers_episode_id ON episode_modifiers(robot_id,"episode_id");

CREATE INDEX idx_episode_modifiers_rule_version_id ON episode_modifiers(robot_id,"rule_version_id");

CREATE INDEX idx_episode_modifiers_period_id ON episode_modifiers(robot_id,"period_id");

CREATE INDEX idx_episode_modifiers_source_relation_id ON episode_modifiers(robot_id,"source_relation_id");

CREATE TABLE weight_evaluations (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "memory_id" TEXT NOT NULL,
  "evaluation_seq" INTEGER NOT NULL,
  "episode_modifier_id" TEXT,
  "rule_version_id" TEXT NOT NULL,
  "relationship_units" INTEGER NOT NULL,
  "event_units" INTEGER NOT NULL,
  "novelty_units" INTEGER NOT NULL,
  "resonance_units" INTEGER NOT NULL,
  "effort_units" INTEGER NOT NULL,
  "random_bonus_units" INTEGER NOT NULL DEFAULT 0,
  "importance_units" INTEGER NOT NULL,
  "reason_codes_json" TEXT NOT NULL CHECK ("reason_codes_json" IS NULL OR json_valid("reason_codes_json")),
  "effective_at" INTEGER NOT NULL,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"memory_id") REFERENCES memories(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"episode_modifier_id") REFERENCES episode_modifiers(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"rule_version_id") REFERENCES rule_versions(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,memory_id,evaluation_seq),
  CHECK(importance_units=min(10000,relationship_units+event_units+novelty_units+resonance_units+effort_units+random_bonus_units)),
  CHECK(importance_units BETWEEN 0 AND 10000)
) STRICT;

CREATE INDEX idx_weight_evaluations_memory_id ON weight_evaluations(robot_id,"memory_id");

CREATE INDEX idx_weight_evaluations_episode_modifier_id ON weight_evaluations(robot_id,"episode_modifier_id");

CREATE INDEX idx_weight_evaluations_rule_version_id ON weight_evaluations(robot_id,"rule_version_id");

CREATE TABLE growth_entries (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "episode_id" TEXT,
  "source_memory_id" TEXT,
  "episode_modifier_id" TEXT,
  "scope_id" TEXT NOT NULL,
  "dimension" INTEGER NOT NULL,
  "topic_id" TEXT,
  "person_id" TEXT,
  "rule_version_id" TEXT NOT NULL,
  "period_id" TEXT,
  "operation_kind" INTEGER NOT NULL,
  "reverses_entry_id" TEXT,
  "idempotency_key" TEXT NOT NULL,
  "base_units" INTEGER NOT NULL,
  "signal_direction" INTEGER NOT NULL DEFAULT 0,
  "relationship_factor_bp" INTEGER NOT NULL,
  "interaction_factor_bp" INTEGER NOT NULL,
  "allocation_bp" INTEGER NOT NULL,
  "repeat_discount_bp" INTEGER NOT NULL,
  "uncapped_delta" INTEGER NOT NULL,
  "applied_delta" INTEGER NOT NULL,
  "evidence_kind" INTEGER NOT NULL,
  "occurred_at" INTEGER NOT NULL,
  "audit_state" INTEGER NOT NULL DEFAULT 0,
  "explanation_json" TEXT CHECK ("explanation_json" IS NULL OR json_valid("explanation_json")),
  "settlement_revision" INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"episode_id") REFERENCES memories(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"source_memory_id") REFERENCES memories(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"episode_modifier_id") REFERENCES episode_modifiers(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"topic_id") REFERENCES topics(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"person_id") REFERENCES persons(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"rule_version_id") REFERENCES rule_versions(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"period_id") REFERENCES scoring_periods(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"reverses_entry_id") REFERENCES growth_entries(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,idempotency_key),
  CHECK(dimension BETWEEN 1 AND 4),
  CHECK((dimension IN(1,2) AND topic_id IS NOT NULL AND person_id IS NULL) OR (dimension IN(3,4) AND person_id IS NOT NULL AND topic_id IS NULL)),
  CHECK(operation_kind BETWEEN 1 AND 3),
  CHECK(signal_direction BETWEEN -1 AND 1),
  CHECK(settlement_revision>=1),
  CHECK(operation_kind<>2 OR reverses_entry_id IS NOT NULL),
  CHECK(operation_kind<>1 OR audit_state<>0 OR (episode_id IS NOT NULL AND source_memory_id IS NOT NULL AND episode_modifier_id IS NOT NULL AND period_id IS NOT NULL)),
  CHECK(operation_kind<>1 OR dimension NOT IN(1,3) OR applied_delta>=0)
) STRICT;

CREATE INDEX idx_growth_entries_episode_id ON growth_entries(robot_id,"episode_id");

CREATE INDEX idx_growth_entries_source_memory_id ON growth_entries(robot_id,"source_memory_id");

CREATE INDEX idx_growth_entries_episode_modifier_id ON growth_entries(robot_id,"episode_modifier_id");

CREATE INDEX idx_growth_entries_scope_id ON growth_entries(robot_id,"scope_id");

CREATE INDEX idx_growth_entries_topic_id ON growth_entries(robot_id,"topic_id");

CREATE INDEX idx_growth_entries_person_id ON growth_entries(robot_id,"person_id");

CREATE INDEX idx_growth_entries_rule_version_id ON growth_entries(robot_id,"rule_version_id");

CREATE INDEX idx_growth_entries_period_id ON growth_entries(robot_id,"period_id");

CREATE INDEX idx_growth_entries_reverses_entry_id ON growth_entries(robot_id,"reverses_entry_id");

CREATE TABLE growth_quota_usage (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "period_id" TEXT NOT NULL,
  "entry_id" TEXT,
  "bucket_key" TEXT NOT NULL,
  "charged_units" INTEGER NOT NULL,
  "anonymized" INTEGER NOT NULL DEFAULT 0 CHECK ("anonymized" IN (0,1)),
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"period_id") REFERENCES scoring_periods(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"entry_id") REFERENCES growth_entries(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,period_id,entry_id,bucket_key),
  CHECK(charged_units>=0),
  CHECK(entry_id IS NOT NULL OR anonymized=1)
) STRICT;

CREATE INDEX idx_growth_quota_usage_period_id ON growth_quota_usage(robot_id,"period_id");

CREATE INDEX idx_growth_quota_usage_entry_id ON growth_quota_usage(robot_id,"entry_id");

CREATE TABLE growth_state (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "dimension" INTEGER NOT NULL,
  "topic_id" TEXT,
  "person_id" TEXT,
  "raw_total_units" INTEGER NOT NULL DEFAULT 0,
  "total_units" INTEGER NOT NULL DEFAULT 0,
  "positive_episode_count" INTEGER NOT NULL DEFAULT 0,
  "negative_episode_count" INTEGER NOT NULL DEFAULT 0,
  "positive_day_count" INTEGER NOT NULL DEFAULT 0,
  "negative_day_count" INTEGER NOT NULL DEFAULT 0,
  "interaction_day_count" INTEGER NOT NULL DEFAULT 0,
  "stage_key" TEXT NOT NULL,
  "stage_rule_version_id" TEXT NOT NULL,
  "last_entry_id" TEXT,
  "state_version" INTEGER NOT NULL DEFAULT 1,
  "last_stage_changed_at" INTEGER,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"topic_id") REFERENCES topics(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"person_id") REFERENCES persons(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"stage_rule_version_id") REFERENCES rule_versions(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"last_entry_id") REFERENCES growth_entries(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  CHECK(dimension BETWEEN 1 AND 4),
  CHECK((dimension IN(1,2) AND topic_id IS NOT NULL AND person_id IS NULL) OR (dimension IN(3,4) AND person_id IS NOT NULL AND topic_id IS NULL)),
  CHECK(dimension NOT IN(2,4) OR total_units BETWEEN -10000 AND 10000)
) STRICT;

CREATE INDEX idx_growth_state_topic_id ON growth_state(robot_id,"topic_id");

CREATE INDEX idx_growth_state_person_id ON growth_state(robot_id,"person_id");

CREATE INDEX idx_growth_state_stage_rule_version_id ON growth_state(robot_id,"stage_rule_version_id");

CREATE INDEX idx_growth_state_last_entry_id ON growth_state(robot_id,"last_entry_id");

CREATE TABLE growth_stage_events (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "growth_state_id" TEXT NOT NULL,
  "state_version" INTEGER NOT NULL,
  "previous_stage" TEXT NOT NULL,
  "next_stage" TEXT NOT NULL,
  "rule_version_id" TEXT NOT NULL,
  "trigger_entry_id" TEXT,
  "change_kind" INTEGER NOT NULL,
  "occurred_at" INTEGER NOT NULL,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"growth_state_id") REFERENCES growth_state(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"rule_version_id") REFERENCES rule_versions(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"trigger_entry_id") REFERENCES growth_entries(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,growth_state_id,state_version)
) STRICT;

CREATE INDEX idx_growth_stage_events_growth_state_id ON growth_stage_events(robot_id,"growth_state_id");

CREATE INDEX idx_growth_stage_events_rule_version_id ON growth_stage_events(robot_id,"rule_version_id");

CREATE INDEX idx_growth_stage_events_trigger_entry_id ON growth_stage_events(robot_id,"trigger_entry_id");

CREATE TABLE dynamic_states (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "scope_id" TEXT NOT NULL,
  "state_key" TEXT NOT NULL,
  "value_units" INTEGER NOT NULL,
  "baseline_units" INTEGER NOT NULL,
  "last_event_at" INTEGER NOT NULL,
  "decay_rule_version_id" TEXT NOT NULL,
  "evidence_memory_id" TEXT,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"decay_rule_version_id") REFERENCES rule_versions(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"evidence_memory_id") REFERENCES memories(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,scope_id,state_key)
) STRICT;

CREATE INDEX idx_dynamic_states_scope_id ON dynamic_states(robot_id,"scope_id");

CREATE INDEX idx_dynamic_states_decay_rule_version_id ON dynamic_states(robot_id,"decay_rule_version_id");

CREATE INDEX idx_dynamic_states_evidence_memory_id ON dynamic_states(robot_id,"evidence_memory_id");

CREATE TABLE public_projections (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "topic_id" TEXT NOT NULL,
  "dimension" INTEGER NOT NULL,
  "public_stage_key" TEXT NOT NULL,
  "projection_policy_version" TEXT NOT NULL,
  "status" INTEGER NOT NULL DEFAULT 0,
  "evaluated_at" INTEGER NOT NULL,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"topic_id") REFERENCES topics(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,topic_id,dimension),
  CHECK(dimension IN(1,2))
) STRICT;

CREATE INDEX idx_public_projections_topic_id ON public_projections(robot_id,"topic_id");

CREATE TABLE projection_inputs (
  "robot_id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "projection_id" TEXT NOT NULL,
  "growth_state_id" TEXT NOT NULL,
  "state_version" INTEGER NOT NULL,
  PRIMARY KEY(robot_id,projection_id,growth_state_id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"projection_id") REFERENCES public_projections(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"growth_state_id") REFERENCES growth_state(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX idx_projection_inputs_projection_id ON projection_inputs(robot_id,"projection_id");

CREATE INDEX idx_projection_inputs_growth_state_id ON projection_inputs(robot_id,"growth_state_id");

CREATE TABLE conversation_states (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "conversation_id" TEXT NOT NULL,
  "scope_id" TEXT NOT NULL,
  "generation" INTEGER NOT NULL DEFAULT 1,
  "topic_summary" TEXT,
  "handoff_json" TEXT CHECK ("handoff_json" IS NULL OR json_valid("handoff_json")),
  "expires_at" INTEGER,
  "core_version" INTEGER NOT NULL,
  "auth_epoch" INTEGER NOT NULL,
  "status" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"conversation_id") REFERENCES conversations(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,conversation_id)
) STRICT;

CREATE INDEX idx_conversation_states_conversation_id ON conversation_states(robot_id,"conversation_id");

CREATE INDEX idx_conversation_states_scope_id ON conversation_states(robot_id,"scope_id");

CREATE TABLE conversation_state_sources (
  "robot_id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "state_id" TEXT NOT NULL,
  "source_id" TEXT NOT NULL,
  PRIMARY KEY(robot_id,state_id,source_id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"state_id") REFERENCES conversation_states(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX idx_conversation_state_sources_state_id ON conversation_state_sources(robot_id,"state_id");

CREATE INDEX idx_conversation_state_sources_source_id ON conversation_state_sources(robot_id,"source_id");

CREATE TABLE retrieval_runs (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "business_request_id" TEXT NOT NULL,
  "conversation_id" TEXT,
  "requester_person_id" TEXT,
  "entrypoint_kind" INTEGER NOT NULL,
  "auth_epoch" INTEGER NOT NULL,
  "query_source_id" TEXT,
  "query_method" INTEGER NOT NULL,
  "candidate_count" INTEGER NOT NULL DEFAULT 0,
  "returned_count" INTEGER NOT NULL DEFAULT 0,
  "returned_bytes" INTEGER NOT NULL DEFAULT 0,
  "latency_ms" INTEGER,
  "status" INTEGER NOT NULL DEFAULT 0,
  "expires_at" INTEGER NOT NULL,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"conversation_id") REFERENCES conversations(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"requester_person_id") REFERENCES persons(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"query_source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  CHECK(candidate_count BETWEEN 0 AND 60),
  CHECK(returned_count BETWEEN 0 AND 6),
  CHECK(returned_bytes BETWEEN 0 AND 2400)
) STRICT;

CREATE INDEX idx_retrieval_runs_conversation_id ON retrieval_runs(robot_id,"conversation_id");

CREATE INDEX idx_retrieval_runs_requester_person_id ON retrieval_runs(robot_id,"requester_person_id");

CREATE INDEX idx_retrieval_runs_query_source_id ON retrieval_runs(robot_id,"query_source_id");

CREATE TABLE retrieval_run_scopes (
  "robot_id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "run_id" TEXT NOT NULL,
  "scope_id" TEXT NOT NULL,
  "scope_auth_epoch" INTEGER NOT NULL,
  PRIMARY KEY(robot_id,run_id,scope_id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"run_id") REFERENCES retrieval_runs(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX idx_retrieval_run_scopes_run_id ON retrieval_run_scopes(robot_id,"run_id");

CREATE INDEX idx_retrieval_run_scopes_scope_id ON retrieval_run_scopes(robot_id,"scope_id");

CREATE TABLE retrieval_items (
  "robot_id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "run_id" TEXT NOT NULL,
  "memory_id" TEXT NOT NULL,
  "memory_version" INTEGER NOT NULL,
  "rank" INTEGER NOT NULL,
  "relevance_bp" INTEGER NOT NULL,
  "importance_bp" INTEGER NOT NULL,
  "recency_bp" INTEGER NOT NULL,
  "selected" INTEGER NOT NULL DEFAULT 0 CHECK ("selected" IN (0,1)),
  "used" INTEGER NOT NULL DEFAULT 0 CHECK ("used" IN (0,1)),
  "reason_codes_json" TEXT NOT NULL CHECK ("reason_codes_json" IS NULL OR json_valid("reason_codes_json")),
  PRIMARY KEY(robot_id,run_id,memory_id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"run_id") REFERENCES retrieval_runs(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"memory_id") REFERENCES memories(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX idx_retrieval_items_run_id ON retrieval_items(robot_id,"run_id");

CREATE INDEX idx_retrieval_items_memory_id ON retrieval_items(robot_id,"memory_id");

CREATE TABLE index_jobs (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "memory_id" TEXT NOT NULL,
  "target_version" INTEGER NOT NULL,
  "index_kind" INTEGER NOT NULL,
  "model_fingerprint" TEXT NOT NULL,
  "operation" INTEGER NOT NULL,
  "status" INTEGER NOT NULL DEFAULT 0,
  "attempts" INTEGER NOT NULL DEFAULT 0,
  "available_at" INTEGER NOT NULL,
  "lease_until" INTEGER,
  "last_error_code" TEXT,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"memory_id") REFERENCES memories(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,memory_id,target_version,index_kind,model_fingerprint)
) STRICT;

CREATE INDEX idx_index_jobs_memory_id ON index_jobs(robot_id,"memory_id");

CREATE TABLE memory_search_documents (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "memory_id" TEXT NOT NULL,
  "source_version" INTEGER NOT NULL,
  "scope_id" TEXT NOT NULL,
  "tokenized_text" TEXT NOT NULL,
  "tokenizer_version" TEXT NOT NULL,
  "storage_state" INTEGER NOT NULL,
  "fact_state" INTEGER NOT NULL,
  "search_rowid" INTEGER NOT NULL UNIQUE,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"memory_id") REFERENCES memories(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,memory_id)
) STRICT;

CREATE INDEX idx_memory_search_documents_memory_id ON memory_search_documents(robot_id,"memory_id");

CREATE INDEX idx_memory_search_documents_scope_id ON memory_search_documents(robot_id,"scope_id");

CREATE TABLE deletion_jobs (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "target_kind" INTEGER NOT NULL,
  "target_id" TEXT NOT NULL,
  "requested_by_kind" INTEGER NOT NULL,
  "requested_by_person_id" TEXT,
  "dedupe_token" TEXT NOT NULL,
  "status" INTEGER NOT NULL DEFAULT 0,
  "phase" INTEGER NOT NULL DEFAULT 0,
  "created_auth_epoch" INTEGER NOT NULL,
  "last_error_code" TEXT,
  "completed_at" INTEGER,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"requested_by_person_id") REFERENCES persons(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,dedupe_token)
) STRICT;

CREATE INDEX idx_deletion_jobs_requested_by_person_id ON deletion_jobs(robot_id,"requested_by_person_id");

CREATE TABLE deletion_marks (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "source_dedupe_hmac" TEXT NOT NULL,
  "deleted_at" INTEGER NOT NULL,
  "reason_code" TEXT NOT NULL,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  UNIQUE(robot_id,source_dedupe_hmac)
) STRICT;

CREATE TABLE schema_migrations (
  "version" INTEGER NOT NULL,
  "checksum" TEXT NOT NULL,
  "applied_at" INTEGER NOT NULL,
  PRIMARY KEY(version),
  CHECK(version>0)
) STRICT;

CREATE TABLE state_documents (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "path" TEXT NOT NULL,
  "body" TEXT NOT NULL,
  "content_hash" TEXT NOT NULL,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  UNIQUE(robot_id,path)
) STRICT;

CREATE TABLE migration_batches (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "legacy_path" TEXT NOT NULL,
  "content_hash" TEXT NOT NULL,
  "status" INTEGER NOT NULL DEFAULT 0,
  "imported_count" INTEGER NOT NULL DEFAULT 0,
  "completed_at" INTEGER,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  UNIQUE(robot_id,legacy_path,content_hash)
) STRICT;

CREATE TABLE migration_mappings (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "batch_id" TEXT NOT NULL,
  "legacy_id" TEXT NOT NULL,
  "target_table" TEXT NOT NULL,
  "target_id" TEXT NOT NULL,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"batch_id") REFERENCES migration_batches(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,batch_id,legacy_id,target_table)
) STRICT;

CREATE INDEX idx_migration_mappings_batch_id ON migration_mappings(robot_id,"batch_id");

CREATE TABLE diaries (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "scope_id" TEXT NOT NULL,
  "local_day" TEXT NOT NULL,
  "timezone" TEXT NOT NULL,
  "current_version_id" TEXT,
  "status" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"current_version_id") REFERENCES diary_versions(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,local_day,timezone)
) STRICT;

CREATE INDEX idx_diaries_scope_id ON diaries(robot_id,"scope_id");

CREATE INDEX idx_diaries_current_version_id ON diaries(robot_id,"current_version_id");

CREATE TABLE diary_versions (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "diary_id" TEXT NOT NULL,
  "version_no" INTEGER NOT NULL,
  "title" TEXT NOT NULL,
  "body" TEXT NOT NULL,
  "operation_id" TEXT,
  "source_date" TEXT NOT NULL,
  "status" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"diary_id") REFERENCES diaries(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"operation_id") REFERENCES business_operations(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,diary_id,version_no)
) STRICT;

CREATE INDEX idx_diary_versions_diary_id ON diary_versions(robot_id,"diary_id");

CREATE INDEX idx_diary_versions_operation_id ON diary_versions(robot_id,"operation_id");

CREATE TABLE diary_attempts (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "diary_id" TEXT NOT NULL,
  "version_id" TEXT,
  "operation_id" TEXT,
  "trigger_kind" INTEGER NOT NULL,
  "status" INTEGER NOT NULL,
  "started_at" INTEGER NOT NULL,
  "finished_at" INTEGER,
  "error_code" TEXT,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"diary_id") REFERENCES diaries(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"version_id") REFERENCES diary_versions(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"operation_id") REFERENCES business_operations(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX idx_diary_attempts_diary_id ON diary_attempts(robot_id,"diary_id");

CREATE INDEX idx_diary_attempts_version_id ON diary_attempts(robot_id,"version_id");

CREATE INDEX idx_diary_attempts_operation_id ON diary_attempts(robot_id,"operation_id");

CREATE TABLE diary_inputs (
  "robot_id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "version_id" TEXT NOT NULL,
  "source_id" TEXT NOT NULL,
  "source_version" INTEGER NOT NULL,
  PRIMARY KEY(robot_id,version_id,source_id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"version_id") REFERENCES diary_versions(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX idx_diary_inputs_version_id ON diary_inputs(robot_id,"version_id");

CREATE INDEX idx_diary_inputs_source_id ON diary_inputs(robot_id,"source_id");

CREATE TABLE state_signals (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "scope_id" TEXT NOT NULL,
  "source_id" TEXT NOT NULL,
  "state_key" TEXT NOT NULL,
  "signal" INTEGER NOT NULL,
  "rule_version_id" TEXT NOT NULL,
  "operation_id" TEXT,
  "occurred_at" INTEGER NOT NULL,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"rule_version_id") REFERENCES rule_versions(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"operation_id") REFERENCES business_operations(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX idx_state_signals_scope_id ON state_signals(robot_id,"scope_id");

CREATE INDEX idx_state_signals_source_id ON state_signals(robot_id,"source_id");

CREATE INDEX idx_state_signals_rule_version_id ON state_signals(robot_id,"rule_version_id");

CREATE INDEX idx_state_signals_operation_id ON state_signals(robot_id,"operation_id");

CREATE TABLE state_changes (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "scope_id" TEXT NOT NULL,
  "state_key" TEXT NOT NULL,
  "previous_units" INTEGER NOT NULL,
  "next_units" INTEGER NOT NULL,
  "source_id" TEXT,
  "operation_id" TEXT,
  "occurred_at" INTEGER NOT NULL,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"operation_id") REFERENCES business_operations(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX idx_state_changes_scope_id ON state_changes(robot_id,"scope_id");

CREATE INDEX idx_state_changes_source_id ON state_changes(robot_id,"source_id");

CREATE INDEX idx_state_changes_operation_id ON state_changes(robot_id,"operation_id");

CREATE TABLE persona_revisions (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "version_no" INTEGER NOT NULL,
  "display_name" TEXT NOT NULL,
  "core_persona" TEXT NOT NULL,
  "scope_id" TEXT NOT NULL,
  "source_id" TEXT,
  "actor_person_id" TEXT,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"actor_person_id") REFERENCES persons(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,version_no)
) STRICT;

CREATE INDEX idx_persona_revisions_scope_id ON persona_revisions(robot_id,"scope_id");

CREATE INDEX idx_persona_revisions_source_id ON persona_revisions(robot_id,"source_id");

CREATE INDEX idx_persona_revisions_actor_person_id ON persona_revisions(robot_id,"actor_person_id");

CREATE TABLE stickers (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "scope_id" TEXT NOT NULL,
  "asset_id" TEXT NOT NULL,
  "label" TEXT NOT NULL,
  "instruction_source_id" TEXT,
  "image_source_id" TEXT NOT NULL,
  "last_used_at" INTEGER,
  "status" INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"asset_id") REFERENCES source_assets(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"instruction_source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"image_source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX idx_stickers_scope_id ON stickers(robot_id,"scope_id");

CREATE INDEX idx_stickers_asset_id ON stickers(robot_id,"asset_id");

CREATE INDEX idx_stickers_instruction_source_id ON stickers(robot_id,"instruction_source_id");

CREATE INDEX idx_stickers_image_source_id ON stickers(robot_id,"image_source_id");

CREATE TABLE sticker_uses (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "sticker_id" TEXT NOT NULL,
  "operation_id" TEXT,
  "used_at" INTEGER NOT NULL,
  "status" INTEGER NOT NULL,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"sticker_id") REFERENCES stickers(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"operation_id") REFERENCES business_operations(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX idx_sticker_uses_sticker_id ON sticker_uses(robot_id,"sticker_id");

CREATE INDEX idx_sticker_uses_operation_id ON sticker_uses(robot_id,"operation_id");

CREATE TABLE topic_aliases (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "topic_id" TEXT NOT NULL,
  "scope_id" TEXT NOT NULL,
  "alias" TEXT NOT NULL,
  "source_id" TEXT,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"topic_id") REFERENCES topics(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,scope_id,alias)
) STRICT;

CREATE INDEX idx_topic_aliases_topic_id ON topic_aliases(robot_id,"topic_id");

CREATE INDEX idx_topic_aliases_scope_id ON topic_aliases(robot_id,"scope_id");

CREATE INDEX idx_topic_aliases_source_id ON topic_aliases(robot_id,"source_id");

CREATE TABLE person_aliases (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "person_id" TEXT NOT NULL,
  "scope_id" TEXT NOT NULL,
  "alias" TEXT NOT NULL,
  "source_id" TEXT,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"person_id") REFERENCES persons(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,scope_id,person_id,alias)
) STRICT;

CREATE INDEX idx_person_aliases_person_id ON person_aliases(robot_id,"person_id");

CREATE INDEX idx_person_aliases_scope_id ON person_aliases(robot_id,"scope_id");

CREATE INDEX idx_person_aliases_source_id ON person_aliases(robot_id,"source_id");

CREATE TABLE business_operations (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "root_operation_id" TEXT,
  "parent_operation_id" TEXT,
  "platform_account_id" TEXT,
  "scope_id" TEXT NOT NULL,
  "action_code" TEXT NOT NULL,
  "origin_kind" INTEGER NOT NULL,
  "started_at" INTEGER NOT NULL,
  "finished_at" INTEGER,
  "status" INTEGER NOT NULL,
  "duration_ms" INTEGER,
  "generation_ms" INTEGER,
  "account_check_ms" INTEGER,
  "send_ms" INTEGER,
  "account_attempts" INTEGER NOT NULL DEFAULT 0,
  "error_code" TEXT,
  "rule_version_id" TEXT,
  "model_call_count" INTEGER,
  "usage_state" INTEGER NOT NULL,
  "idempotency_key" TEXT NOT NULL,
  "legacy_payload" TEXT NOT NULL,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"root_operation_id") REFERENCES business_operations(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"parent_operation_id") REFERENCES business_operations(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"platform_account_id") REFERENCES platform_accounts(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"rule_version_id") REFERENCES rule_versions(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,idempotency_key)
) STRICT;

CREATE INDEX idx_business_operations_root_operation_id ON business_operations(robot_id,"root_operation_id");

CREATE INDEX idx_business_operations_parent_operation_id ON business_operations(robot_id,"parent_operation_id");

CREATE INDEX idx_business_operations_platform_account_id ON business_operations(robot_id,"platform_account_id");

CREATE INDEX idx_business_operations_scope_id ON business_operations(robot_id,"scope_id");

CREATE INDEX idx_business_operations_rule_version_id ON business_operations(robot_id,"rule_version_id");

CREATE TABLE operation_events (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "operation_id" TEXT NOT NULL,
  "sequence_no" INTEGER NOT NULL,
  "occurred_at" INTEGER NOT NULL,
  "event_kind" INTEGER NOT NULL,
  "status" INTEGER NOT NULL,
  "reason_code" TEXT,
  "duration_ms" INTEGER,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"operation_id") REFERENCES business_operations(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,operation_id,sequence_no)
) STRICT;

CREATE INDEX idx_operation_events_operation_id ON operation_events(robot_id,"operation_id");

CREATE TABLE model_usage_records (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "provider_code" TEXT NOT NULL,
  "model_name" TEXT NOT NULL,
  "meter_key" TEXT NOT NULL,
  "meter_scope" INTEGER NOT NULL,
  "provider_request_id" TEXT,
  "provider_thread_id" TEXT,
  "provider_turn_id" TEXT,
  "occurred_at" INTEGER,
  "reported_at" INTEGER NOT NULL,
  "usage_source" INTEGER NOT NULL,
  "input_tokens" INTEGER,
  "output_tokens" INTEGER,
  "cached_input_tokens" INTEGER,
  "reasoning_output_tokens" INTEGER,
  "completeness" INTEGER NOT NULL,
  "receipt_revision" INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  UNIQUE(robot_id,provider_code,meter_key),
  CHECK(input_tokens IS NULL OR input_tokens>=0),
  CHECK(output_tokens IS NULL OR output_tokens>=0),
  CHECK(cached_input_tokens IS NULL OR (cached_input_tokens>=0 AND cached_input_tokens<=input_tokens))
) STRICT;

CREATE TABLE operation_usage_links (
  "robot_id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "operation_id" TEXT NOT NULL,
  "usage_id" TEXT NOT NULL,
  "attribution_kind" INTEGER NOT NULL,
  PRIMARY KEY(robot_id,operation_id,usage_id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"operation_id") REFERENCES business_operations(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"usage_id") REFERENCES model_usage_records(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX idx_operation_usage_links_operation_id ON operation_usage_links(robot_id,"operation_id");

CREATE INDEX idx_operation_usage_links_usage_id ON operation_usage_links(robot_id,"usage_id");

CREATE TABLE operation_source_links (
  "robot_id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "operation_id" TEXT NOT NULL,
  "source_id" TEXT NOT NULL,
  "relation_kind" INTEGER NOT NULL,
  PRIMARY KEY(robot_id,operation_id,source_id,relation_kind),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"operation_id") REFERENCES business_operations(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX idx_operation_source_links_operation_id ON operation_source_links(robot_id,"operation_id");

CREATE INDEX idx_operation_source_links_source_id ON operation_source_links(robot_id,"source_id");

CREATE TABLE activity_events (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "kind" TEXT NOT NULL,
  "summary" TEXT NOT NULL,
  "occurred_at" INTEGER NOT NULL,
  "scope_id" TEXT NOT NULL,
  "source_id" TEXT,
  "operation_id" TEXT,
  "data_json" TEXT NOT NULL CHECK ("data_json" IS NULL OR json_valid("data_json")),
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"source_id") REFERENCES sources(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY(robot_id,"operation_id") REFERENCES business_operations(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
) STRICT;

CREATE INDEX idx_activity_events_scope_id ON activity_events(robot_id,"scope_id");

CREATE INDEX idx_activity_events_source_id ON activity_events(robot_id,"source_id");

CREATE INDEX idx_activity_events_operation_id ON activity_events(robot_id,"operation_id");

CREATE TABLE legacy_records (
  "robot_id" TEXT NOT NULL,
  "id" TEXT NOT NULL,
  "created_at" INTEGER NOT NULL,
  "updated_at" INTEGER NOT NULL,
  "row_version" INTEGER NOT NULL DEFAULT 1 CHECK (row_version>=1),
  "namespace" TEXT NOT NULL,
  "record_key" TEXT NOT NULL,
  "position" INTEGER NOT NULL,
  "payload" TEXT NOT NULL,
  "scope_id" TEXT NOT NULL,
  PRIMARY KEY(robot_id,id),
  FOREIGN KEY(robot_id) REFERENCES robots(robot_id) ON DELETE RESTRICT,
  FOREIGN KEY(robot_id,"scope_id") REFERENCES access_scopes(robot_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE(robot_id,namespace,record_key)
) STRICT;

CREATE INDEX idx_legacy_records_scope_id ON legacy_records(robot_id,"scope_id");

CREATE UNIQUE INDEX uq_one_public ON access_scopes(robot_id) WHERE kind=1 AND status=0;

CREATE UNIQUE INDEX uq_one_owner ON relationships(robot_id) WHERE declared_role=3 AND status=0;

CREATE UNIQUE INDEX uq_one_period ON scoring_periods(robot_id) WHERE status=0;

CREATE UNIQUE INDEX uq_episode_key ON memories(robot_id,episode_key) WHERE episode_key IS NOT NULL;

CREATE UNIQUE INDEX uq_grant_conversation ON scope_grants(robot_id,scope_id,person_id,conversation_id) WHERE status=0 AND conversation_id IS NOT NULL;

CREATE UNIQUE INDEX uq_grant_person ON scope_grants(robot_id,scope_id,person_id) WHERE status=0 AND conversation_id IS NULL;

CREATE UNIQUE INDEX uq_protection ON memory_protections(robot_id,memory_id,protection_kind) WHERE status=0;

CREATE UNIQUE INDEX uq_reversal ON growth_entries(robot_id,reverses_entry_id) WHERE operation_kind=2;

CREATE UNIQUE INDEX uq_topic_award ON growth_entries(robot_id,episode_id,dimension,topic_id,settlement_revision) WHERE operation_kind=1 AND topic_id IS NOT NULL;

CREATE UNIQUE INDEX uq_person_award ON growth_entries(robot_id,episode_id,dimension,person_id,settlement_revision) WHERE operation_kind=1 AND person_id IS NOT NULL;

CREATE UNIQUE INDEX uq_topic_state ON growth_state(robot_id,dimension,topic_id) WHERE topic_id IS NOT NULL;

CREATE UNIQUE INDEX uq_person_state ON growth_state(robot_id,dimension,person_id) WHERE person_id IS NOT NULL;

CREATE INDEX idx_activity_time ON activity_events(robot_id,occurred_at,id);

CREATE INDEX idx_operations_time ON business_operations(robot_id,started_at,id);

CREATE INDEX idx_operations_filter ON business_operations(robot_id,action_code,status,started_at,id);

CREATE INDEX idx_memories_scope_time ON memories(robot_id,scope_id,storage_state,occurred_at,id);

CREATE INDEX idx_quota_bucket ON growth_quota_usage(robot_id,period_id,bucket_key);

CREATE VIRTUAL TABLE memory_fts USING fts5(tokenized_text, content='memory_search_documents', content_rowid='search_rowid');

CREATE TRIGGER search_insert AFTER INSERT ON memory_search_documents BEGIN INSERT INTO memory_fts(rowid,tokenized_text) VALUES(new.search_rowid,new.tokenized_text); END;

CREATE TRIGGER search_delete AFTER DELETE ON memory_search_documents BEGIN INSERT INTO memory_fts(memory_fts,rowid,tokenized_text) VALUES('delete',old.search_rowid,old.tokenized_text); END;

CREATE TRIGGER search_update AFTER UPDATE ON memory_search_documents BEGIN INSERT INTO memory_fts(memory_fts,rowid,tokenized_text) VALUES('delete',old.search_rowid,old.tokenized_text); INSERT INTO memory_fts(rowid,tokenized_text) VALUES(new.search_rowid,new.tokenized_text); END;

COMMIT;

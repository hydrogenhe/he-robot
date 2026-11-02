package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// GrowthAward 是共享业务规则计算后的候选奖励，数值单位为百分之一点。
type GrowthAward struct {
	Parent                string
	Topic                 string
	Familiarity, Affinity int64
	Public                bool
}

// GrowthInput 包含已由业务入口核验的经历和有界信号，模型不提供权限/倍率。
type GrowthInput struct {
	Reaction                                                                 string
	RelationshipUnits, EventUnits, NoveltyUnits, ResonanceUnits, EffortUnits int64
	Key, Platform, Account, Peer, Visibility, Summary, At, MaterialKey       string
	RelationFactor, InteractionFactor, BaseFamiliarity, Importance           int64
	EvidenceKind                                                             int
	Awards                                                                   []GrowthAward
	PersonFamiliarity, PersonAffinity                                        int64
	RandomDraw                                                               *int
	RandomBonus                                                              int64
	Now                                                                      time.Time
}

// GrowthBalance 是内部汇总值；公开角色上下文仅使用白名单粗阶段。
type GrowthBalance struct {
	Topic                                                          string
	Familiarity, Affinity                                          int64
	PositiveEpisodes, PositiveDays, NegativeEpisodes, NegativeDays int
	Stage                                                          string
	Public                                                         bool
}

// ApplyGrowth 在一个事务内保证经历去重、日额度、随机名额、账目与状态共同提交。
func (d *Database) ApplyGrowth(ctx context.Context, input *GrowthInput) error {
	if input == nil || input.Key == "" || input.Now.IsZero() {
		return fmt.Errorf("missing growth evidence")
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if strings.HasPrefix(input.Key, "conversation:") {
		active, checkErr := d.growthWindowActive(ctx, tx, strings.TrimPrefix(input.Key, "conversation:"))
		if checkErr != nil {
			return checkErr
		}
		if !active {
			return nil
		}
	}
	now := input.Now.UnixMilli()
	scope := "public"
	if input.Visibility == "private" {
		scope, err = d.privateScope(ctx, tx, input.Platform, input.Account, input.Peer)
		if err != nil {
			return err
		}
	}
	episode := stable("episode", input.Key)
	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM memories WHERE robot_id=? AND episode_key=?", d.RobotID, input.Key).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	rule := "growth-v0.1"
	config := `{"schemaVersion":1,"unit":100,"topicDailyFamiliarity":1000,"topicDailyAffinity":400,"allDailyFamiliarity":3000,"allDailyAffinity":1200,"stages":[[500,3,2],[2000,8,5],[6000,15,10]]}`
	if err = exec(ctx, tx, `INSERT INTO rule_versions(robot_id,id,created_at,updated_at,version_no,version_key,schema_version,config_json,config_hash,created_by_kind,status,published_at) VALUES(?,?,?,?,1,?,1,?,?,1,1,?) ON CONFLICT DO NOTHING`, d.RobotID, rule, now, now, rule, config, digest([]byte(config)), now); err != nil {
		return err
	}
	if err = exec(ctx, tx, "UPDATE robots SET active_rule_version_id=? WHERE robot_id=?", rule, d.RobotID); err != nil {
		return err
	}
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	local := input.Now.In(zone)
	day := local.Format("2006-01-02")
	period := stable("period", day)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
	if err = exec(ctx, tx, "UPDATE scoring_periods SET status=1 WHERE robot_id=? AND status=0 AND id<>?", d.RobotID, period); err != nil {
		return err
	}
	if err = exec(ctx, tx, `INSERT INTO scoring_periods(robot_id,id,created_at,updated_at,sequence_no,local_day,timezone,starts_at,ends_at) VALUES(?,?,?,?,(SELECT coalesce(max(sequence_no),0)+1 FROM scoring_periods WHERE robot_id=?),?,'Asia/Shanghai',?,?) ON CONFLICT DO NOTHING`, d.RobotID, period, now, now, d.RobotID, day, start.UnixMilli(), start.AddDate(0, 0, 1).UnixMilli()); err != nil {
		return err
	}
	var material any
	if input.MaterialKey != "" {
		mid := stable("material", scope, input.Platform, input.MaterialKey)
		material = mid
		if err = exec(ctx, tx, "INSERT INTO materials(robot_id,id,created_at,updated_at,scope_id,kind,canonical_key,platform,external_object_id) VALUES(?,?,?,?,?,1,?,?,?) ON CONFLICT DO NOTHING", d.RobotID, mid, now, now, scope, mid, input.Platform, input.MaterialKey); err != nil {
			return err
		}
		var previous int64
		if err = tx.QueryRowContext(ctx, "SELECT coalesce(max(observed_at),0) FROM sources WHERE robot_id=? AND material_id=? AND result IN(1,2)", d.RobotID, mid).Scan(&previous); err != nil {
			return err
		}
		if previous > 0 {
			copyInput := *input
			copyInput.Awards = append([]GrowthAward(nil), input.Awards...)
			for i := range copyInput.Awards {
				copyInput.Awards[i].Familiarity = 0
				copyInput.Awards[i].Affinity = 0
			}
			input = &copyInput
		}
	}
	source := stable("growth-source", input.Key)
	if err = exec(ctx, tx, `INSERT INTO sources(robot_id,id,created_at,updated_at,scope_id,kind,dedupe_key,content_text,observed_at,result) VALUES(?,?,?,?,?,1,?,?,?,1)`, d.RobotID, source, now, now, scope, input.Key, input.Summary, parseTime(input.At)); err != nil {
		return err
	}
	if material != nil {
		if err = exec(ctx, tx, "UPDATE sources SET material_id=?,kind=2,observation_scope=?,result=? WHERE robot_id=? AND id=?", material, map[bool]string{true: "标题、简介和评论", false: "已记录的实际材料内容"}[input.EvidenceKind == 4], map[bool]int{true: 2, false: 1}[input.EvidenceKind == 4], d.RobotID, source); err != nil {
			return err
		}
	}
	if err = exec(ctx, tx, `INSERT INTO memories(robot_id,id,created_at,updated_at,scope_id,kind,episode_key,title,summary,occurred_at,episode_status) VALUES(?,?,?,?,?,1,?,'实际经历',?,?,2)`, d.RobotID, episode, now, now, scope, input.Key, input.Summary, parseTime(input.At)); err != nil {
		return err
	}
	if err = exec(ctx, tx, "INSERT INTO memory_sources(robot_id,created_at,memory_id,source_id,evidence_role) VALUES(?,?,?,?,1)", d.RobotID, now, episode, source); err != nil {
		return err
	}
	if err = d.indexEpisode(ctx, tx, episode, scope, input); err != nil {
		return err
	}
	modifier := stable("modifier", episode)
	bonus := int64(0)
	activated := 0
	eligible := 0
	if input.RandomDraw != nil {
		eligible = 1
		if *input.RandomDraw < 500 {
			var used int
			if err = tx.QueryRowContext(ctx, "SELECT random_bonus_used FROM scoring_periods WHERE robot_id=? AND id=?", d.RobotID, period).Scan(&used); err != nil {
				return err
			}
			if used == 0 {
				bonus = input.RandomBonus
				activated = 1
				if err = exec(ctx, tx, "UPDATE scoring_periods SET random_bonus_used=1 WHERE robot_id=? AND id=?", d.RobotID, period); err != nil {
					return err
				}
			}
		}
	}
	if err = exec(ctx, tx, `INSERT INTO episode_modifiers(robot_id,id,created_at,updated_at,episode_id,rule_version_id,period_id,origin_kind,relationship_factor_bp,interaction_factor_bp,repeat_discount_bp,novelty_bp,resonance_bp,effort_bp,random_eligible,random_draw,random_bonus_units,random_activated,draw_algorithm_version,evaluated_at) VALUES(?,?,?,?,?,?,?,4,?,?,10000,?,?,?,?,?,?,?,'crypto-v1',?)`, d.RobotID, modifier, now, now, episode, rule, period, input.RelationFactor, input.InteractionFactor, input.NoveltyUnits, input.ResonanceUnits, input.EffortUnits, eligible, input.RandomDraw, bonus, activated, now); err != nil {
		return err
	}
	if input.RelationshipUnits+input.EventUnits+input.NoveltyUnits+input.ResonanceUnits+input.EffortUnits == 0 {
		copyInput := *input
		copyInput.EventUnits = input.Importance
		input = &copyInput
	}
	importance := min(int64(10000), input.Importance+bonus)
	if err = exec(ctx, tx, `INSERT INTO weight_evaluations(robot_id,id,created_at,updated_at,memory_id,evaluation_seq,episode_modifier_id,rule_version_id,relationship_units,event_units,novelty_units,resonance_units,effort_units,random_bonus_units,importance_units,reason_codes_json,effective_at) VALUES(?,?,?,?,?,1,?,?,?,?,?,?,?, ?,?,'["verified_experience"]',?)`, d.RobotID, stable("weight", episode), now, now, episode, modifier, rule, input.RelationshipUnits, input.EventUnits, input.NoveltyUnits, input.ResonanceUnits, input.EffortUnits, bonus, importance, now); err != nil {
		return err
	}
	for _, award := range input.Awards {
		topic := stable("topic", award.Topic)
		topicScope := "internal"
		if award.Public {
			topicScope = "public"
		}
		if err = exec(ctx, tx, `INSERT INTO topics(robot_id,id,created_at,updated_at,scope_id,key,label,public_eligible) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, d.RobotID, topic, now, now, topicScope, award.Topic, award.Topic, award.Public); err != nil {
			return err
		}
		if award.Parent != "" {
			parent := stable("topic", award.Parent)
			if err = exec(ctx, tx, `INSERT INTO topics(robot_id,id,created_at,updated_at,scope_id,key,label,public_eligible) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, d.RobotID, parent, now, now, topicScope, award.Parent, award.Parent, award.Public); err != nil {
				return err
			}
			if err = exec(ctx, tx, "UPDATE topics SET parent_topic_id=? WHERE robot_id=? AND id=?", parent, d.RobotID, topic); err != nil {
				return err
			}
		}
		if err = exec(ctx, tx, "INSERT INTO memory_topics(robot_id,created_at,memory_id,topic_id,allocation_bp) VALUES(?,?,?,?,?)", d.RobotID, now, episode, topic, 10000/max(1, len(input.Awards))); err != nil {
			return err
		}
		for _, dim := range []int{1, 2} {
			delta := award.Familiarity
			topicCap := int64(1000)
			allCap := int64(3000)
			suffix := "familiarity"
			if dim == 2 {
				delta = award.Affinity
				topicCap = 400
				allCap = 1200
				suffix = "affinity"
			}
			if err = d.growthEntry(ctx, tx, input, episode, modifier, rule, period, scope, dim, topic, "", delta, topicCap, allCap, "topic:"+topic+":"+suffix, "all_topics:"+suffix); err != nil {
				return err
			}
		}
	}
	if input.Peer != "" && input.PersonFamiliarity > 0 {
		person := stable(input.Platform, input.Peer)
		if err = exec(ctx, tx, "INSERT INTO persons(robot_id,id,created_at,updated_at) VALUES(?,?,?,?) ON CONFLICT DO NOTHING", d.RobotID, person, now, now); err != nil {
			return err
		}
		if err = exec(ctx, tx, "INSERT INTO person_accounts(robot_id,id,created_at,updated_at,person_id,platform,platform_uid,verification_kind) VALUES(?,?,?,?,?,?,?,1) ON CONFLICT DO NOTHING", d.RobotID, stable("person-account", person), now, now, person, input.Platform, input.Peer); err != nil {
			return err
		}
		role, origin := 1, 1
		if input.RelationFactor == 40000 {
			role, origin = 3, 3
			if err = exec(ctx, tx, "UPDATE relationships SET status=1 WHERE robot_id=? AND declared_role=3 AND person_id<>?", d.RobotID, person); err != nil {
				return err
			}
		}
		if err = exec(ctx, tx, "INSERT INTO relationships(robot_id,id,created_at,updated_at,person_id,declared_role,role_origin) VALUES(?,?,?,?,?,?,?) ON CONFLICT(robot_id,person_id) DO UPDATE SET declared_role=CASE WHEN relationships.declared_role=2 AND excluded.declared_role=1 THEN 2 ELSE excluded.declared_role END,role_origin=CASE WHEN relationships.declared_role=2 AND excluded.declared_role=1 THEN relationships.role_origin ELSE excluded.role_origin END,status=0,updated_at=excluded.updated_at", d.RobotID, stable("relation", person), now, now, person, role, origin); err != nil {
			return err
		}
		if err = d.growthEntry(ctx, tx, input, episode, modifier, rule, period, scope, 3, "", person, input.PersonFamiliarity, 400, 400, "person:"+person+":familiarity", ""); err != nil {
			return err
		}
		if err = d.growthEntry(ctx, tx, input, episode, modifier, rule, period, scope, 4, "", person, input.PersonAffinity, 200, 200, "person:"+person+":affinity", ""); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *Database) growthEntry(ctx context.Context, tx *sql.Tx, input *GrowthInput, episode, modifier, rule, period, scope string, dim int, topic, person string, delta, cap, allCap int64, bucket, allBucket string) error {
	var used int64
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(sum(charged_units),0) FROM growth_quota_usage WHERE robot_id=? AND period_id=? AND bucket_key=?", d.RobotID, period, bucket).Scan(&used); err != nil {
		return err
	}
	available := max(int64(0), cap-used)
	if allBucket != "" {
		var allUsed int64
		if err := tx.QueryRowContext(ctx, "SELECT coalesce(sum(charged_units),0) FROM growth_quota_usage WHERE robot_id=? AND period_id=? AND bucket_key=?", d.RobotID, period, allBucket).Scan(&allUsed); err != nil {
			return err
		}
		available = min(available, max(int64(0), allCap-allUsed))
	}
	parentBucket := ""
	if topic != "" {
		var parent *string
		if err := tx.QueryRowContext(ctx, "SELECT parent_topic_id FROM topics WHERE robot_id=? AND id=?", d.RobotID, topic).Scan(&parent); err != nil {
			return err
		}
		if parent != nil {
			suffix := "familiarity"
			if dim == 2 {
				suffix = "affinity"
			}
			parentBucket = "topic:" + *parent + ":" + suffix
			var parentUsed int64
			if err := tx.QueryRowContext(ctx, "SELECT coalesce(sum(charged_units),0) FROM growth_quota_usage WHERE robot_id=? AND period_id=? AND bucket_key=?", d.RobotID, period, parentBucket).Scan(&parentUsed); err != nil {
				return err
			}
			available = min(available, max(int64(0), cap-parentUsed))
		}
	}
	direction := int64(0)
	if delta > 0 {
		direction = 1
	} else if delta < 0 {
		direction = -1
	}
	amount := delta * direction
	applied := min(amount, available) * direction
	now := input.Now.UnixMilli()
	target := topic + person
	id := stable(episode, fmt.Sprint(dim), target)
	factor := input.RelationFactor
	if dim >= 3 {
		factor = 10000
	}
	base, allocation := input.BaseFamiliarity, int64(10000/max(1, len(input.Awards)))
	if dim == 2 || dim == 4 {
		base = 50
	}
	if dim >= 3 {
		allocation = 10000
	}
	if dim == 3 {
		base = 100
	}
	err := exec(ctx, tx, `INSERT INTO growth_entries(robot_id,id,created_at,updated_at,episode_id,source_memory_id,episode_modifier_id,scope_id,dimension,topic_id,person_id,rule_version_id,period_id,operation_kind,idempotency_key,base_units,signal_direction,relationship_factor_bp,interaction_factor_bp,allocation_bp,repeat_discount_bp,uncapped_delta,applied_delta,evidence_kind,occurred_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,1,?,?,?,?,?,?,10000,?,?,?,?)`, d.RobotID, id, now, now, episode, episode, modifier, scope, dim, nullable(topic), nullable(person), rule, period, id, base, direction, factor, input.InteractionFactor, allocation, delta, applied, input.EvidenceKind, parseTime(input.At))
	if err != nil {
		return err
	}
	for _, b := range []string{bucket, allBucket, parentBucket} {
		if b == "" {
			continue
		}
		if err = exec(ctx, tx, "INSERT INTO growth_quota_usage(robot_id,id,created_at,updated_at,period_id,entry_id,bucket_key,charged_units) VALUES(?,?,?,?,?,?,?,?)", d.RobotID, stable(id, b), now, now, period, id, b, applied*direction); err != nil {
			return err
		}
	}
	return d.rebuildGrowth(ctx, tx, dim, topic, person, rule, id, now)
}

func (d *Database) rebuildGrowth(ctx context.Context, tx *sql.Tx, dim int, topic, person, rule, entry string, now int64) error {
	var total int64
	var pos, neg, pdays, ndays, days int
	err := tx.QueryRowContext(ctx, `SELECT coalesce(sum(applied_delta),0),count(DISTINCT CASE WHEN applied_delta>0 THEN episode_id END),count(DISTINCT CASE WHEN applied_delta<0 THEN episode_id END),count(DISTINCT CASE WHEN applied_delta>0 THEN period_id END),count(DISTINCT CASE WHEN applied_delta<0 THEN period_id END),count(DISTINCT CASE WHEN applied_delta<>0 THEN period_id END) FROM growth_entries e WHERE robot_id=? AND dimension=? AND coalesce(topic_id,'')=? AND coalesce(person_id,'')=? AND operation_kind=1 AND NOT EXISTS(SELECT 1 FROM growth_entries r WHERE r.robot_id=e.robot_id AND r.reverses_entry_id=e.id)`, d.RobotID, dim, topic, person).Scan(&total, &pos, &neg, &pdays, &ndays, &days)
	if err != nil {
		return err
	}
	stage := "no_preference"
	display := total
	if dim == 1 || dim == 3 {
		stage = "unfamiliar"
		if total > 0 {
			stage = "encountered"
		}
		if total >= 500 {
			stage = "beginning_familiar"
		}
		if total >= 2000 {
			stage = "fairly_familiar"
		}
		if total >= 6000 {
			stage = "familiar"
		}
	} else {
		display = max(int64(-10000), min(int64(10000), total))
		score := total
		episodes, dates := pos, pdays
		prefix := "like"
		if score < 0 {
			score = -score
			episodes, dates = neg, ndays
			prefix = "dislike"
		}
		if score >= 500 && episodes >= 3 && dates >= 2 {
			stage = prefix + "_1"
		}
		if score >= 2000 && episodes >= 8 && dates >= 5 {
			stage = prefix + "_2"
		}
		if score >= 6000 && episodes >= 15 && dates >= 10 {
			stage = prefix + "_3"
		}
	}
	id := stable("state", fmt.Sprint(dim), topic, person)
	var previous string
	var stateVersion int64
	scanErr := tx.QueryRowContext(ctx, "SELECT stage_key,state_version FROM growth_state WHERE robot_id=? AND id=?", d.RobotID, id).Scan(&previous, &stateVersion)
	if scanErr != nil && scanErr != sql.ErrNoRows {
		return scanErr
	}
	if dim == 2 || dim == 4 {
		score := total
		episodes, dates := pos, pdays
		prefix := "like"
		if score < 0 {
			score = -score
			episodes, dates = neg, ndays
			prefix = "dislike"
		}
		for _, level := range []struct {
			n              int
			threshold      int64
			episodes, days int
		}{{3, 5000, 15, 10}, {2, 1500, 8, 5}, {1, 300, 3, 2}} {
			name := fmt.Sprintf("%s_%d", prefix, level.n)
			if previous == name && score >= level.threshold && episodes >= level.episodes && dates >= level.days && (stage == "no_preference" || strings.HasSuffix(stage, "_1") && level.n > 1 || strings.HasSuffix(stage, "_2") && level.n > 2) {
				stage = name
				break
			}
		}
	}

	err = exec(ctx, tx, `INSERT INTO growth_state(robot_id,id,created_at,updated_at,dimension,topic_id,person_id,raw_total_units,total_units,positive_episode_count,negative_episode_count,positive_day_count,negative_day_count,interaction_day_count,stage_key,stage_rule_version_id,last_entry_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(robot_id,id) DO UPDATE SET updated_at=excluded.updated_at,raw_total_units=excluded.raw_total_units,total_units=excluded.total_units,positive_episode_count=excluded.positive_episode_count,negative_episode_count=excluded.negative_episode_count,positive_day_count=excluded.positive_day_count,negative_day_count=excluded.negative_day_count,interaction_day_count=excluded.interaction_day_count,stage_key=excluded.stage_key,last_entry_id=excluded.last_entry_id,state_version=growth_state.state_version+1`, d.RobotID, id, now, now, dim, nullable(topic), nullable(person), total, display, pos, neg, pdays, ndays, days, stage, rule, entry)
	if err != nil {
		return err
	}
	if previous != stage {
		return exec(ctx, tx, "INSERT INTO growth_stage_events(robot_id,id,created_at,updated_at,growth_state_id,state_version,previous_stage,next_stage,rule_version_id,trigger_entry_id,change_kind,occurred_at) VALUES(?,?,?,?,?,?,?,?,?,?,1,?)", d.RobotID, stable(id, fmt.Sprint(stateVersion+1)), now, now, id, stateVersion+1, previous, stage, rule, entry, now)
	}
	return nil
}

// GrowthBalances 返回内部主题状态，调用方必须按场景构造公开白名单投影。
func (d *Database) GrowthBalances(ctx context.Context) ([]GrowthBalance, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT t.label,t.public_eligible,coalesce(f.total_units,0),coalesce(a.total_units,0),coalesce(a.positive_episode_count,0),coalesce(a.positive_day_count,0),coalesce(a.negative_episode_count,0),coalesce(a.negative_day_count,0),coalesce(a.stage_key,'no_preference') FROM topics t LEFT JOIN growth_state f ON f.robot_id=t.robot_id AND f.topic_id=t.id AND f.dimension=1 LEFT JOIN growth_state a ON a.robot_id=t.robot_id AND a.topic_id=t.id AND a.dimension=2 WHERE t.robot_id=? AND (f.id IS NOT NULL OR a.id IS NOT NULL) ORDER BY t.key`, d.RobotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []GrowthBalance{}
	for rows.Next() {
		var b GrowthBalance
		if err = rows.Scan(&b.Topic, &b.Public, &b.Familiarity, &b.Affinity, &b.PositiveEpisodes, &b.PositiveDays, &b.NegativeEpisodes, &b.NegativeDays, &b.Stage); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

// PersonBalance 用于当前对象关系和授权管理页，不暴露其他人的关系细节。
type PersonBalance struct {
	Platform, Peer        string
	Familiarity, Affinity int64
	Days, Interactions    int
	Role                  int
}

// PersonBalances 返回有稳定平台身份的内部关系账汇总。
func (d *Database) PersonBalances(ctx context.Context) ([]PersonBalance, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT p.platform,p.platform_uid,coalesce(f.total_units,0),coalesce(a.total_units,0),coalesce(f.interaction_day_count,0),coalesce(f.positive_episode_count,0),coalesce(r.declared_role,1) FROM person_accounts p LEFT JOIN growth_state f ON f.robot_id=p.robot_id AND f.person_id=p.person_id AND f.dimension=3 LEFT JOIN growth_state a ON a.robot_id=p.robot_id AND a.person_id=p.person_id AND a.dimension=4 LEFT JOIN relationships r ON r.robot_id=p.robot_id AND r.person_id=p.person_id AND r.status=0 WHERE p.robot_id=? AND p.status=0`, d.RobotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []PersonBalance{}
	for rows.Next() {
		var b PersonBalance
		if err = rows.Scan(&b.Platform, &b.Peer, &b.Familiarity, &b.Affinity, &b.Days, &b.Interactions, &b.Role); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

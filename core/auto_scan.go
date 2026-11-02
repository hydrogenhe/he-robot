package core

import (
	"context"
	"gpt-robot/model"
	"slices"
	"strings"
	"time"
)

// AutoTick 一次有界增量检查，最多发送一条；没有新评论时绝不调用模型。
func (s *Service) AutoTick(ctx context.Context) error {
	if !s.autoRunMu.TryLock() {
		return model.Fail("BUSY", "自动回复检查正在进行")
	}
	defer s.autoRunMu.Unlock()
	runCtx, runCancel := s.beginAutoRun(ctx)
	defer s.endAutoRun(runCancel)
	ctx = runCtx
	err := s.autoTick(ctx)
	if err != nil && ctx.Err() == nil {
		_ = s.pauseAuto(ctx, "自动回复执行失败，已暂停；请检查处理记录后再启用")
	}
	return err
}
func (s *Service) autoTick(ctx context.Context) error {
	s.autoMu.Lock()
	config := s.autoConfig
	revision := s.autoState.Revision
	paused := s.autoState.Paused
	s.autoMu.Unlock()
	if !config.Enabled || paused || !s.robotRunning() {
		return nil
	}
	tickCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	ctx = tickCtx
	credentials, err := s.Credentials(ctx)
	if err != nil {
		return err
	}
	if credentials == nil || credentials.UID != config.AccountUID {
		return s.pauseAuto(ctx, "账号已切换或登录凭证缺失，已暂停")
	}
	permissions, err := s.Permissions(ctx)
	if err != nil {
		return err
	}
	if !permissions.Enabled && config.Source != "browsing" {
		return s.pauseAuto(ctx, "互动发送权限已关闭")
	}
	if config.Source == "browsing" {
		config.Videos = s.browsingTargets(config.AccountUID)
	}
	for _, bv := range config.Videos {
		if config.Source != "browsing" && !slices.Contains(permissions.AllowedVideos, bv) {
			return s.pauseAuto(ctx, "监听视频已不在发送白名单")
		}
		if err := s.scanAutoVideo(ctx, &config, bv, revision); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return s.pauseAuto(ctx, "平台读取失败，已暂停；检查登录或风控后再保存启用")
		}
	}
	if err := s.processAutoQueue(ctx, &config, revision); err != nil {
		return err
	}
	s.autoMu.Lock()
	defer s.autoMu.Unlock()
	if s.autoState.Revision != revision {
		return nil
	}
	s.autoState.LastAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.autoState.NextAt = time.Now().Add(time.Duration(config.IntervalSeconds) * time.Second).UTC().Format(time.RFC3339Nano)
	s.pruneAutoEntries()
	return s.Store.Write(context.WithoutCancel(ctx), "data/automation/state.json", &s.autoState)
}
func (s *Service) scanAutoVideo(ctx context.Context, config *model.AutoReplyConfig, bvid string, revision int64) error {
	s.autoMu.Lock()
	snapshot, exists := s.autoState.Videos[bvid]
	s.autoMu.Unlock()
	if !exists {
		snapshot = model.AutoVideoState{Since: time.Now().Unix(), RootCounts: map[string]int{}}
	}
	if snapshot.Video.Aid == "" {
		video, err := s.Bili.Video(ctx, bvid)
		if err != nil {
			return err
		}
		snapshot.Video = video
	}
	baseline := !snapshot.Initialized
	oldCounts := map[string]int{}
	for id, count := range snapshot.RootCounts {
		oldCounts[id] = count
	}
	latestCounts := map[string]int{}
	for page := 1; page <= 3; page++ {
		if !s.autoRevisionCurrent(revision) {
			return nil
		}
		roots, err := s.Bili.Comments(ctx, snapshot.Video.Aid, page, "0")
		if err != nil {
			return err
		}
		known := 0
		for _, comment := range roots.Comments {
			latestCounts[comment.ID] = comment.ReplyCount
			_, seen := oldCounts[comment.ID]
			if seen {
				known++
			}
			if baseline && model.ValidNumber(comment.ID) {
				s.autoMu.Lock()
				if s.autoState.Revision == revision {
					key := bvid + "/" + comment.ID
					if _, exists := s.autoState.Entries[key]; !exists {
						c := comment
						c.Text = model.Clip(c.Text, 500)
						s.autoState.Entries[key] = model.AutoEntry{Key: key, At: time.Now().UTC().Format(time.RFC3339Nano), BVID: bvid, Comment: c, Status: model.AutoSkipped, Reason: "首次扫描基线，不回复历史评论"}
					}
				}
				s.autoMu.Unlock()
			}
			if !baseline {
				if err := s.observeAutoComment(ctx, config, bvid, &comment, snapshot.Since, revision); err != nil {
					return err
				}
			}
			// 只在楼中楼计数变化时查尾部，不每轮逐楼扫描。
			previous, tracked := oldCounts[comment.ID]
			if !baseline && comment.ReplyCount > 0 && (!tracked || comment.ReplyCount != previous) {
				lastPage := (comment.ReplyCount + 19) / 20
				for floorPage := lastPage; floorPage >= 1 && floorPage > lastPage-3; floorPage-- {
					replies, err := s.Bili.Comments(ctx, snapshot.Video.Aid, floorPage, comment.ID)
					if err != nil {
						return err
					}
					recent := false
					for _, reply := range replies.Comments {
						if reply.Root == "0" || reply.Root == "" {
							reply.Root = comment.ID
						}
						if reply.CreatedAt > snapshot.Since {
							recent = true
						}
						if err := s.observeAutoComment(ctx, config, bvid, &reply, snapshot.Since, revision); err != nil {
							return err
						}
					}
					if !recent {
						break
					}
				}
			}
		}
		if !roots.HasMore || (!baseline && len(roots.Comments) > 0 && known == len(roots.Comments)) {
			break
		}
	}
	if baseline {
		snapshot.Since = time.Now().Unix()
	}
	snapshot.Initialized = true
	snapshot.RootCounts = latestCounts
	s.autoMu.Lock()
	defer s.autoMu.Unlock()
	if s.autoState.Revision != revision {
		return nil
	}
	s.autoState.Videos[bvid] = snapshot
	return s.Store.Write(ctx, "data/automation/state.json", &s.autoState)
}
func (s *Service) observeAutoComment(ctx context.Context, config *model.AutoReplyConfig, bvid string, comment *model.Comment, since int64, revision int64) error {
	if !model.ValidNumber(comment.ID) || !model.ValidNumber(comment.UID) || comment.CreatedAt <= since || comment.CreatedAt > time.Now().Add(5*time.Minute).Unix() || comment.CreatedAt < time.Now().Add(-30*time.Minute).Unix() {
		return nil
	}
	key := bvid + "/" + comment.ID
	s.autoMu.Lock()
	defer s.autoMu.Unlock()
	if s.autoState.Revision != revision || !s.autoConfig.Enabled {
		return nil
	}
	if _, seen := s.autoState.Entries[key]; seen {
		return nil
	}
	decision := evaluateConversation(comment.Text, comment.UID == config.PriorityUID)
	if comment.UID == config.AccountUID {
		decision = model.AutoDecision{Reason: "机器人自己的评论，不自动回复"}
	}
	if (comment.State != nil && (*comment.State == 17 || *comment.State == 11)) || (comment.RootState != nil && (*comment.RootState == 17 || *comment.RootState == 11)) {
		decision = model.AutoDecision{Reason: "评论展示受限或待审核，跳过"}
	}
	status := model.AutoQueued
	if !decision.Allowed {
		status = model.AutoSkipped
	}
	c := *comment
	c.Text = model.Clip(c.Text, 500)
	entry := model.AutoEntry{Key: key, At: time.Now().UTC().Format(time.RFC3339Nano), BVID: bvid, Comment: c, Status: status, Reason: decision.Reason, Reply: decision.Reply}
	s.autoState.Entries[key] = entry
	// 认领/发送前会持久化整份队列，这里不为每条跳过反复写文件。
	return nil
}
func (s *Service) autoRevisionCurrent(revision int64) bool {
	s.autoMu.Lock()
	defer s.autoMu.Unlock()
	return s.robotRunning() && s.autoConfig.Enabled && !s.autoState.Paused && s.autoState.Revision == revision
}
func (s *Service) pauseAuto(ctx context.Context, reason string) error {
	s.autoMu.Lock()
	defer s.autoMu.Unlock()
	s.autoState.Paused = true
	s.autoState.Reason = reason
	s.autoState.NextAt = ""
	return s.Store.Write(context.WithoutCancel(ctx), "data/automation/state.json", &s.autoState)
}
func (s *Service) pruneAutoEntries() {
	if len(s.autoState.Entries) <= 2000 {
		return
	}
	cutoff := time.Now().Add(-24 * time.Hour).Unix()
	for key, entry := range s.autoState.Entries {
		if entry.Status != model.AutoQueued && entry.Status != model.AutoProcessing && entry.Comment.CreatedAt < cutoff {
			delete(s.autoState.Entries, key)
		}
	}
}
func (s *Service) processAutoQueue(ctx context.Context, config *model.AutoReplyConfig, revision int64) error {
	s.autoMu.Lock()
	entries := []model.AutoEntry{}
	for _, entry := range s.autoState.Entries {
		if entry.Status == model.AutoQueued {
			entries = append(entries, entry)
		}
	}
	s.autoMu.Unlock()
	slices.SortFunc(entries, compareAutoQueueTime)
	ordered := make([]model.AutoEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Comment.UID == config.PriorityUID {
			ordered = append(ordered, entry)
		}
	}
	for _, entry := range entries {
		if entry.Comment.UID != config.PriorityUID {
			ordered = append(ordered, entry)
		}
	}
	entries = ordered
	drafts, err := s.Drafts(ctx)
	if err != nil {
		return err
	}
	permissions, err := s.Permissions(ctx)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !s.autoRevisionCurrent(revision) {
			return nil
		}
		if entry.Comment.CreatedAt < time.Now().Add(-30*time.Minute).Unix() {
			s.setAutoEntry(&entry, model.AutoSkipped, "互动已超过30分钟，跳过")
			continue
		}
		if slices.Contains(permissions.BlockedUsers, entry.Comment.UID) || (len(permissions.AllowedUsers) > 0 && !slices.Contains(permissions.AllowedUsers, entry.Comment.UID)) {
			s.setAutoEntry(&entry, model.AutoSkipped, "用户不在互动权限允许范围")
			continue
		}
		duplicate := false
		for _, old := range drafts {
			if old.Account == config.AccountUID && old.BVID == entry.BVID && old.Parent == entry.Comment.ID && (activeDraft(old.Status) || old.AttemptAt != "") {
				duplicate = true
				break
			}
		}
		if duplicate {
			s.setAutoEntry(&entry, model.AutoSkipped, "该评论已有草稿或发送尝试，不重复回复")
			continue
		}
		d := model.Draft{Account: config.AccountUID, BVID: entry.BVID, TargetUser: entry.Comment.UID, TargetText: entry.Comment.Text, Text: entry.Reply, AutoCommentID: entry.Comment.ID}
		if err := s.autoSendAllowed(&d, drafts, time.Now()); err != nil {
			s.setAutoEntry(&entry, model.AutoSkipped, autoMessage(err))
			continue
		}
		// 冷却时保留队列，不提前创建草稿或检查平台。
		for _, old := range drafts {
			if old.Account == config.AccountUID && old.AttemptAt != "" {
				at, _ := time.Parse(time.RFC3339Nano, old.AttemptAt)
				if time.Since(at) < time.Duration(permissions.CooldownSeconds)*time.Second {
					return nil
				}
			}
		}
		s.setAutoEntry(&entry, model.AutoProcessing, "已认领，准备AI生成")
		if err := s.saveAutoState(ctx); err != nil {
			return err
		}
		return s.executeAutoEntry(ctx, config, &entry)
	}
	return nil
}

func compareAutoQueueTime(a, b model.AutoEntry) int { return strings.Compare(a.At, b.At) }

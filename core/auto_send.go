package core

import (
	"context"
	"errors"
	"gpt-robot/model"
	"time"
)

func autoMessage(err error) string {
	var e *model.Error
	if errors.As(err, &e) {
		return e.Message
	}
	return "自动回复处理失败"
}
func (s *Service) setAutoEntry(entry *model.AutoEntry, status model.AutoEntryStatus, reason string) {
	entry.Status = status
	entry.Reason = reason
	s.autoMu.Lock()
	s.autoState.Entries[entry.Key] = *entry
	s.autoMu.Unlock()
}
func (s *Service) saveAutoState(ctx context.Context) error {
	s.autoMu.Lock()
	defer s.autoMu.Unlock()
	return s.Store.Write(context.WithoutCancel(ctx), "data/automation/state.json", &s.autoState)
}
func (s *Service) prepareAutoEntry(ctx context.Context, config *model.AutoReplyConfig, entry *model.AutoEntry) (model.Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ds, err := s.Drafts(ctx)
	if err != nil {
		return model.Draft{}, err
	}
	for _, old := range ds {
		if old.Account == config.AccountUID && old.BVID == entry.BVID && (old.AutoCommentID == entry.Comment.ID || old.Parent == entry.Comment.ID && (activeDraft(old.Status) || old.AttemptAt != "")) {
			return old, model.Fail("AUTO_DUPLICATE", "该评论已有草稿或回复账本，不重复创建或发送")
		}
	}
	root := entry.Comment.Root
	if root == "" || root == "0" {
		root = entry.Comment.ID
	}
	d := model.Draft{ID: model.ID(), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), BVID: entry.BVID, Root: root, Parent: entry.Comment.ID, Text: entry.Reply, Account: config.AccountUID, TargetUser: entry.Comment.UID, TargetText: entry.Comment.Text, Status: "pending", AutoCommentID: entry.Comment.ID}
	if _, err := s.permitted(ctx, &d, config.AccountUID); err != nil {
		return d, err
	}
	ds = append(ds, d)
	if err := s.Store.Write(ctx, "data/events/drafts.json", ds); err != nil {
		return d, err
	}
	return d, nil
}
func (s *Service) executeAutoEntry(ctx context.Context, config *model.AutoReplyConfig, entry *model.AutoEntry) error {
	input := model.OperationStart{Platform: "bilibili", Action: "auto-reply", Source: "automation"}
	operationID, err := s.StartOperation(ctx, &input)
	if err != nil {
		s.setAutoEntry(entry, model.AutoInterrupted, "流水保存失败，未发送")
		s.saveAutoState(ctx)
		return err
	}
	at := time.Now().UTC().Format(time.RFC3339Nano)
	if entry.Comment.CreatedAt > 0 {
		at = time.Unix(entry.Comment.CreatedAt, 0).UTC().Format(time.RFC3339Nano)
	}
	experience := model.DevelopmentInput{Platform: "bilibili", Account: config.AccountUID, Peer: entry.Comment.UID, Visibility: "public", Source: "comment:" + entry.Comment.ID, At: at, Text: entry.Comment.Text, Interaction: true}
	reply, runErr := s.generateReplyWithMemories(ctx, operationID, "video-comment", entry.Comment.Text, nil, nil, &experience)
	entry.Reply = reply
	if runErr != nil && (!s.robotRunning() || ctx.Err() != nil) {
		s.setAutoEntry(entry, model.AutoSkipped, "机器人已暂停，未发送；不重放旧消息")
		finish := model.OperationFinish{Status: "cancelled", Account: config.AccountUID}
		if err := s.FinishOperation(context.WithoutCancel(ctx), operationID, &finish); err != nil {
			return err
		}
		return s.saveAutoState(ctx)
	}
	var draft model.Draft
	if runErr == nil && reply != "" {
		draft, runErr = s.prepareAutoEntry(ctx, config, entry)
	}
	if runErr == nil && reply == "" {
		s.setAutoEntry(entry, model.AutoSkipped, "AI判断无需回复")
		finish := model.OperationFinish{Status: "skipped", Account: config.AccountUID}
		if err := s.FinishOperation(ctx, operationID, &finish); err != nil {
			return err
		}
		return s.saveAutoState(ctx)
	}
	if runErr == nil {
		entry.DraftID = draft.ID
		s.setAutoEntry(entry, model.AutoProcessing, "草稿已持久化，开始发送复核")
		if err := s.saveAutoState(ctx); err != nil {
			return err
		}
		draft, runErr = s.Send(ctx, draft.ID)
	}
	result := "failed"
	reason := autoMessage(runErr)
	if runErr == nil {
		result = draft.Status
		reason = "AI回复已执行，公开展示以平台状态为准"
	}
	entry.DraftID = draft.ID
	entry.Result = result
	duplicate := runErr != nil && OperationErrorCode(runErr) == "AUTO_DUPLICATE"
	if duplicate {
		result = "skipped"
		entry.Result = result
	}
	status := model.AutoCompleted
	if runErr != nil || draft.Status == "unknown" || draft.Status == "sending" {
		status = model.AutoInterrupted
	}
	if duplicate || (runErr != nil && OperationErrorCode(runErr) == "SENSITIVE_OUTPUT") {
		status = model.AutoSkipped
	}
	s.setAutoEntry(entry, status, reason)
	finish := model.OperationFinish{Status: result, Reference: draft.ID, Account: config.AccountUID}
	if runErr != nil {
		finish.ErrorCode = OperationErrorCode(runErr)
	}
	if err := s.FinishOperation(ctx, operationID, &finish); err != nil {
		return err
	}
	if err := s.saveAutoState(ctx); err != nil {
		return err
	}
	if status == model.AutoInterrupted || draft.Status == "failed" || draft.Status == "restricted" || draft.Status == "pending_review" {
		return s.pauseAuto(ctx, "发送失败、结果不明或展示受限，已暂停；请人工回查，不自动重试")
	}
	return nil
}

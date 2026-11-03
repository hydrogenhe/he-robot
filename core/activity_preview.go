package core

import (
	"context"
	"gpt-robot/model"
	"strings"
)

// OperationPreviews 仅为认证网页按当前账号即时组装摘要，不将私聊正文写入通用流水或记忆。
func (s *Service) OperationPreviews(ctx context.Context, items []model.Operation) (map[string]model.ActivityPreview, error) {
	result := map[string]model.ActivityPreview{}
	needed := map[string]bool{}
	for _, op := range items {
		needed[op.Reference] = true
	}
	drafts, err := s.Drafts(ctx)
	if err != nil {
		return nil, err
	}
	byDraft := map[string]model.Draft{}
	for _, d := range drafts {
		if needed[d.ID] {
			byDraft[d.ID] = d
		}
	}
	events, err := s.Store.Events(ctx, 200)
	if err != nil {
		return nil, err
	}
	byEvent := map[string]model.Event{}
	for _, e := range events {
		if needed[e.ID] {
			byEvent[e.ID] = e
		}
	}
	s.privateMu.Lock()
	account := s.privateConfig.SenderUID
	peer := s.privateConfig.PeerUID
	private := map[string]model.PrivateOutbox{}
	privateGroups := map[string][]string{}
	for _, out := range s.privateState.Outbox {
		if out.GroupID != "" && out.Status == "sent" {
			privateGroups[out.GroupID] = append(privateGroups[out.GroupID], out.Text)
		}
		if needed[out.RequestID] {
			private[out.RequestID] = out
		}
	}
	s.privateMu.Unlock()
	secrets := s.generationSecrets(ctx)
	for _, op := range items {
		preview := model.ActivityPreview{}
		if op.Action == "diary-generate" && s.Store.Database != nil {
			value, previewErr := s.Store.Database.DiaryOperationPreview(ctx, op.ID)
			if previewErr != nil {
				return nil, previewErr
			}
			preview = value
		}
		if e, ok := byEvent[op.Reference]; ok {
			preview.Title = e.Summary
			if d, ok := e.Data["description"].(string); ok {
				preview.Text = d
			}
		}
		if d, ok := byDraft[op.Reference]; ok && d.Account == op.Account {
			preview.Title = "与 UID " + d.TargetUser + " 的评论互动"
			preview.Text = d.Text
		}
		if (op.Action == "private-auto-reply" || op.Action == "private-send" || op.Action == "private-sticker-learn" || strings.HasPrefix(op.Action, "proactive-")) && op.Account == account && account != "" && peer != "" {
			if out, ok := private[op.Reference]; ok {
				preview.Title = "回复主人 · UID " + peer
				preview.Text = out.Text
				if out.GroupID != "" && len(privateGroups[out.GroupID]) > 0 {
					preview.Text = strings.Join(privateGroups[out.GroupID], " ")
				}
			}
		}
		preview.Title = model.RedactSensitiveText(preview.Title, secrets...)
		preview.Text = model.RedactSensitiveText(preview.Text, secrets...)
		if preview.Title != "" || preview.Text != "" {
			result[op.ID] = preview
		}
	}
	return result, nil
}

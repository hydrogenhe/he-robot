package core

import (
	"context"
	"fmt"
	"gpt-robot/model"
	"log/slog"
	"slices"
	"strings"
	"time"
)

func (s *Service) selfQueryAuthorized(ctx context.Context, round *conversationRound) bool {
	if round == nil || ctx.Err() != nil || !s.robotRunning() {
		return false
	}
	s.privateMu.Lock()
	defer s.privateMu.Unlock()
	return s.privateConfig == round.config && s.privateConfig.Enabled && round.message.Sender == round.config.PeerUID && round.message.Receiver == round.config.SenderUID && s.privateState.Processing == round.message.Key
}

func (s *Service) selfDiaryDate(value string) (string, error) {
	now := s.DiaryNow()
	switch value {
	case "", "today":
		return diaryDay(now), nil
	case "yesterday":
		return diaryDay(now.Add(-24 * time.Hour)), nil
	case "latest":
		return value, nil
	default:
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return "", model.Fail("SELF_QUERY", "日记日期无效，请说明要看哪一天。")
		}
		return value, nil
	}
}

// querySelf 统一受控查询入口；领域读取复用当前机器人存储，不接受模型SQL或文件路径。
func (s *Service) querySelf(ctx context.Context, round *conversationRound, action *model.SelfAction) (*model.SelfQueryResult, *model.DiaryEntry, error) {
	if !s.selfQueryAuthorized(ctx, round) {
		return nil, nil, model.Fail("PRIVATE_SCOPE", "私聊授权已变化，未读取自身资料")
	}
	if action == nil || len([]rune(action.Query)) > 80 || strings.ContainsAny(action.Query, "\r\n") || (action.Action != "query_self" && action.Action != "write_diary") || action.Action == "write_diary" && action.Resource != "diary" || action.Full && action.Resource != "diary" {
		return nil, nil, model.Fail("SELF_QUERY", "这项自身资料操作尚不支持。")
	}
	search := action.Query
	if (action.Resource == "memories" || action.Resource == "experiences") && action.Date != "" && action.Date != "latest" {
		day, err := s.selfDiaryDate(action.Date)
		if err != nil {
			return nil, nil, err
		}
		search = day + " " + search
	}
	result := &model.SelfQueryResult{
		Resource: action.Resource,
		Records:  []model.SelfRecord{},
	}
	var entry *model.DiaryEntry
	switch action.Resource {
	case "diary":
		day, err := s.selfDiaryDate(action.Date)
		if err != nil {
			return nil, nil, err
		}
		entry = &model.DiaryEntry{
			Date:   day,
			Status: "not_written",
		}
		if day == "latest" {
			rows, err := s.Store.Diaries(ctx, 30)
			if err != nil {
				return nil, nil, err
			}
			for _, row := range rows {
				if row.Status == "completed" {
					copyEntry := row
					entry = &copyEntry
					break
				}
			}
		} else {
			if _, err := s.Store.Read(ctx, diaryFile(day), entry); err != nil {
				return nil, nil, err
			}
			if entry.Date != day {
				return nil, nil, model.Fail("SELF_QUERY", "日记日期与记录不一致，未返回内容。")
			}
		}
		if action.Action == "write_diary" {
			if day != diaryDay(s.DiaryNow()) {
				return nil, nil, model.Fail("SELF_QUERY", "只能现在写今天的日记，不补造过去的经历。")
			}
			// 聊天不因页面测试模式开启而覆盖已经完成的日记，也不自动重试失败。
			if entry.Status != "completed" {
				input := model.DiaryGenerateInput{DirectReply: true}
				entry, err = s.generateDiary(ctx, &input)
				if err != nil {
					return nil, nil, err
				}
			}
		}
		record := model.SelfRecord{
			Date:   entry.Date,
			Title:  entry.Title,
			Status: entry.Status,
			Source: entry.OperationID,
		}
		result.Records = append(result.Records, record)
	case "memories":
		if !round.config.MemoryEnabled {
			return nil, nil, model.Fail("SELF_QUERY", "私聊记忆当前关闭，没有读取私聊记忆。")
		}
		query := model.PrivateMemoryQuery{
			Query:          &search,
			BeforeSequence: round.message.Seq,
			PageSize:       5,
		}
		page, err := s.PrivateMemories(ctx, &query)
		if err != nil {
			return nil, nil, err
		}
		for _, memory := range page.Items {
			record := model.SelfRecord{Text: memory.Quote}
			if len(memory.Sources) > 0 {
				source := memory.Sources[len(memory.Sources)-1]
				record.Date, record.Source = source.OccurredAt, source.MessageKey
			}
			result.Records = append(result.Records, record)
		}
	case "experiences":
		experience := model.DevelopmentInput{
			Platform:       "bilibili",
			Account:        round.config.SenderUID,
			Peer:           round.config.PeerUID,
			Visibility:     "private",
			BeforeSequence: round.message.Seq,
		}
		channel := "private-message"
		var scope *model.DevelopmentInput
		if round.config.MemoryEnabled {
			scope = &experience
		} else {
			channel = "video-comment"
		}
		rows, err := s.recallEpisodes(ctx, search, channel, scope)
		if err != nil {
			return nil, nil, err
		}
		for _, row := range rows {
			record := model.SelfRecord{
				Date:   row.OccurredAt,
				Text:   row.Summary + "；当时感想：" + row.Reaction,
				Source: row.SourceID,
			}
			result.Records = append(result.Records, record)
		}
	case "growth":
		growth, err := s.DevelopmentStatus(ctx)
		if err != nil {
			return nil, nil, err
		}
		for _, interest := range growth.Interests {
			if interest.Visibility == "private" && (interest.Account != round.config.SenderUID || interest.Conversation != round.config.PeerUID || !round.config.MemoryEnabled) {
				continue
			}
			if action.Query != "" && !strings.Contains(interest.Topic, action.Query) {
				continue
			}
			record := model.SelfRecord{
				Title: interest.Topic,
				Text:  fmt.Sprintf("熟悉程度%d；喜好%d；接触不等于喜欢", interest.Familiarity, interest.Preference),
				Date:  interest.UpdatedAt,
			}
			result.Records = append(result.Records, record)
			if len(result.Records) == 5 {
				break
			}
		}
	case "activities":
		query := model.OperationQuery{
			Page:     1,
			PageSize: 5,
		}
		if action.Date != "" && action.Date != "latest" {
			day, err := s.selfDiaryDate(action.Date)
			if err != nil {
				return nil, nil, err
			}
			zone := time.FixedZone("Asia/Shanghai", 8*60*60)
			start, _ := time.ParseInLocation("2006-01-02", day, zone)
			from := start.UTC().Format(time.RFC3339Nano)
			to := start.Add(24*time.Hour - time.Nanosecond).UTC().Format(time.RFC3339Nano)
			query.From, query.To = &from, &to
		}
		rows := []model.Operation{}
		if s.Store.Database != nil {
			page, err := s.Store.Database.OperationPage(ctx, &query, nil)
			if err != nil {
				return nil, nil, err
			}
			rows = page.Items
		} else {
			s.operationsMu.Lock()
			for _, row := range s.operations {
				if query.From != nil && row.At < *query.From || query.To != nil && row.At > *query.To {
					continue
				}
				rows = append(rows, row)
			}
			s.operationsMu.Unlock()
			slices.SortFunc(rows, compareOperations)
			rows = rows[:min(5, len(rows))]
		}
		for _, row := range rows {
			record := model.SelfRecord{
				Date:   row.At,
				Title:  row.Action,
				Status: row.Status,
				Source: row.ID,
			}
			result.Records = append(result.Records, record)
		}
	case "status":
		state, err := s.CharacterStatus(ctx)
		if err != nil {
			return nil, nil, err
		}
		diary, err := s.DiaryOverview(ctx)
		if err != nil {
			return nil, nil, err
		}
		result.Records = append(result.Records, model.SelfRecord{
			Title: "当前角色状态",
			Text:  state.Description,
		}, model.SelfRecord{
			Title:  "今日日记",
			Date:   diary.Today,
			Status: diary.TodayStatus,
		})
	default:
		return nil, nil, model.Fail("SELF_QUERY", "目前还不能查询这类资料。")
	}
	if !s.selfQueryAuthorized(ctx, round) {
		return nil, nil, model.Fail("PRIVATE_SCOPE", "查询期间授权已变化，未返回资料")
	}
	return result, entry, nil
}

func (s *Service) completeSelfAction(ctx context.Context, operation string, round *conversationRound, prompt *model.ReplyPrompt, first *model.GeneratedReply) (*model.GeneratedReply, error) {
	if !s.selfQueryAuthorized(ctx, round) || prompt.Channel != "private-message" || !prompt.Owner || prompt.SelfResult != nil || len(prompt.SelfCapabilities) == 0 || len(first.Actions) > 0 || first.RecallQuery != "" {
		return nil, model.Fail("PRIVATE_SCOPE", "本轮不能执行自身资料动作")
	}
	data, diary, err := s.querySelf(ctx, round, first.SelfAction)
	// 动作输出替换模型执行前的猜测；失败也只发送已经确认的事实。
	s.modelRouter.CloseConversations()
	if err != nil {
		slog.Warn("自身资料动作未完成", "operation", operation, "code", OperationErrorCode(err))
		text := "这次没有查阅或写入成功，稍后再试试吧。"
		if business, ok := err.(*model.Error); ok {
			switch business.Code {
			case "SELF_QUERY":
				text = business.Message
			case "DIARY_ATTEMPTED":
				text = "今天已尝试写日记但没有完成，我不会自动重复生成。"
			}
		}
		if !s.selfQueryAuthorized(ctx, round) {
			return nil, err
		}
		reply := &model.GeneratedReply{
			Text:     text,
			Messages: []string{text},
		}
		return reply, nil
	}
	if diary != nil && (first.SelfAction.Full || first.SelfAction.Action == "write_diary") {
		text := "这一天的日记还没有写好。"
		if diary.Status == "empty" {
			text = "今天还没有足够的已记录经历，暂时没有写成日记。"
		}
		if diary.Status == "completed" {
			body := diary.Date + "《" + diary.Title + "》\n" + diary.Body
			if !model.Text(diary.Title, 40) || !model.Text(diary.Body, 800) || model.ContainsSensitiveText(body, s.generationSecrets(ctx)...) {
				return nil, model.Fail("SENSITIVE_OUTPUT", "日记内容不合格，未发送")
			}
			chars := []rune(body)
			for len(chars) > 0 {
				n := min(450, len(chars))
				round.document = append(round.document, string(chars[:n]))
				chars = chars[n:]
			}
			text = "这是我保存的这篇日记。"
		}
		reply := &model.GeneratedReply{
			Text:     text,
			Messages: []string{text},
		}
		return reply, nil
	}
	if first.Usage == nil || first.Usage.Input+first.Usage.Output > 9904 {
		reply := &model.GeneratedReply{Text: "已查询到记录，但本轮回复预算不足，请稍后再问我。"}
		return reply, nil
	}
	next := *prompt
	next.SelfCapabilities, next.Capabilities = nil, nil
	next.SelfResult = data
	next.AllowRecall, next.RecallComplete = false, true
	next.Conversation = nil
	s.sanitizeReplyPrompt(ctx, &next)
	bounded, err := model.BoundReplyPrompt(&next)
	if err != nil {
		return nil, err
	}
	result, err := s.generateReplyRound(ctx, operation, true, bounded)
	if err != nil {
		return nil, err
	}
	if result.Usage != nil && first.Usage.Input+first.Usage.Output+result.Usage.Input+result.Usage.Output > 24000 {
		return nil, model.Fail("CONTEXT_BUDGET", "本次查询已知用量超过上限")
	}
	if !s.selfQueryAuthorized(ctx, round) || result.SelfAction != nil || len(result.Actions) > 0 || result.RecallQuery != "" {
		return nil, model.Fail("PRIVATE_SCOPE", "查询回复不能再次执行动作或使用旧授权")
	}
	return result, nil
}

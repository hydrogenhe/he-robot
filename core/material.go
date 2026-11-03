package core

import (
	"context"
	"fmt"
	"gpt-robot/model"
	"strings"
)

// RecordMaterialExperience 只由已核验的本机材料入口调用；复用记忆、成长和活动规则，不发送平台内容。
func (s *Service) RecordMaterialExperience(ctx context.Context, input *model.MaterialExperienceInput) (result *model.MaterialExperienceResult, err error) {
	if input == nil || !developmentIdentifier.MatchString(input.Platform) || len(input.Platform) > 24 || !developmentIdentifier.MatchString(input.SourceID) || !model.Text(input.SourceID, 100) || !model.Text(input.Title, 100) || !model.Text(input.Summary, 350) || !model.Text(input.Memory, 1000) || !model.Text(input.Reaction, 60) || input.FrameCount < 1 || input.FrameCount > 540 || input.Signal == nil || len(input.Interests) > 2 || len(input.Usage) < 1 || len(input.Usage) > 2 {
		return nil, model.Fail("INPUT", "材料经历缺少已核验来源、有限摘要、感想或真实用量")
	}
	signal := input.Signal
	traits := model.CharacterTraits{Warmth: signal.Warmth, Curiosity: signal.Curiosity, Playfulness: signal.Playfulness}
	if !validMood(signal.Mood) || signal.Intensity < 0 || signal.Intensity > 100 || !validTraits(&traits, -1, 1) {
		return nil, model.Fail("INPUT", "材料状态建议超出范围")
	}
	semanticTopics := []string{}
	for _, interest := range input.Interests {
		if !model.Text(interest.Topic, 20) || strings.ContainsAny(interest.Topic, "\r\n") || interest.Delta < -1 || interest.Delta > 1 {
			return nil, model.Fail("INPUT", "语义主题格式或成长建议超出范围")
		}
		semanticTopics = append(semanticTopics, interest.Topic)
	}
	text := strings.Join([]string{input.Title, input.Summary, input.Memory, input.Reaction, strings.Join(semanticTopics, "、")}, "\n")
	if model.ContainsSensitiveText(text, s.generationSecrets(ctx)...) {
		return nil, model.Fail("SENSITIVE", "材料结果含敏感内容，未保存")
	}
	for _, u := range input.Usage {
		if u.Provider == "" || u.RequestID == "" || u.Model == "" || u.Input < 0 || u.Output < 0 || u.CachedInput != nil && (*u.CachedInput < 0 || *u.CachedInput > u.Input) {
			return nil, model.Fail("INPUT", "材料用量不是有效回执")
		}
	}
	// 独立锁覆盖整件材料；其他普通聊天仍走原有锁。重复请求不刷经历或成长。
	s.materialMu.Lock()
	defer s.materialMu.Unlock()
	key := input.Platform + ":" + input.SourceID
	events, err := s.Store.Events(ctx, 0)
	if err != nil {
		return nil, err
	}
	for _, event := range events {
		if event.Kind == "material-understood" && event.Data["materialKey"] == key {
			return nil, model.Fail("CONFLICT", "该材料经历已经记录，禁止重复入账；请查看已有来源")
		}
	}
	credentials, err := s.Credentials(ctx)
	if err != nil {
		return nil, err
	}
	if credentials == nil || credentials.UID == "" {
		return nil, model.Fail("ACCOUNT", "材料经历需要当前已绑定账号")
	}
	start := model.OperationStart{Platform: input.Platform, Action: "material-understood", Source: "local"}
	operation := input.OperationID
	if operation == "" {
		operation, err = s.StartOperation(ctx, &start)
		if err != nil {
			return nil, err
		}
	} else {
		s.operationsMu.Lock()
		op, ok := s.operations[operation]
		s.operationsMu.Unlock()
		if ok && input.ResumeProcessing && op.Status == "failed" && op.ErrorCode == "INPUT" && op.Reference == "" && op.Platform == input.Platform && op.Action == "material-understood" {
			op.Status = "running"
			op.ErrorCode = ""
			if err = s.Store.AppendOperation(ctx, &op); err != nil {
				return nil, err
			}
			s.operationsMu.Lock()
			s.operations[operation] = op
			s.operationsMu.Unlock()
		}
		if !ok || op.Status != "running" || op.Platform != input.Platform || op.Action != "material-understood" {
			return nil, model.Fail("INPUT", "材料活动不匹配")
		}
	}
	result = &model.MaterialExperienceResult{OperationID: operation}
	finish := model.OperationFinish{Status: "failed", Account: credentials.UID}
	defer func() {
		if finishErr := s.FinishOperation(context.WithoutCancel(ctx), operation, &finish); finishErr != nil && err == nil {
			err = finishErr
		}
	}()
	for _, usage := range input.Usage {
		report := model.UsageReport{OperationID: operation, Usage: usage}
		if err = s.reportUsage(ctx, &report, "codex_app_server"); err != nil {
			return result, err
		}
	}
	description := fmt.Sprintf("来源%s；读取%d张抽帧与OCR字幕，未听音频，非全程观看。%s", input.SourceID, input.FrameCount, input.Summary)
	event, err := s.Store.Event(ctx, "material-understood", "阅读视频材料："+input.Title, map[string]any{"materialKey": key, "platform": input.Platform, "sourceId": input.SourceID, "frameCount": input.FrameCount, "ocrRead": true, "audioRead": false, "description": description, "reaction": input.Reaction, "semanticTopics": semanticTopics})
	if err != nil {
		return result, err
	}
	result.EventID = event.ID
	noteInput := model.NoteInput{Kind: "memory", Title: input.Title, Text: description + "\n模型识别的主题：" + strings.Join(semanticTopics, "、") + "\n当时的感想：" + input.Reaction + "\n记住：" + input.Memory, Sources: []string{event.ID}}
	note, err := s.Note(ctx, &noteInput)
	if err != nil {
		return result, err
	}
	result.NoteID = note.ID
	development := model.DevelopmentInput{Platform: input.Platform, Account: credentials.UID, Visibility: "public", Source: "material:" + input.SourceID, At: event.At, Text: model.Clip(description, 430) + "；语义主题：" + strings.Join(semanticTopics, "、"), Reaction: input.Reaction, EvidenceKind: 1, MaterialKey: key, Interests: input.Interests}
	if err = s.RecordDevelopment(ctx, &development); err != nil {
		return result, err
	}
	if err = s.applyCharacterSignal(ctx, event.ID, "material", signal); err != nil {
		return result, err
	}
	if err = s.queueProactive(ctx, "video", "video:"+event.ID, "刚读了《"+input.Title+"》的视频材料："+input.Reaction+"\n"+input.SourceURL, operation); err != nil {
		return result, err
	}
	finish.Status = "recorded"
	finish.Reference = event.ID
	return result, nil
}

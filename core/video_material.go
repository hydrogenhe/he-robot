package core

import (
	"context"
	"gpt-robot/model"
)

func (s *Service) readUnderstoodVideo(ctx context.Context, c *model.BrowsingConfig, state *model.BrowsingState, video *model.Video, keyword, operation string) (err error) {
	if s.VideoReader == nil {
		return model.Fail("VIDEO_ENV", "当前平台未接入视频理解适配")
	}
	defer func() {
		if err == nil {
			return
		}
		s.operationsMu.Lock()
		op := s.operations[operation]
		s.operationsMu.Unlock()
		if op.Status == "running" {
			finish := model.OperationFinish{Status: "failed", ErrorCode: OperationErrorCode(err)}
			_ = s.FinishOperation(context.WithoutCancel(ctx), operation, &finish)
		}
	}()
	persona, err := s.Store.Text(ctx, "data/character/persona.md")
	if err != nil {
		return err
	}
	character, err := s.CharacterStatus(ctx)
	if err != nil {
		return err
	}
	input := model.VideoReadInput{Video: *video, Persona: persona, State: character.Description, MaxMinutes: c.MaxDurationMinutes}
	budget, err := s.reserveGeneration(ctx, false)
	if err != nil {
		return err
	}
	result, runErr := s.VideoReader.ReadVideo(ctx, &input)
	var usage *model.TokenUsage
	if result != nil && len(result.Usage) > 0 {
		usage = &result.Usage[0]
	}
	if err = s.settleGeneration(context.WithoutCancel(ctx), budget, false, usage); err != nil {
		runErr = err
	}
	if usage != nil {
		report := model.UsageReport{OperationID: operation, Usage: *usage}
		if reportErr := s.reportUsage(context.WithoutCancel(ctx), &report, generatedUsageSource(usage)); reportErr != nil {
			runErr = reportErr
		}
	}
	s.browsingMu.Lock()
	valid := s.browsingConfig.Enabled && s.browsingState.Revision == state.Revision
	s.browsingMu.Unlock()
	if runErr == nil && (!valid || !s.robotRunning() || ctx.Err() != nil) {
		runErr = model.Fail("CANCELLED", "视频理解期间授权改变，未保存或发送")
	}
	if runErr != nil {
		finish := model.OperationFinish{Status: "failed", ErrorCode: OperationErrorCode(runErr)}
		if err = s.FinishOperation(context.WithoutCancel(ctx), operation, &finish); err != nil {
			return err
		}
		return runErr
	}
	if result == nil {
		return model.Fail("AI_REPLY", "视频没有有效材料结果")
	}
	result.OperationID = operation
	saved, err := s.RecordMaterialExperience(ctx, result)
	if err != nil {
		return err
	}
	record := model.BrowsedVideo{Video: *video, Keyword: keyword, At: s.ProactiveNow().UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), AccountUID: c.AccountUID, Revision: state.Revision, EventID: saved.EventID, Preview: result.Reaction}
	state.Records = append(state.Records, record)
	if len(state.Records) > 100 {
		state.Records = state.Records[len(state.Records)-100:]
	}
	return nil
}

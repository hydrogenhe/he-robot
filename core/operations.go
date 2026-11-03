package core

import (
	"context"
	"errors"
	"gpt-robot/model"
	"log/slog"
	"slices"
	"strings"
	"time"
)

type usageRequestKey struct {
	provider  string
	requestID string
}

func (s *Service) initializeOperations(ctx context.Context) error {
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	records, err := s.Store.Operations(ctx)
	if err != nil {
		return err
	}
	if err := s.initializeUsageExclusions(ctx); err != nil {
		return err
	}
	if _, err := s.Store.Read(ctx, "data/usage/codex-cursor.json", &s.codexState); err != nil {
		return err
	}
	s.operations = make(map[string]model.Operation)
	s.usageRequests = make(map[usageRequestKey]string)
	for _, op := range records {
		s.operations[op.ID] = op
	}
	for id, op := range s.operations {
		for _, u := range op.Usage {
			key := usageRequestKey{provider: u.Provider, requestID: u.RequestID}
			s.usageRequests[key] = id
		}
		if op.Status == "running" {
			op.Status = "unknown"
			op.ErrorCode = "INTERRUPTED"
			if err := s.Store.AppendOperation(ctx, &op); err != nil {
				return err
			}
			s.operations[id] = op
		}
	}
	// 仅首次补入已有事实；标记最后写入，中断后按事实ID去重续补。
	var done bool
	if _, err := s.Store.Read(ctx, "data/usage/history-imported.json", &done); err != nil {
		return err
	}
	if done {
		return nil
	}
	events, err := s.Store.Events(ctx, 0)
	if err != nil {
		return err
	}
	for _, e := range events {
		platform := "bilibili"
		switch e.Kind {
		case "comments", "draft", "sent", "send_result", "login":
		default:
			continue
		}
		if _, ok := s.operations[e.ID]; ok {
			continue
		}
		op := model.Operation{
			ID:        e.ID,
			At:        e.At,
			Platform:  platform,
			Action:    e.Kind,
			Source:    "history",
			Status:    "recorded",
			Reference: e.ID,
			Usage:     []model.TokenUsage{},
		}
		if account, ok := e.Data["account"].(string); ok {
			op.Account = account
		}
		if err := s.Store.AppendOperation(ctx, &op); err != nil {
			return err
		}
		s.operations[op.ID] = op
	}
	notes, err := s.Store.Notes(ctx)
	if err != nil {
		return err
	}
	for _, n := range notes {
		if _, ok := s.operations[n.ID]; ok {
			continue
		}
		op := model.Operation{
			ID:        n.ID,
			At:        n.At,
			Platform:  "local",
			Action:    "note",
			Source:    "history",
			Status:    "recorded",
			Reference: n.ID,
			Usage:     []model.TokenUsage{},
		}
		if err := s.Store.AppendOperation(ctx, &op); err != nil {
			return err
		}
		s.operations[op.ID] = op
	}
	return s.Store.Write(ctx, "data/usage/history-imported.json", true)
}

// StartOperation 在副作用前保存运行状态，不触发平台或模型查询。
func (s *Service) StartOperation(ctx context.Context, input *model.OperationStart) (string, error) {
	if input == nil || !model.Text(input.Platform, 40) || !model.Text(input.Action, 60) || (input.Source != "web" && input.Source != "mcp" && input.Source != "automation" && input.Source != "local") {
		return "", model.Fail("INPUT", "业务流水参数不正确")
	}
	var account *model.Account
	if input.Platform != "local" {
		if _, err := s.Store.Read(ctx, ".private/account.json", &account); err != nil {
			return "", err
		}
	}
	op := model.Operation{
		ID:       model.ID(),
		At:       time.Now().UTC().Format(time.RFC3339Nano),
		Platform: input.Platform,
		Action:   input.Action,
		Source:   input.Source,
		Status:   "running",
		Usage:    []model.TokenUsage{},
	}
	if account != nil {
		op.Account = account.UID
	}
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	if s.operations == nil {
		return "", model.Fail("STATE", "业务流水尚未初始化")
	}
	if err := s.Store.AppendOperation(ctx, &op); err != nil {
		return "", err
	}
	s.operations[op.ID] = op
	return op.ID, nil
}

// FinishOperation 不记录错误正文，避免平台响应或敏感输入进入流水。
func (s *Service) FinishOperation(ctx context.Context, id string, input *model.OperationFinish) error {
	if input == nil {
		return model.Fail("INPUT", "缺少操作结果")
	}
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	op, ok := s.operations[id]
	if !ok {
		return model.Fail("NOT_FOUND", "业务流水不存在")
	}
	if op.Status != "running" {
		return model.Fail("STATE", "业务流水已结束")
	}
	op.Status = input.Status
	op.Reference = input.Reference
	op.ErrorCode = input.ErrorCode
	op.Timings = cloneOperationTimings(input.Timings)
	if input.Account != "" {
		op.Account = input.Account
	}
	started, err := time.Parse(time.RFC3339Nano, op.At)
	if err != nil {
		return model.Internal(err)
	}
	op.DurationMS = time.Since(started).Milliseconds()
	if err := s.Store.AppendOperation(context.WithoutCancel(ctx), &op); err != nil {
		return err
	}
	s.operations[id] = op
	return nil
}

// ReportUsage 按供应商请求ID幂等；同一模型调用不能归入两条操作重复计数。
func (s *Service) ReportUsage(ctx context.Context, input *model.UsageReport) error {
	return s.reportUsage(ctx, input, "host_reported")
}
func (s *Service) reportUsage(ctx context.Context, input *model.UsageReport, source string) error {
	if input == nil {
		return model.Fail("INPUT", "缺少用量上报")
	}
	u := input.Usage
	if !model.ValidID(input.OperationID) || !model.Text(u.RequestID, 200) || !model.Text(u.Provider, 80) || !model.Text(u.Model, 100) || u.Input < 0 || u.Output < 0 || u.Input > 1000000000 || u.Output > 1000000000 {
		return model.Fail("INPUT", "用量字段不正确")
	}
	if (u.CachedInput != nil && (*u.CachedInput < 0 || *u.CachedInput > u.Input)) || (u.ReasoningOutput != nil && (*u.ReasoningOutput < 0 || *u.ReasoningOutput > u.Output)) {
		return model.Fail("INPUT", "缓存和推理用量必须是对应总量的子集")
	}
	u.Source = source
	u = cloneTokenUsage(u)
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	op, ok := s.operations[input.OperationID]
	if !ok {
		return model.Fail("NOT_FOUND", "业务流水不存在")
	}
	if op.UsageOwner != "" {
		return model.Fail("CONFLICT", "此操作已由Codex轮次统一计量，不能重复手工上报")
	}
	key := usageRequestKey{provider: u.Provider, requestID: u.RequestID}
	if owner, ok := s.usageRequests[key]; ok {
		if owner != op.ID {
			return model.Fail("CONFLICT", "同一模型调用已归属其他操作，不能重复计数")
		}
		for _, previous := range op.Usage {
			if previous.Provider == u.Provider && previous.RequestID == u.RequestID {
				if !sameUsage(&previous, &u) {
					return model.Fail("CONFLICT", "同一请求已上报不同用量")
				}
				return nil
			}
		}
	}
	if len(op.Usage) >= 1000 {
		return model.Fail("LIMIT", "单次操作用量记录超过限制")
	}
	op.Usage = append(slices.Clone(op.Usage), u)
	if err := s.Store.AppendOperation(ctx, &op); err != nil {
		return err
	}
	s.operations[op.ID] = op
	s.usageRequests[key] = op.ID
	return nil
}
func sameUsage(a, b *model.TokenUsage) bool {
	return a.Model == b.Model && a.Input == b.Input && a.Output == b.Output && sameOptional(a.CachedInput, b.CachedInput) && sameOptional(a.ReasoningOutput, b.ReasoningOutput)
}
func sameOptional(a, b *int64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// QueryOperations 只查启动加载的本地索引；返回有界页和筛选范围内的汇总。
func (s *Service) QueryOperations(ctx context.Context, input *model.OperationQuery) (*model.OperationPage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateOperationQuery(input); err != nil {
		return nil, err
	}
	collectionStatus, collectErr := s.CollectCodexUsage(ctx)
	if collectErr != nil {
		slog.Warn("读取业务Token计量失败", "error", collectErr)
	}
	if s.Store.Database != nil {
		s.operationsMu.Lock()
		excluded := []string{}
		for thread, enabled := range s.excludedUsageThreads {
			if enabled {
				excluded = append(excluded, thread)
			}
		}
		s.operationsMu.Unlock()
		result, err := s.Store.Database.OperationPage(ctx, input, excluded)
		if result != nil {
			result.CollectionStatus = collectionStatus
		}
		return result, err
	}
	page, size := input.Page, input.PageSize
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 30
	}
	result := &model.OperationPage{
		CollectionStatus: collectionStatus,
		Items:            []model.Operation{},
		Page:             page,
		PageSize:         size,
		Platforms:        []string{},
		Actions:          []string{},
	}
	s.operationsMu.Lock()
	defer s.operationsMu.Unlock()
	items := []model.Operation{}
	counted := map[usageRequestKey]bool{}
	platforms, actions := map[string]bool{}, map[string]bool{}
	for _, op := range s.operations {
		if s.excludedUsageThreads[op.UsageThread] {
			op.Usage = nil
			op.UsageOwner = ""
			op.UsageThread = ""
			op.UsageTurn = ""
		}
		platforms[op.Platform] = true
		actions[op.Action] = true
		if !operationMatches(&op, input) {
			continue
		}
		if op.UsageOwner != "" && len(op.Usage) == 0 {
			if owner, ok := s.operations[op.UsageOwner]; ok {
				op.Usage = owner.Usage
			}
		}
		items = append(items, op)
		if len(op.Usage) > 0 || op.UsageOwner != "" {
			result.Reported++
		}
		for _, u := range op.Usage {
			key := usageRequestKey{provider: u.Provider, requestID: u.RequestID}
			if counted[key] {
				continue
			}
			counted[key] = true
			result.Input += u.Input
			result.Output += u.Output
			if u.CachedInput != nil {
				result.CachedInput += *u.CachedInput
				result.CachedReports++
			}
		}
	}
	for p := range platforms {
		result.Platforms = append(result.Platforms, p)
	}
	slices.Sort(result.Platforms)
	for a := range actions {
		result.Actions = append(result.Actions, a)
	}
	slices.Sort(result.Actions)
	slices.SortFunc(items, compareOperations)
	result.Total = len(items)
	start := (page - 1) * size
	if start < len(items) {
		end := min(start+size, len(items))
		for _, op := range items[start:end] {
			op.Timings = cloneOperationTimings(op.Timings)
			op.Usage = slices.Clone(op.Usage)
			for i := range op.Usage {
				op.Usage[i] = cloneTokenUsage(op.Usage[i])
			}
			result.Items = append(result.Items, op)
		}
	}
	return result, nil
}

func cloneOperationTimings(source *model.OperationTimings) *model.OperationTimings {
	if source == nil {
		return nil
	}
	target := *source
	if source.GenerationMS != nil {
		elapsed := *source.GenerationMS
		target.GenerationMS = &elapsed
	}
	if source.AccountCheckMS != nil {
		elapsed := *source.AccountCheckMS
		target.AccountCheckMS = &elapsed
	}
	if source.SendMS != nil {
		elapsed := *source.SendMS
		target.SendMS = &elapsed
	}
	return &target
}
func compareOperations(a, b model.Operation) int {
	if a.At == b.At {
		return strings.Compare(b.ID, a.ID)
	}
	left, _ := time.Parse(time.RFC3339Nano, a.At)
	right, _ := time.Parse(time.RFC3339Nano, b.At)
	return right.Compare(left)
}
func validateOperationQuery(input *model.OperationQuery) error {
	if input == nil {
		return model.Fail("INPUT", "缺少查询条件")
	}
	if input.Page < 0 || input.Page > 1000000 || input.PageSize < 0 || input.PageSize > 100 {
		return model.Fail("INPUT", "页码或页大小不正确")
	}
	for _, v := range []*string{input.Platform, input.Account, input.Action, input.Status} {
		if v != nil && !model.Text(*v, 100) {
			return model.Fail("INPUT", "筛选值不能为空且最多100字")
		}
	}
	for _, v := range []*string{input.From, input.To} {
		if v != nil {
			if _, err := time.Parse(time.RFC3339Nano, *v); err != nil {
				return model.Fail("INPUT", "日期必须包含时区")
			}
		}
	}
	if input.From != nil && input.To != nil {
		from, _ := time.Parse(time.RFC3339Nano, *input.From)
		to, _ := time.Parse(time.RFC3339Nano, *input.To)
		if from.After(to) {
			return model.Fail("INPUT", "开始时间不能晚于结束时间")
		}
	}
	return nil
}
func operationMatches(op *model.Operation, q *model.OperationQuery) bool {
	if (q.Platform != nil && op.Platform != *q.Platform) || (q.Account != nil && op.Account != *q.Account) || (q.Action != nil && op.Action != *q.Action) || (q.Status != nil && op.Status != *q.Status) || (q.HasUsage != nil && (len(op.Usage) > 0 || op.UsageOwner != "") != *q.HasUsage) {
		return false
	}
	at, err := time.Parse(time.RFC3339Nano, op.At)
	if err != nil {
		return false
	}
	if q.From != nil {
		from, _ := time.Parse(time.RFC3339Nano, *q.From)
		if at.Before(from) {
			return false
		}
	}
	if q.To != nil {
		to, _ := time.Parse(time.RFC3339Nano, *q.To)
		if at.After(to) {
			return false
		}
	}
	return true
}

// OperationErrorCode 将错误归类为公开业务码，不保存任意错误文本。
func OperationErrorCode(err error) string {
	var e *model.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return "INTERNAL"
}

func cloneTokenUsage(u model.TokenUsage) model.TokenUsage {
	u.RelatedOperations = slices.Clone(u.RelatedOperations)
	if u.CachedInput != nil {
		value := *u.CachedInput
		u.CachedInput = &value
	}
	if u.ReasoningOutput != nil {
		value := *u.ReasoningOutput
		u.ReasoningOutput = &value
	}
	return u
}

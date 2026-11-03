package core

import (
	"context"
	"encoding/json"
	"errors"
	"gpt-robot/businesshost"
	"gpt-robot/model"
	"gpt-robot/modelprovider"
	"gpt-robot/platforms/bilibili"
	"gpt-robot/usage/codex"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type ServiceInterface interface {
	ProactiveStatus(context.Context) (*model.ProactiveOverview, error)
	SaveProactiveConfig(context.Context, *model.ProactiveConfig) (*model.ProactiveOverview, error)
	ProactiveTick(context.Context) error
	RecordMaterialExperience(context.Context, *model.MaterialExperienceInput) (*model.MaterialExperienceResult, error)
	ConversationStatus(context.Context) (*model.ConversationStatus, error)
	SaveConversationConfig(context.Context, *model.ConversationConfig) (*model.ConversationStatus, error)
	OperationPreviews(context.Context, []model.Operation) (map[string]model.ActivityPreview, error)
	BrowsingOverview(context.Context) (*model.BrowsingOverview, error)
	SaveBrowsing(context.Context, *model.BrowsingConfig) (*model.BrowsingOverview, error)
	BrowseNow(context.Context) error
	BrowsingTick(context.Context) error
	GenerationSafety(context.Context) (*model.GenerationSafetyStatus, error)
	SaveGenerationSafety(context.Context, *model.GenerationSafetyConfig) (*model.GenerationSafetyStatus, error)
	ModelProviderStatus(context.Context) (*model.ModelProviderStatus, error)
	ConnectModelHarness(context.Context, *model.HarnessConnectInput) (*model.ModelProviderStatus, error)
	CodexModels(context.Context) ([]model.ModelChoice, error)
	SaveModelProviderConfig(context.Context, *model.ModelProviderInput) (*model.ModelProviderStatus, error)
	RecordDevelopment(context.Context, *model.DevelopmentInput) error
	DevelopmentStatus(context.Context) (*model.DevelopmentOverview, error)
	DiaryOverview(context.Context) (*model.DiaryOverview, error)
	SaveDiaryConfig(context.Context, *model.DiarySettings) (*model.DiaryOverview, error)
	GenerateDiary(context.Context, *model.DiaryGenerateInput) (*model.DiaryEntry, error)
	PrivateMemories(context.Context, *model.PrivateMemoryQuery) (*model.PrivateMemoryPage, error)
	ForgetPrivateMemory(context.Context, string) error
	CharacterStatus(context.Context) (*model.CharacterState, error)
	RobotStatus(context.Context) (*model.RobotState, error)
	ControlRobot(context.Context, *model.RobotControlInput) (*model.RobotState, error)
	SetRobot(context.Context, bool) (*model.RobotState, error)
	PrivateOverview(context.Context) (*model.PrivateOverview, error)
	SavePrivateConfig(context.Context, *model.PrivateSettings) (*model.PrivateOverview, error)
	PrivateTick(context.Context) error
	ResumePrivateReplies(context.Context) (*model.PrivateOverview, error)
	SendPrivate(context.Context, *model.PrivateSendInput) (*model.PrivateOutbox, error)
	RefreshPrivate(context.Context) (*model.PrivateOverview, error)
	AutoOverview(context.Context) (*model.AutoReplyOverview, error)
	SaveAutoConfig(context.Context, *model.AutoReplyConfig) (*model.AutoReplyOverview, error)
	AutoTick(context.Context) error
	RunAutomation(context.Context)
	StartOperation(context.Context, *model.OperationStart) (string, error)
	FinishOperation(context.Context, string, *model.OperationFinish) error
	ReportUsage(context.Context, *model.UsageReport) error
	CollectCodexUsage(context.Context) (string, error)
	QueryOperations(context.Context, *model.OperationQuery) (*model.OperationPage, error)
	Initialize(context.Context) error
	Credentials(context.Context) (*model.Credentials, error)
	Permissions(context.Context) (model.Permissions, error)
	SavePermissions(context.Context, *model.Permissions) (model.Permissions, error)
	SavePersona(context.Context, string) error
	Context(context.Context) (*model.ContextResult, error)
	Search(context.Context, string) (*model.SearchResult, error)
	Note(context.Context, *model.NoteInput) (model.Note, error)
	Drafts(context.Context) ([]model.Draft, error)
	Overview(context.Context) (*model.Overview, error)
	Account(context.Context) (model.AccountResult, error)
	ImportSession(context.Context, *model.ImportSessionInput) (model.AccountResult, error)
	QR(context.Context) (string, error)
	Poll(context.Context) (model.LoginResult, error)
	Comments(context.Context, *model.CommentsInput) (*model.CommentsResult, error)
	Prepare(context.Context, *model.DraftInput) (model.Draft, error)
	PrepareDynamic(context.Context, string) (model.Draft, error)
	Send(context.Context, string) (model.Draft, error)
	Cancel(context.Context, string) (model.Draft, error)
	Recheck(context.Context, string) (model.Draft, error)
}
type loginAttempt struct {
	key      string
	expires  time.Time
	nextPoll time.Time
}
type Service struct {
	browsingWorkerMu sync.Mutex
	videoRunMu       sync.Mutex
	videoCancel      context.CancelFunc
	proactiveMu      sync.Mutex
	proactiveConfig  model.ProactiveConfig
	proactiveState   model.ProactiveState
	ProactiveNow     func() time.Time
	VideoReader      model.VideoMaterialReader
	materialMu       sync.Mutex
	// ExpressionDraw 为系统表达策略提供随机数，默认使用加密随机源；测试可注入确定结果。
	ExpressionDraw func(int) int
	// ExpressionWait 为分条发送提供可取消等待，不产生模型调用。
	ExpressionWait           func(context.Context, time.Duration) error
	stickerMu                sync.Mutex
	conversationMu           sync.Mutex
	browsingMu               sync.Mutex
	browsingConfig           model.BrowsingConfig
	browsingState            model.BrowsingState
	generationMu             sync.Mutex
	generationSafety         model.GenerationSafetyConfig
	generationState          model.GenerationSafetyState
	modelRouter              *modelprovider.Router
	modelConfigMu            sync.Mutex
	PrivatePush              model.PrivatePushSource
	privatePushWake          chan struct{}
	privatePushNextCheck     time.Time
	privatePushSince         time.Time
	privatePushStatus        model.PrivatePushStatus
	privatePushPendingSource string
	privateDeferredSource    string
	privateCheckSource       string
	privatePushPending       bool
	privatePushKeys          map[string]bool
	// DevelopmentNow 成长时钟默认time.Now，仅用于解释新经历与每日限幅。
	DevelopmentNow func() time.Time
	developmentMu  sync.Mutex
	development    model.DevelopmentState
	// DiaryNow 日记业务的时钟依赖，默认time.Now，日期统一按北京时间解释。
	DiaryNow     func() time.Time
	diaryMu      sync.Mutex
	diaryConfig  model.DiaryConfig
	diaryChecked string
	// DiarySources 已授权平台的本地经历适配器；不对HTTP/MCP暴露任意素材注入。
	DiarySources             []model.DiaryExperienceSource
	DiaryGenerator           model.DiaryGenerator
	nativeUsageFiles         map[string]string
	privateMemoryMu          sync.Mutex
	privateMemories          map[string]*model.PrivateMemoryState
	robotControlMu           sync.Mutex
	characterMu              sync.Mutex
	character                model.CharacterState
	Generator                model.ReplyGenerator
	robotMu                  sync.Mutex
	robot                    model.RobotState
	robotCancel              context.CancelFunc
	privateMu                sync.Mutex
	privateConfig            model.PrivateConfig
	privateState             model.PrivateState
	Store                    *Store
	Bili                     bilibili.ClientInterface
	mu                       sync.Mutex
	login                    *loginAttempt
	operationsMu             sync.Mutex
	operations               map[string]model.Operation
	usageRequests            map[usageRequestKey]string
	collectionMu             sync.Mutex
	codexCollector           codex.CollectorInterface
	codexState               codex.State
	codexThreads             []codex.Thread
	discoveredAt             time.Time
	discoveredOperationCount int
	excludedUsageThreads     map[string]bool
	autoMu                   sync.Mutex
	autoRunMu                sync.Mutex
	autoConfig               model.AutoReplyConfig
	autoState                model.AutoReplyState
	autoWake                 chan struct{}
}

var _ ServiceInterface = (*Service)(nil)

func NewService(root string, bili bilibili.ClientInterface) *Service {
	router := modelprovider.New(root)
	generator := businesshost.NewGenerator(root, router)
	s := &Service{
		ExpressionDraw:  randomExpressionDraw,
		ExpressionWait:  waitExpression,
		modelRouter:     router,
		DevelopmentNow:  time.Now,
		ProactiveNow:    time.Now,
		DiaryNow:        time.Now,
		DiaryGenerator:  generator,
		Store:           NewStore(root),
		Bili:            bili,
		Generator:       generator,
		codexCollector:  codex.New(codexHome()),
		autoWake:        make(chan struct{}, 1),
		privatePushWake: make(chan struct{}, 1),
		privatePushKeys: map[string]bool{},
	}
	if s.Bili == nil {
		s.Bili = bilibili.New(s.Credentials, nil)
	}
	s.VideoReader = businesshost.NewVideoReader(root, s.modelRouter.Config, root)
	s.PrivatePush = s.Bili
	return s
}
func (s *Service) Initialize(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.Store.InitializeDatabase(ctx); err != nil {
		return err
	}
	if err := s.initializeGenerationSafety(ctx); err != nil {
		return err
	}
	if err := s.initializeModelProvider(ctx); err != nil {
		return err
	}
	if err := s.initializeConversations(ctx); err != nil {
		return err
	}
	text, err := s.Store.Text(ctx, "data/character/persona.md")
	if err != nil {
		return err
	}
	if text == "" {
		if err = s.Store.WriteText(ctx, "data/character/persona.md", "# 我是谁\n我是一个友善、好奇的AI角色。\n\n# 说话方式\n中文、简短、自然；不编造共同经历。\n"); err != nil {
			return err
		}
	}
	p := model.DefaultPermissions()
	found, err := s.Store.Read(ctx, "config/permissions.json", &p)
	if err != nil {
		return err
	}
	if err = p.Validate(); err != nil {
		return err
	}
	if !found {
		if err = s.Store.Write(ctx, "config/permissions.json", p); err != nil {
			return err
		}
	}
	ds, err := s.Drafts(ctx)
	if err != nil {
		return err
	}
	changed := false
	for i := range ds {
		if ds[i].Status == "sending" {
			ds[i].Status = "unknown"
			ds[i].Error = "上次发送期间服务停止，必须人工回查"
			changed = true
		}
	}
	if changed {
		if err := s.Store.Write(ctx, "data/events/drafts.json", ds); err != nil {
			return err
		}
	}
	if err := s.initializeOperations(ctx); err != nil {
		return err
	}
	if err := s.initializeAuto(ctx); err != nil {
		return err
	}
	if err := s.initializeRobot(ctx); err != nil {
		return err
	}
	if err := s.projectCoreName(ctx); err != nil {
		return err
	}
	if err := s.initializeCharacter(ctx); err != nil {
		return err
	}
	if err := s.initializeDiary(ctx); err != nil {
		return err
	}
	if err := s.initializeDevelopment(ctx); err != nil {
		return err
	}
	if err := s.initializeBrowsing(ctx); err != nil {
		return err
	}
	return s.initializeProactive(ctx)
}
func (s *Service) Credentials(ctx context.Context) (*model.Credentials, error) {
	var c *model.Credentials
	_, err := s.Store.Read(ctx, ".private/bilibili.json", &c)
	return c, err
}
func (s *Service) Permissions(ctx context.Context) (model.Permissions, error) {
	p := model.DefaultPermissions()
	_, err := s.Store.Read(ctx, "config/permissions.json", &p)
	if err != nil {
		return p, err
	}
	return p, p.Validate()
}
func (s *Service) SavePermissions(ctx context.Context, input *model.Permissions) (model.Permissions, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := input.Validate(); err != nil {
		return model.Permissions{}, err
	}
	p := *input
	if p.AllowedVideos == nil {
		p.AllowedVideos = []string{}
	}
	if p.AllowedUsers == nil {
		p.AllowedUsers = []string{}
	}
	if p.BlockedUsers == nil {
		p.BlockedUsers = []string{}
	}
	if err := s.Store.Write(ctx, "config/permissions.json", p); err != nil {
		return p, err
	}
	_, err := s.Store.Event(ctx, "permissions", "用户更新了互动权限", nil)
	return p, err
}
func (s *Service) SavePersona(ctx context.Context, persona string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	persona = strings.TrimSpace(persona)
	if !model.Text(persona, 20000) {
		return model.Fail("INPUT", "人设不能为空且最多20000字")
	}
	updated, err := s.namedPersona(ctx, persona)
	if err != nil {
		return err
	}
	if !model.Text(updated, 20000) {
		return model.Fail("INPUT", "核心名字加入后人设超过20000字")
	}
	persona = updated
	if err := s.Store.WriteText(ctx, "data/character/persona.md", persona); err != nil {
		return err
	}
	s.modelRouter.CloseConversations()
	_, err = s.Store.Event(ctx, "persona", "用户更新了人设", nil)
	return err
}
func (s *Service) Context(ctx context.Context) (*model.ContextResult, error) {
	persona, err := s.Store.Text(ctx, "data/character/persona.md")
	if err != nil {
		return nil, err
	}
	notes, err := s.Store.Notes(ctx)
	if err != nil {
		return nil, err
	}
	events, err := s.Store.Events(ctx, 8)
	if err != nil {
		return nil, err
	}
	state, err := s.CharacterStatus(ctx)
	if err != nil {
		return nil, err
	}
	memories := []model.Note{}
	for _, n := range notes {
		if n.Kind == "memory" {
			n = compactPublicNote(&n)
			memories = append(memories, n)
		}
	}
	if len(memories) > 5 {
		memories = memories[len(memories)-5:]
	}
	for i := range events {
		events[i].Summary = model.Clip(events[i].Summary, 200)
		events[i].Data = nil
	}
	result := &model.ContextResult{Persona: model.Clip(persona, 800), PersonaTruncated: utf8.RuneCountInString(persona) > 800, Description: model.Clip(state.Description, 240), Memories: memories, RecentEvents: events}
	return result, nil
}
func (s *Service) Search(ctx context.Context, query string) (*model.SearchResult, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if !model.Text(query, 100) {
		return nil, model.Fail("INPUT", "检索词为1至100字")
	}
	events, err := s.Store.Events(ctx, 0)
	if err != nil {
		return nil, err
	}
	notes, err := s.Store.Notes(ctx)
	if err != nil {
		return nil, err
	}
	result := &model.SearchResult{Events: []model.Event{}, Notes: []model.Note{}}
	for _, e := range events {
		b, _ := json.Marshal(e)
		if strings.Contains(strings.ToLower(string(b)), query) {
			result.Events = append(result.Events, e)
		}
	}
	if len(result.Events) > 3 {
		result.Events = result.Events[len(result.Events)-3:]
	}
	for _, n := range notes {
		if strings.Contains(strings.ToLower(n.Title+" "+n.Text+" "+n.At), query) {
			result.Notes = append(result.Notes, n)
		}
	}
	if len(result.Notes) > 3 {
		result.Notes = result.Notes[len(result.Notes)-3:]
	}
	for i := range result.Notes {
		result.Notes[i] = compactPublicNote(&result.Notes[i])
	}
	for i := range result.Events {
		result.Events[i].Summary = model.Clip(result.Events[i].Summary, 200)
		result.Events[i].Data = nil
	}
	return result, nil
}
func (s *Service) Note(ctx context.Context, input *model.NoteInput) (model.Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if input == nil {
		return model.Note{}, model.Fail("INPUT", "缺少记忆参数")
	}
	p := *input
	p.Title = strings.TrimSpace(p.Title)
	p.Text = strings.TrimSpace(p.Text)
	if (p.Kind != "memory" && p.Kind != "diary") || !model.Text(p.Title, 100) || !model.Text(p.Text, 4000) || len(p.Sources) < 1 || len(p.Sources) > 20 {
		return model.Note{}, model.Fail("INPUT", "记忆参数不正确")
	}
	events, err := s.Store.Events(ctx, 0)
	if err != nil {
		return model.Note{}, err
	}
	ids := map[string]bool{}
	for _, e := range events {
		ids[e.ID] = true
	}
	for _, id := range p.Sources {
		if !model.ValidID(id) || !ids[id] {
			return model.Note{}, model.Fail("SOURCE", "来源必须是现有经历ID，不能虚构")
		}
	}
	n := model.Note{ID: model.ID(), At: time.Now().UTC().Format(time.RFC3339Nano), Kind: p.Kind, Title: p.Title, Text: p.Text, Sources: p.Sources}
	notes, err := s.Store.Notes(ctx)
	if err != nil {
		return n, err
	}
	notes = append(notes, n)
	if err = s.Store.Write(ctx, "data/memory/notes.json", notes); err != nil {
		return n, err
	}
	if n.Kind == "diary" {
		err = s.Store.WriteText(ctx, "data/diary/"+n.At[:10]+"-"+n.ID+".md", "# "+n.Title+"\n\n"+n.Text+"\n\n来源："+strings.Join(n.Sources, ", "))
	}
	return n, err
}
func (s *Service) Drafts(ctx context.Context) ([]model.Draft, error) {
	ds := []model.Draft{}
	_, err := s.Store.Read(ctx, "data/events/drafts.json", &ds)
	return ds, err
}
func (s *Service) Overview(ctx context.Context) (*model.Overview, error) {
	persona, err := s.Store.Text(ctx, "data/character/persona.md")
	if err != nil {
		return nil, err
	}
	p, err := s.Permissions(ctx)
	if err != nil {
		return nil, err
	}
	var account *model.Account
	if _, err = s.Store.Read(ctx, ".private/account.json", &account); err != nil {
		return nil, err
	}
	ds, err := s.Drafts(ctx)
	if err != nil {
		return nil, err
	}
	events, err := s.Store.Events(ctx, 150)
	if err != nil {
		return nil, err
	}
	notes, err := s.Store.Notes(ctx)
	if err != nil {
		return nil, err
	}
	if len(notes) > 100 {
		notes = notes[len(notes)-100:]
	}
	slices.Reverse(ds)
	slices.Reverse(events)
	slices.Reverse(notes)
	s.autoMu.Lock()
	autoEnabled, autoPaused := s.autoConfig.Enabled, s.autoState.Paused
	s.autoMu.Unlock()
	robot, err := s.RobotStatus(ctx)
	if err != nil {
		return nil, err
	}
	result := &model.Overview{RobotStatus: robot.Status, RobotEnabled: robot.Enabled, AutoEnabled: autoEnabled, AutoPaused: autoPaused, Persona: persona, Permissions: p, Account: account, Drafts: ds, Events: events, Notes: notes, DataPath: s.Store.Root + "/data"}
	return result, nil
}
func (s *Service) account(ctx context.Context) (model.AccountResult, error) {
	a, err := s.Bili.Status(ctx)
	if err != nil {
		var business *model.Error
		if errors.As(err, &business) && (business.Code == "LOGIN" || business.Code == "BILI_-101") {
			err = s.Store.Write(ctx, ".private/account.json", nil)
			return model.AccountResult{}, err
		}
		return model.AccountResult{}, err
	}
	if err = s.Store.Write(ctx, ".private/account.json", a); err != nil {
		return model.AccountResult{}, err
	}
	result := model.AccountResult{LoggedIn: true, Account: a}
	return result, nil
}
func (s *Service) Account(ctx context.Context) (model.AccountResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.account(ctx)
}
func (s *Service) ImportSession(ctx context.Context, input *model.ImportSessionInput) (model.AccountResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if input == nil {
		return model.AccountResult{}, model.Fail("INPUT", "缺少会话参数")
	}
	cookie, expectedUID := input.Cookie, input.ExpectedUID
	if len(input.UserAgent) > 512 || strings.ContainsAny(input.UserAgent, "\r\n") {
		return model.AccountResult{}, model.Fail("INPUT", "User-Agent格式不正确")
	}
	if !model.Text(cookie, 30000) || !model.ValidNumber(expectedUID) {
		return model.AccountResult{}, model.Fail("INPUT", "会话或UID格式不正确")
	}
	cookies := map[string]string{}
	for _, line := range strings.Split(cookie, "\n") {
		cols := strings.Split(line, "\t")
		if len(cols) > 1 && bilibili.SessionCookieAllowed(strings.TrimSpace(cols[0])) {
			cookies[strings.TrimSpace(cols[0])] = strings.TrimSpace(cols[1])
		}
		line = strings.TrimPrefix(strings.TrimPrefix(line, "Cookie:"), "cookie:")
		for _, part := range strings.Split(line, ";") {
			k, v, ok := strings.Cut(part, "=")
			k = strings.TrimSpace(k)
			if ok && bilibili.SessionCookieAllowed(k) {
				cookies[k] = strings.TrimSpace(v)
			}
		}
	}
	if cookies["SESSDATA"] == "" || cookies["bili_jct"] == "" {
		return model.AccountResult{}, model.Fail("CREDENTIAL", "需要SESSDATA和bili_jct，未保存任何内容")
	}
	candidate := &model.Credentials{UID: expectedUID, Cookies: cookies, UserAgent: input.UserAgent}
	probe := bilibili.New(func(context.Context) (*model.Credentials, error) { return candidate, nil }, nil)
	a, err := probe.Status(ctx)
	if err != nil {
		return model.AccountResult{}, err
	}
	if a.UID != expectedUID {
		return model.AccountResult{}, model.Fail("ACCOUNT", "登录账号不是指定小号，未保存")
	}
	if err = s.checkPendingAccount(ctx, a.UID); err != nil {
		return model.AccountResult{}, err
	}
	cookies["DedeUserID"] = a.UID
	if err = s.Store.Write(ctx, ".private/bilibili.json", candidate); err != nil {
		return model.AccountResult{}, err
	}
	if err = s.Store.Write(ctx, ".private/account.json", a); err != nil {
		return model.AccountResult{}, err
	}
	_, err = s.Store.Event(ctx, "login", "导入并验证已有B站会话", map[string]any{"uid": a.UID})
	result := model.AccountResult{LoggedIn: true, Account: a}
	return result, err
}
func (s *Service) checkPendingAccount(ctx context.Context, uid string) error {
	ds, err := s.Drafts(ctx)
	if err != nil {
		return err
	}
	for _, d := range ds {
		if d.Status == "pending" && d.Account != uid {
			return model.Fail("ACCOUNT", "存在其他账号的待审草稿，请先取消再切换账号")
		}
	}
	return nil
}
func (s *Service) QR(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	url, key, err := s.Bili.QR(ctx)
	if err != nil {
		return "", err
	}
	s.login = &loginAttempt{key: key, expires: time.Now().Add(180 * time.Second)}
	return url, nil
}
func (s *Service) Poll(ctx context.Context) (model.LoginResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.login
	if a == nil || time.Now().After(a.expires) {
		return model.LoginResult{}, model.Fail("QR", "二维码已过期，请重新生成")
	}
	if time.Now().Before(a.nextPoll) {
		return model.LoginResult{}, model.Fail("QR_WAIT", "请稍候再检查扫码结果")
	}
	a.nextPoll = time.Now().Add(3 * time.Second)
	p, err := s.Bili.Poll(ctx, a.key)
	if err != nil {
		return model.LoginResult{}, err
	}
	result := model.LoginResult{Code: p.Code}
	if p.Code == 0 && p.Credentials != nil {
		if err = s.checkPendingAccount(ctx, p.Credentials.UID); err != nil {
			return result, err
		}
		if err = s.Store.Write(ctx, ".private/bilibili.json", p.Credentials); err != nil {
			return result, err
		}
		s.login = nil
		result.AccountResult, err = s.account(ctx)
		if err != nil {
			return result, err
		}
		_, err = s.Store.Event(ctx, "login", "B站扫码登录完成", nil)
	}
	return result, err
}
func (s *Service) Comments(ctx context.Context, input *model.CommentsInput) (*model.CommentsResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if input == nil || !model.ValidBV(input.BVID) {
		return nil, model.Fail("INPUT", "视频BV号不正确")
	}
	page := 1
	root := ""
	if input.Page != nil {
		page = *input.Page
	}
	if input.Root != nil {
		root = *input.Root
		if !model.ValidNumber(root) {
			return nil, model.Fail("INPUT", "根评论ID不正确")
		}
	}
	if page < 1 || page > 500 {
		return nil, model.Fail("INPUT", "页码为1至500")
	}
	v, err := s.Bili.Video(ctx, input.BVID)
	if err != nil {
		return nil, err
	}
	p, err := s.Bili.Comments(ctx, v.Aid, page, root)
	if err != nil {
		return nil, err
	}
	if root == "" {
		root = "0"
	}
	e, err := s.Store.Event(ctx, "comments", "读取《"+v.Title+"》评论", map[string]any{"bvid": v.BVID, "page": page, "root": root, "comments": p.Comments})
	if err != nil {
		return nil, err
	}
	credentials, err := s.Credentials(ctx)
	if err != nil {
		return nil, err
	}
	if credentials != nil && credentials.UID != "" {
		experience := model.DevelopmentInput{Platform: "bilibili", Account: credentials.UID, Visibility: "public", Source: "video:" + v.BVID, At: e.At, Text: model.Clip(v.Title, 500)}
		if model.Text(experience.Text, 500) {
			if err := s.RecordDevelopment(ctx, &experience); err != nil {
				return nil, err
			}
		}
	}
	result := &model.CommentsResult{Video: v, CommentPage: p, EventID: e.ID}
	return result, nil
}
func (s *Service) operationPermissions(ctx context.Context, kind, bvid string) (model.Permissions, error) {
	p, err := s.Permissions(ctx)
	if err != nil {
		return p, err
	}
	if !p.Enabled && kind != "browsing-comment" {
		return p, model.Fail("DENIED", "手动发送已关闭，请在回复工作台的发送设置中开启")
	}
	if kind == "dynamic" {
		if !p.AllowDynamic {
			return p, model.Fail("DENIED", "动态发布未启用")
		}
	} else if kind != "browsing-comment" && !slices.Contains(p.AllowedVideos, bvid) {
		return p, model.Fail("DENIED", "该视频不在允许回复列表")
	}
	return p, nil
}
func (s *Service) permitted(ctx context.Context, d *model.Draft, uid string) (model.Permissions, error) {
	kind := d.Kind
	if s.autoBrowsingTarget(d) {
		kind = "browsing-comment"
	}
	p, err := s.operationPermissions(ctx, kind, d.BVID)
	if err != nil {
		return p, err
	}
	if d.Account != uid {
		return p, model.Fail("ACCOUNT", "草稿账号与当前登录账号不同")
	}
	if slices.Contains(p.BlockedUsers, d.TargetUser) {
		return p, model.Fail("DENIED", "该用户在禁止回复列表")
	}
	if d.Kind != "dynamic" && len(p.AllowedUsers) > 0 && !slices.Contains(p.AllowedUsers, d.TargetUser) {
		return p, model.Fail("DENIED", "该评论用户不在允许列表")
	}
	return p, nil
}
func activeDraft(status string) bool {
	return slices.Contains([]string{"pending", "sending", "submitted", "visible", "restricted", "pending_review", "unknown"}, status)
}
func (s *Service) Prepare(ctx context.Context, input *model.DraftInput) (model.Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if input == nil {
		return model.Draft{}, model.Fail("INPUT", "缺少草稿参数")
	}
	p := *input
	if err := p.Normalize(); err != nil {
		return model.Draft{}, err
	}
	if _, err := s.operationPermissions(ctx, "comment", p.BVID); err != nil {
		return model.Draft{}, err
	}
	a, err := s.Bili.Status(ctx)
	if err != nil {
		return model.Draft{}, err
	}
	ds, err := s.Drafts(ctx)
	if err != nil {
		return model.Draft{}, err
	}
	for _, d := range ds {
		if d.Kind != "dynamic" && d.Account == a.UID && d.BVID == p.BVID && d.Root == p.Root && d.Parent == p.Parent && d.Text == p.Text && activeDraft(d.Status) {
			_, err = s.permitted(ctx, &d, a.UID)
			return d, err
		}
	}
	v, err := s.Bili.Video(ctx, p.BVID)
	if err != nil {
		return model.Draft{}, err
	}
	target := model.Comment{}
	if p.Parent != "0" {
		target, err = s.Bili.Target(ctx, v.Aid, p.Root, p.Parent)
		if err != nil {
			return model.Draft{}, err
		}
	}
	d := model.Draft{BVID: p.BVID, Root: p.Root, Parent: p.Parent, Text: p.Text, ID: model.ID(), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Account: a.UID, TargetUser: target.UID, TargetText: target.Text, VideoTitle: v.Title, Status: "pending"}
	if p.Parent == "0" {
		d.TargetText = "在视频下发布一级评论"
	}
	if _, err = s.permitted(ctx, &d, a.UID); err != nil {
		return d, err
	}
	ds = append(ds, d)
	if err = s.Store.Write(ctx, "data/events/drafts.json", ds); err != nil {
		return d, err
	}
	_, err = s.Store.Event(ctx, "draft", "生成待审核回复", map[string]any{"draftId": d.ID, "bvid": d.BVID, "parent": d.Parent, "text": d.Text})
	return d, err
}
func (s *Service) PrepareDynamic(ctx context.Context, text string) (model.Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	text = strings.TrimSpace(text)
	if !model.Text(text, 1000) {
		return model.Draft{}, model.Fail("INPUT", "动态正文为1至1000字")
	}
	if _, err := s.operationPermissions(ctx, "dynamic", ""); err != nil {
		return model.Draft{}, err
	}
	a, err := s.Bili.Status(ctx)
	if err != nil {
		return model.Draft{}, err
	}
	ds, err := s.Drafts(ctx)
	if err != nil {
		return model.Draft{}, err
	}
	d := model.Draft{ID: model.ID(), Kind: "dynamic", Root: "0", Parent: "0", Text: text, Account: a.UID, TargetText: "发布到自己的B站空间", VideoTitle: "文字动态", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Status: "pending"}
	if _, err = s.permitted(ctx, &d, a.UID); err != nil {
		return d, err
	}
	for _, old := range ds {
		if old.Kind == "dynamic" && old.Account == a.UID && old.Text == text && activeDraft(old.Status) {
			return old, nil
		}
	}
	ds = append(ds, d)
	if err = s.Store.Write(ctx, "data/events/drafts.json", ds); err != nil {
		return d, err
	}
	_, err = s.Store.Event(ctx, "draft", "生成文字动态草稿", map[string]any{"draftId": d.ID, "text": d.Text, "account": a.UID})
	return d, err
}
func draftIndex(ds []model.Draft, id string) int {
	for i := range ds {
		if ds[i].ID == id {
			return i
		}
	}
	return -1
}
func (s *Service) Send(ctx context.Context, id string) (model.Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ds, err := s.Drafts(ctx)
	if err != nil {
		return model.Draft{}, err
	}
	i := draftIndex(ds, id)
	if i < 0 {
		return model.Draft{}, model.Fail("MISSING", "草稿不存在")
	}
	d := &ds[i]
	if d.Status != "pending" {
		return *d, model.Fail("STATE", "草稿已处理，不能重复发送")
	}
	if model.ContainsSensitiveText(d.Text, s.generationSecrets(ctx)...) {
		return *d, model.Fail("SENSITIVE_OUTPUT", "正文包含疑似凭证，未发送")
	}
	p, err := s.permitted(ctx, d, d.Account)
	if err != nil {
		return *d, err
	}
	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	last := time.Time{}
	attempts := 0
	for _, old := range ds {
		if old.Account != d.Account || old.AttemptAt == "" {
			continue
		}
		at, e := time.Parse(time.RFC3339Nano, old.AttemptAt)
		if e == nil && at.After(last) {
			last = at
		}
		if old.AutoCommentID == "" && strings.HasPrefix(old.AttemptAt, today) {
			attempts++
		}
	}
	if now.Sub(last) < time.Duration(p.CooldownSeconds)*time.Second {
		return *d, model.Fail("COOLDOWN", "尚在发送冷却期，请稍后再试")
	}
	if d.AutoCommentID != "" {
		if err := s.autoSendAllowed(d, ds, now); err != nil {
			return *d, err
		}
	} else if attempts >= p.DailyLimit {
		return *d, model.Fail("LIMIT", "已达今日发送尝试上限（UTC日期）")
	}
	a, err := s.Bili.Status(ctx)
	if err != nil {
		return *d, err
	}
	if _, err = s.permitted(ctx, d, a.UID); err != nil {
		return *d, err
	}
	v := model.Video{}
	if d.Kind != "dynamic" {
		v, err = s.Bili.Video(ctx, d.BVID)
		if err != nil {
			return *d, err
		}
		if d.Parent != "0" {
			t, e := s.Bili.Target(ctx, v.Aid, d.Root, d.Parent)
			if e != nil {
				return *d, e
			}
			if t.UID != d.TargetUser || t.Text != d.TargetText {
				return *d, model.Fail("TARGET", "目标评论已变化，请取消并重新生成草稿")
			}
		}
	}
	if d.AutoCommentID != "" {
		if err := s.autoSendAllowed(d, ds, time.Now().UTC()); err != nil {
			return *d, err
		}
	}
	d.Status = "sending"
	d.AttemptAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err = s.Store.Write(ctx, "data/events/drafts.json", ds); err != nil {
		return *d, err
	}
	// 发送后即便客户端断开，也必须持久保存回执，不能丢失结果。
	persist := context.WithoutCancel(ctx)
	var receipt model.CommentReceipt
	if d.Kind == "dynamic" {
		d.RPID, err = s.Bili.SendDynamic(ctx, d.Text)
	} else {
		receipt, err = s.Bili.Send(ctx, v.Aid, d.Text, d.Root, d.Parent, d.BVID)
		d.RPID = receipt.ID
		d.PlatformMessage = receipt.Message
		d.PlatformState = receipt.State
	}
	if err != nil {
		d.Status = "unknown"
		var business *model.Error
		if errors.As(err, &business) {
			d.Error = business.Message
			if strings.HasPrefix(business.Code, "BILI_") {
				d.Status = "failed"
			}
		} else {
			d.Error = "发送结果不明，请人工回查"
		}
		if _, logErr := s.Store.Event(persist, "send_result", d.Error, map[string]any{"draftId": d.ID, "status": d.Status}); logErr != nil {
			return *d, logErr
		}
		return *d, s.Store.Write(persist, "data/events/drafts.json", ds)
	}
	d.Status = "submitted"
	if err = s.Store.Write(persist, "data/events/drafts.json", ds); err != nil {
		return *d, err
	}
	if _, err = s.Store.Event(persist, "sent", "已向B站提交回复", map[string]any{"draftId": d.ID, "bvid": d.BVID, "rpid": d.RPID, "text": d.Text, "platformMessage": d.PlatformMessage, "platformState": d.PlatformState}); err != nil {
		return *d, err
	}
	if receipt.State != nil && (*receipt.State == 17 || *receipt.State == 11) {
		applyVisibility(d, model.Visibility{Found: true, State: receipt.State})
		return *d, s.Store.Write(persist, "data/events/drafts.json", ds)
	}
	visible, checkErr := s.checkVisible(ctx, d, &v)
	if checkErr != nil {
		d.Error = "已提交，后续回查异常，请检查"
	} else {
		applyVisibility(d, visible)
	}
	return *d, s.Store.Write(persist, "data/events/drafts.json", ds)
}
func (s *Service) checkVisible(ctx context.Context, d *model.Draft, v *model.Video) (model.Visibility, error) {
	if d.Kind == "dynamic" {
		found, err := s.Bili.Dynamic(ctx, d.RPID)
		result := model.Visibility{Found: err == nil && found.ID == d.RPID && found.UID == d.Account && found.Text == d.Text}
		return result, err
	}
	return s.Bili.Visible(ctx, v.Aid, d.Root, d.RPID)
}

// applyVisibility仅记录发送账号的回查事实，不把state=0或找到ID等同公开可见。
func applyVisibility(d *model.Draft, result model.Visibility) {
	d.PlatformState = result.State
	d.RootPlatformState = result.RootState
	d.VisibilityCheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
	d.Error = ""
	if !result.Found {
		d.Status = "submitted"
		return
	}
	d.Status = "visible"
	state := result.State
	if result.RootState != nil && (*result.RootState == 17 || *result.RootState == 11) {
		state = result.RootState
	}
	if state != nil {
		switch *state {
		case 17:
			d.Status = "restricted"
		case 11:
			d.Status = "pending_review"
		}
	}
}
func (s *Service) Cancel(ctx context.Context, id string) (model.Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ds, err := s.Drafts(ctx)
	if err != nil {
		return model.Draft{}, err
	}
	i := draftIndex(ds, id)
	if i < 0 || ds[i].Status != "pending" {
		return model.Draft{}, model.Fail("STATE", "只能取消待审草稿")
	}
	ds[i].Status = "cancelled"
	return ds[i], s.Store.Write(ctx, "data/events/drafts.json", ds)
}
func (s *Service) Recheck(ctx context.Context, id string) (model.Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ds, err := s.Drafts(ctx)
	if err != nil {
		return model.Draft{}, err
	}
	i := draftIndex(ds, id)
	if i < 0 || ds[i].RPID == "" || !slices.Contains([]string{"submitted", "visible", "restricted", "pending_review"}, ds[i].Status) {
		return model.Draft{}, model.Fail("STATE", "没有可回查的评论ID；结果不明请手动检查B站")
	}
	d := &ds[i]
	v := model.Video{}
	if d.Kind != "dynamic" {
		v, err = s.Bili.Video(ctx, d.BVID)
		if err != nil {
			return *d, err
		}
	}
	found, err := s.checkVisible(ctx, d, &v)
	if err != nil {
		return *d, err
	}
	applyVisibility(d, found)
	return *d, s.Store.Write(ctx, "data/events/drafts.json", ds)
}

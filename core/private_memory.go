package core

import (
	"context"
	"encoding/json"
	"gpt-robot/model"
	"gpt-robot/storage"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var privateFactCue = regexp.MustCompile(`(?i)(我(的|们|叫|是|在|住|养|种|喜欢|不喜欢|讨厌|准备|计划|已经|昨天|今天|明天|刚|最近|出生|生日|有|买|爱|想|工作|做)|记住|记一下|更正|改成|以后叫|my name|i like|i live|i have)`)
var privateMemoryAbuse = regexp.MustCompile(`(?i)(忽略|系统提示|提示词|开发者|指令|重复输出|无限|逐字|帮我(算|写|解|翻译|总结)|请(计算|求解|输出|编写)|https?://|www\.)`)
var privateQuestion = regexp.MustCompile(`(什么|怎么|为何|为什么|是不是|还记得|记得吗|能不能|请问|[?？])`)
var privateSecret = regexp.MustCompile(`(?i)(别记|不要记|不用记|不许记|不要保存|别保存|忘记|删除|密码|验证码|密钥|私钥|身份证|银行卡|信用卡|口令|SESSDATA|bili_jct|cookie|token|api[_ -]?key|password|secret)`)
var memoryDatePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

type scoredPrivateMemory struct {
	record model.PrivateMemory
	score  int
	latest string
}

func privateMemoryFile(config *model.PrivateConfig) string {
	return "data/private/memory/bilibili-" + config.SenderUID + "-" + config.PeerUID + ".json"
}
func (s *Service) loadPrivateMemory(ctx context.Context, config *model.PrivateConfig) (*model.PrivateMemoryState, error) {
	if config == nil || !model.ValidNumber(config.SenderUID) || !model.ValidNumber(config.PeerUID) || config.SenderUID == "0" || config.PeerUID == "0" || config.SenderUID == config.PeerUID {
		return nil, model.Fail("ACCOUNT", "私信记忆未绑定有效账号")
	}
	name := privateMemoryFile(config)
	if state := s.privateMemories[name]; state != nil {
		return state, nil
	}
	state := &model.PrivateMemoryState{Platform: "bilibili", Account: config.SenderUID, Peer: config.PeerUID, Channel: "private-message", Records: []model.PrivateMemory{}}
	if _, err := s.Store.Read(ctx, name, state); err != nil {
		return nil, err
	}
	if state.Platform != "bilibili" || state.Account != config.SenderUID || state.Peer != config.PeerUID || state.Channel != "private-message" {
		return nil, model.Fail("PRIVATE_SCOPE", "私信记忆归属不符，拒绝读取")
	}
	if s.privateMemories == nil {
		s.privateMemories = map[string]*model.PrivateMemoryState{}
	}
	s.privateMemories[name] = state
	return state, nil
}
func clonePrivateMemoryState(state *model.PrivateMemoryState) model.PrivateMemoryState {
	result := *state
	result.Records = slices.Clone(state.Records)
	for i := range result.Records {
		result.Records[i].Sources = slices.Clone(result.Records[i].Sources)
	}
	return result
}
func worthPrivateMemory(text string) bool {
	return model.Text(text, 500) && !privateSecret.MatchString(text) && !privateQuestion.MatchString(text) && privateFactCue.MatchString(text) && !privateMemoryAbuse.MatchString(text) && !formulaAutoRequest.MatchString(text) && !strings.ContainsAny(text, "\r\n{}\\`")
}

// syncPrivateMemory 在已验证的唯一双人会话内增量记事；不调用模型，不读取其他来源。
func (s *Service) syncPrivateMemory(ctx context.Context, config *model.PrivateConfig, page *model.PrivatePage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if config == nil || page == nil || !config.Enabled || !config.MemoryEnabled {
		return nil
	}
	s.privateMemoryMu.Lock()
	defer s.privateMemoryMu.Unlock()
	stored, err := s.loadPrivateMemory(ctx, config)
	if err != nil {
		return err
	}
	state := clonePrivateMemoryState(stored)
	changed := false
	if state.Initialized && state.Generation == config.MemoryGeneration && page.HasMore && len(page.Messages) > 0 {
		minimum := page.Messages[0].Seq
		for _, message := range page.Messages {
			minimum = min(minimum, message.Seq)
		}
		if state.Cursor > 0 && minimum > state.Cursor+1 {
			return model.Fail("PRIVATE_MEMORY_GAP", "私聊新消息超出一次20条范围，已暂停；不会跨过遗漏消息建立记忆")
		}
	}
	// 先处理撤回，即使来源序号已过去，也不能继续检索撤回正文。
	for _, message := range page.Messages {
		if !message.Withdrawn || message.Sender != config.PeerUID || message.Receiver != config.SenderUID {
			continue
		}
		for i := range state.Records {
			active := false
			for j, source := range state.Records[i].Sources {
				if source.MessageKey == message.Key && !source.Withdrawn {
					state.Records[i].Sources[j].Withdrawn = true
					changed = true
				}
				if !state.Records[i].Sources[j].Withdrawn {
					active = true
				}
			}
			if !active && state.Records[i].Quote != "" {
				state.Records[i].Quote = ""
				changed = true
			}
		}
	}
	if !state.Initialized || state.Generation != config.MemoryGeneration {
		state.Cursor = 0
		for _, message := range page.Messages {
			state.Cursor = max(state.Cursor, message.Seq)
		}
		state.Initialized, state.Generation, state.Since = true, config.MemoryGeneration, time.Now().Unix()
		changed = true
	} else {
		messages := slices.Clone(page.Messages)
		slices.SortFunc(messages, comparePrivateMessages)
		for _, message := range messages {
			if message.Seq <= state.Cursor {
				continue
			}
			state.Cursor = message.Seq
			changed = true
			if !model.ValidNumber(message.Key) || message.Key == "0" || message.Sender != config.PeerUID || message.Receiver != config.SenderUID || message.Withdrawn || message.Timestamp < state.Since || message.Timestamp > time.Now().Add(5*time.Minute).Unix() || !worthPrivateMemory(message.Text) {
				continue
			}
			source := model.PrivateMemorySource{MessageKey: message.Key, Sequence: message.Seq, OccurredAt: time.Unix(message.Timestamp, 0).UTC().Format(time.RFC3339), RecordedAt: time.Now().UTC().Format(time.RFC3339Nano)}
			quote := strings.TrimSpace(message.Text)
			index := -1
			for i, record := range state.Records {
				if record.Quote == quote {
					index = i
					break
				}
			}
			if index < 0 {
				record := model.PrivateMemory{ID: model.ID(), Quote: quote, Kind: "user_statement", Sources: []model.PrivateMemorySource{source}}
				state.Records = append(state.Records, record)
			} else {
				state.Records[index].Sources = append(state.Records[index].Sources, source)
			}
		}
	}
	if !changed {
		return nil
	}
	if err := s.Store.Write(context.WithoutCancel(ctx), privateMemoryFile(config), &state); err != nil {
		return err
	}
	s.privateMemories[privateMemoryFile(config)] = &state
	return nil
}
func memoryTokens(query string) []string {
	lower := memoryDatePattern.ReplaceAllString(strings.ToLower(strings.TrimSpace(query)), " ")
	for _, word := range []string{"还记得", "记得吗", "我说过", "我说", "看过", "看到", "记住", "哪些", "事情", "你好", "谢谢", "的是什么", "什么", "怎么", "请问", "之前", "以前", "今天", "昨天", "前天", "告诉我", "你知道", "的", "了", "呢", "吗", "？", "?"} {
		lower = strings.ReplaceAll(lower, word, " ")
	}
	seen := map[string]bool{}
	for _, field := range strings.FieldsFunc(lower, splitMemoryToken) {
		if utf8.RuneCountInString(field) < 2 {
			continue
		}
		seen[field] = true
		runes := []rune(field)
		for i := 0; i+1 < len(runes); i++ {
			seen[string(runes[i:i+2])] = true
		}
	}
	result := []string{}
	for token := range seen {
		result = append(result, token)
	}
	slices.Sort(result)
	return result
}
func splitMemoryToken(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }
func memoryQueryDate(query string, now time.Time) string {
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	day := now.In(zone)
	if value := memoryDatePattern.FindString(query); value != "" {
		return value
	}
	if strings.Contains(query, "前天") {
		return day.AddDate(0, 0, -2).Format("2006-01-02")
	}
	if strings.Contains(query, "昨天") {
		return day.AddDate(0, 0, -1).Format("2006-01-02")
	}
	if strings.Contains(query, "今天") {
		return day.Format("2006-01-02")
	}
	return ""
}
func queryPrivateMemories(state *model.PrivateMemoryState, input *model.PrivateMemoryQuery, now time.Time) []model.PrivateMemory {
	query := ""
	if input.Query != nil {
		query = strings.TrimSpace(*input.Query)
	}
	tokens := memoryTokens(query)
	date := memoryQueryDate(query, now)
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	scored := []scoredPrivateMemory{}
	for _, record := range state.Records {
		if record.Quote == "" {
			continue
		}
		filtered := record
		filtered.Sources = []model.PrivateMemorySource{}
		latest := ""
		for _, source := range record.Sources {
			if source.Withdrawn || input.BeforeSequence > 0 && source.Sequence >= input.BeforeSequence {
				continue
			}
			if date != "" {
				at, err := time.Parse(time.RFC3339Nano, source.OccurredAt)
				if err != nil || at.In(zone).Format("2006-01-02") != date {
					continue
				}
			}
			filtered.Sources = append(filtered.Sources, source)
			if source.OccurredAt > latest {
				latest = source.OccurredAt
			}
		}
		if len(filtered.Sources) == 0 {
			continue
		}
		score := 0
		for _, token := range tokens {
			if strings.Contains(strings.ToLower(record.Quote), token) {
				score += min(utf8.RuneCountInString(token), 6)
			}
		}
		if query != "" && score == 0 && (len(tokens) > 0 || date == "") {
			continue
		}
		item := scoredPrivateMemory{record: filtered, score: score, latest: latest}
		scored = append(scored, item)
	}
	slices.SortFunc(scored, compareScoredPrivateMemory)
	result := []model.PrivateMemory{}
	for _, item := range scored {
		result = append(result, item.record)
	}
	return result
}
func compareScoredPrivateMemory(a, b scoredPrivateMemory) int {
	if a.score != b.score {
		return b.score - a.score
	}
	if a.latest != b.latest {
		return strings.Compare(b.latest, a.latest)
	}
	return strings.Compare(a.record.ID, b.record.ID)
}

// PrivateMemories 仅本地认证网页使用；先检查当前登录账号，查询不调用模型或平台。
func (s *Service) PrivateMemories(ctx context.Context, input *model.PrivateMemoryQuery) (*model.PrivateMemoryPage, error) {
	if input == nil || input.Page < 0 || input.PageSize < 0 || input.PageSize > 20 || input.Page > 1000000 || input.Query != nil && utf8.RuneCountInString(*input.Query) > 100 {
		return nil, model.Fail("INPUT", "记忆关键词最多100字，单页最多20条")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.privateMu.Lock()
	config := s.privateConfig
	s.privateMu.Unlock()
	if err := s.privateAccount(ctx, &config); err != nil {
		return nil, err
	}
	s.privateMemoryMu.Lock()
	defer s.privateMemoryMu.Unlock()
	state, err := s.loadPrivateMemory(ctx, &config)
	if err != nil {
		return nil, err
	}
	if s.Store.Database != nil && input.BeforeSequence > 0 {
		queryText := ""
		if input.Query != nil {
			queryText = *input.Query
		}
		candidates := storage.MemoryQuery{BeforeSequence: input.BeforeSequence, Namespace: privateMemoryFile(&config), Platform: "bilibili", Account: config.SenderUID, Peer: config.PeerUID, Terms: memoryTokens(queryText), Limit: 60}
		if date := memoryQueryDate(queryText, time.Now()); date != "" {
			if at, parseErr := time.ParseInLocation("2006-01-02", date, time.FixedZone("Asia/Shanghai", 8*60*60)); parseErr == nil {
				candidates.Since = at.UnixMilli()
				candidates.Until = at.AddDate(0, 0, 1).UnixMilli()
			}
		}
		rows, queryErr := s.Store.Database.MemoryCandidates(ctx, &candidates)
		if queryErr != nil {
			return nil, queryErr
		}
		bounded := *state
		bounded.Records = []model.PrivateMemory{}
		for _, raw := range rows {
			var record model.PrivateMemory
			if queryErr = json.Unmarshal(raw, &record); queryErr != nil {
				return nil, queryErr
			}
			bounded.Records = append(bounded.Records, record)
		}
		state = &bounded
	}
	records := queryPrivateMemories(state, input, time.Now())
	page, size := input.Page, input.PageSize
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	result := &model.PrivateMemoryPage{Enabled: config.MemoryEnabled, Account: config.SenderUID, Peer: config.PeerUID, Items: []model.PrivateMemory{}, Total: len(records), Page: page, PageSize: size}
	start := (page - 1) * size
	if start < len(records) {
		result.Items = records[start:min(start+size, len(records))]
	}
	return result, nil
}
func (s *Service) ForgetPrivateMemory(ctx context.Context, id string) error {
	if !model.ValidID(id) {
		return model.Fail("INPUT", "记忆ID无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.privateMu.Lock()
	config := s.privateConfig
	s.privateMu.Unlock()
	if err := s.privateAccount(ctx, &config); err != nil {
		return err
	}
	s.privateMu.Lock()
	defer s.privateMu.Unlock()
	if s.privateConfig != config {
		return model.Fail("PRIVATE_SCOPE", "私聊授权已变化")
	}
	s.privateMemoryMu.Lock()
	defer s.privateMemoryMu.Unlock()
	stored, err := s.loadPrivateMemory(ctx, &config)
	if err != nil {
		return err
	}
	state := clonePrivateMemoryState(stored)
	found := false
	forgotten := map[string]bool{}
	for i := range state.Records {
		if state.Records[i].ID == id {
			state.Records[i].Quote = ""
			for j := range state.Records[i].Sources {
				state.Records[i].Sources[j].Withdrawn = true
				forgotten[state.Records[i].Sources[j].MessageKey] = true
			}
			found = true
			break
		}
	}
	if !found {
		return model.Fail("NOT_FOUND", "找不到这条私聊记忆")
	}
	if err := s.Store.Write(ctx, privateMemoryFile(&config), &state); err != nil {
		return err
	}
	s.privateMemories[privateMemoryFile(&config)] = &state
	for _, out := range s.privateState.Outbox {
		if forgotten[out.AutoKey] && out.MessageKey != "" {
			forgotten[out.MessageKey] = true
		}
	}
	for i := range s.privateState.Inbox {
		if forgotten[s.privateState.Inbox[i].Key] {
			s.privateState.Inbox[i].Text = ""
			s.privateState.Inbox[i].Image = nil
			s.privateState.Inbox[i].Withdrawn = true
		}
	}
	for i := range s.privateState.Outbox {
		if forgotten[s.privateState.Outbox[i].AutoKey] {
			s.privateState.Outbox[i].Text = ""
		}
	}
	if err := s.savePrivate(ctx); err != nil {
		return err
	}
	s.modelRouter.CloseConversations()
	scope := model.ConversationScope{Platform: "bilibili", Account: config.SenderUID, Peer: config.PeerUID, Channel: "private-message"}
	s.conversationMu.Lock()
	defer s.conversationMu.Unlock()
	cleared := conversationHandoffState{Epoch: model.ID(), Generation: config.MemoryGeneration}
	if err := s.Store.Write(ctx, handoffFile(&scope), &cleared); err != nil {
		return err
	}
	return nil
}

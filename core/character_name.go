package core

import (
	"context"
	"gpt-robot/model"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// coreNameFile 保存核心名字及私有来源、水位，不向公开接口输出来源。
	coreNameFile = ".private/character-name.json"
	// coreNameStart 核心人设中受控名字段的起始标记。
	coreNameStart = "<!-- robot-core-name:start -->"
	// coreNameEnd 核心人设中受控名字段的结束标记。
	coreNameEnd = "<!-- robot-core-name:end -->"
)

// coreNameState 仅名字投影到公开人设；账号、消息键及水位始终留在私有目录。
type coreNameState struct {
	Name        string `json:"name"`
	Account     string `json:"account"`
	Owner       string `json:"owner"`
	Initialized bool   `json:"initialized"`
	Cursor      int64  `json:"cursor"`
	Since       int64  `json:"since"`
	MessageKey  string `json:"messageKey,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
}

var namingCommand = regexp.MustCompile(`^(?:你有名字了[，, ]*(?:就)?叫|(?:以后|现在|从现在起)?你(?:以后|现在|从现在起)?(?:就)?叫|你(?:以后|现在|从现在起)?的(?:正式名字|名字)(?:是|叫|改为|改成)|(?:我)?(?:给你|帮你)(?:起名|取名|改名)(?:为|叫|成)?|把你的(?:正式名字|名字)(?:改成|改为))[：: ]*(.+)$`)
var namingUncertain = regexp.MustCompile(`(?i)(如果|假如|要不|是不是|可能|暂时|仅限|私下|在这里|小名|昵称|别名|不要|不能|别叫|不叫|[?？\r\n])`)

func explicitCoreName(text string) string {
	text = strings.TrimSpace(text)
	question := strings.TrimRight(text, "。！!")
	if (strings.HasSuffix(question, "吗") || strings.HasSuffix(question, "呢")) || !model.Text(text, 80) || namingUncertain.MatchString(text) || privateQuestion.MatchString(text) || privateSecret.MatchString(text) || privateMemoryAbuse.MatchString(text) {
		return ""
	}
	match := namingCommand.FindStringSubmatch(text)
	if len(match) != 2 {
		return ""
	}
	name := strings.TrimSpace(strings.TrimRight(match[1], "。！!"))
	for _, pair := range [][2]string{{"“", "”"}, {"「", "」"}, {"\"", "\""}} {
		if strings.HasPrefix(name, pair[0]) && strings.HasSuffix(name, pair[1]) {
			name = strings.TrimSuffix(strings.TrimPrefix(name, pair[0]), pair[1])
			break
		}
	}
	if !validCoreName(name) || privateFactCue.MatchString(name) {
		return ""
	}
	return name
}
func validCoreName(name string) bool {
	if !model.Text(name, 24) || strings.TrimSpace(name) != name {
		return false
	}
	letters := false
	for _, r := range name {
		if unicode.IsLetter(r) {
			letters = true
			continue
		}
		if !unicode.IsNumber(r) && r != '·' && r != '_' && r != '-' {
			return false
		}
	}
	return letters
}
func (s *Service) readCoreName(ctx context.Context) (*coreNameState, error) {
	state := &coreNameState{}
	if _, err := s.Store.Read(ctx, coreNameFile, state); err != nil {
		return nil, err
	}
	if state.Name != "" && !validCoreName(state.Name) {
		return nil, model.Fail("DATA", "核心名字记录无效，未覆盖人设")
	}
	return state, nil
}
func personaWithName(persona, name string) (string, error) {
	if name == "" {
		return persona, nil
	}
	start, end := strings.Index(persona, coreNameStart), strings.Index(persona, coreNameEnd)
	if start >= 0 || end >= 0 {
		if start < 0 || end < start || strings.Count(persona, coreNameStart) != 1 || strings.Count(persona, coreNameEnd) != 1 {
			return "", model.Fail("DATA", "核心名字段标记不完整，请检查人设")
		}
		persona = persona[:start] + persona[end+len(coreNameEnd):]
	}
	// 仅清除内置模板的未命名占位；不重写其他性格或私聊内容。
	persona = strings.ReplaceAll(persona, "还没有固定名字", "已有正式名字（见核心身份）")
	prefix := coreNameStart + "\n# 核心身份\n正式名字：" + name + "\n这是当前正式名字；旧名字仅为历史，不沿用未命名描述。\n" + coreNameEnd + "\n\n"
	return prefix + strings.TrimSpace(persona) + "\n", nil
}
func (s *Service) namedPersona(ctx context.Context, persona string) (string, error) {
	state, err := s.readCoreName(ctx)
	if err != nil {
		return "", err
	}
	return personaWithName(persona, state.Name)
}

// projectCoreName 从已持久化名字修复人设投影；启动及每次私信读取前调用，可恢复两文件间中断。
func (s *Service) projectCoreName(ctx context.Context) error {
	state, err := s.readCoreName(ctx)
	if err != nil {
		return err
	}
	return s.projectNamedPersona(ctx, state.Name)
}
func (s *Service) projectNamedPersona(ctx context.Context, name string) error {
	if name == "" {
		return nil
	}
	persona, err := s.Store.Text(ctx, "data/character/persona.md")
	if err != nil {
		return err
	}
	updated, err := personaWithName(persona, name)
	if err != nil {
		return err
	}
	if updated == persona {
		return nil
	}
	if utf8.RuneCountInString(updated) > 20000 {
		return model.Fail("INPUT", "核心名字加入后人设超过20000字")
	}
	return s.Store.WriteText(ctx, "data/character/persona.md", updated)
}

// syncCoreName 调用方持有业务和私信锁，且已验证登录账号与唯一双人会话；不调用模型。
func (s *Service) syncCoreName(ctx context.Context, config *model.PrivateConfig, page *model.PrivatePage) error {
	state, err := s.readCoreName(ctx)
	if err != nil {
		return err
	}
	if err := s.projectNamedPersona(ctx, state.Name); err != nil {
		return err
	}
	oldName := state.Name
	if !config.Enabled {
		return nil
	}
	if !state.Initialized || state.Account != config.SenderUID || state.Owner != config.PeerUID {
		state.Account, state.Owner, state.Initialized, state.Cursor, state.Since = config.SenderUID, config.PeerUID, true, 0, time.Now().Unix()
		for _, message := range page.Messages {
			state.Cursor = max(state.Cursor, message.Seq)
		}
		// 首次读取或切换账号只建基线，旧命名不能回放覆盖当前名字。
		return s.Store.Write(ctx, coreNameFile, state)
	}
	messages := slices.Clone(page.Messages)
	slices.SortFunc(messages, comparePrivateMessages)
	changed := false
	for _, message := range messages {
		if message.Seq <= state.Cursor {
			continue
		}
		state.Cursor = message.Seq
		changed = true
		if message.Sender != config.PeerUID || message.Receiver != config.SenderUID || message.Withdrawn || !model.ValidNumber(message.Key) || message.Key == "0" || message.Timestamp < state.Since || message.Timestamp < time.Now().Add(-30*time.Minute).Unix() || message.Timestamp > time.Now().Add(5*time.Minute).Unix() {
			continue
		}
		name := explicitCoreName(message.Text)
		if name == "" {
			continue
		}
		state.Name, state.MessageKey, state.UpdatedAt = name, message.Key, time.Now().UTC().Format(time.RFC3339Nano)
	}
	if !changed {
		return nil
	}
	// 名字和来源先原子持久化，再投影到persona；中断后由启动恢复，水位阻止旧命名回退。
	if err := s.Store.Write(ctx, coreNameFile, state); err != nil {
		return err
	}
	if state.Name == oldName {
		return nil
	}
	return s.projectNamedPersona(ctx, state.Name)
}

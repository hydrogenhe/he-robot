package model

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Error 是可安全返回给使用者的业务错误，Err 仅供本机排错。
type Error struct {
	Code    string
	Message string
	Status  int
	Err     error
}

func (e *Error) Error() string        { return e.Message }
func (e *Error) Unwrap() error        { return e.Err }
func Fail(code, message string) error { return &Error{Code: code, Message: message, Status: 400} }
func Internal(err error) error {
	var business *Error
	if errors.As(err, &business) {
		return business
	}
	return &Error{Code: "INTERNAL", Message: "本地操作失败，请检查数据文件与服务日志", Status: 500, Err: err}
}
func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
func ValidID(s string) bool { return uuidPattern.MatchString(s) }

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var digits = regexp.MustCompile(`^\d+$`)
var bvPattern = regexp.MustCompile(`^BV[0-9A-Za-z]{10}$`)

func ValidNumber(s string) bool { return digits.MatchString(s) }
func ValidBV(s string) bool     { return bvPattern.MatchString(s) }
func Text(s string, max int) bool {
	return strings.TrimSpace(s) != "" && utf8.RuneCountInString(s) <= max
}
func Clip(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}

type Permissions struct {
	Enabled         bool     `json:"enabled"`
	AllowDynamic    bool     `json:"allowDynamic"`
	AllowedVideos   []string `json:"allowedVideos"`
	AllowedUsers    []string `json:"allowedUsers"`
	BlockedUsers    []string `json:"blockedUsers"`
	DailyLimit      int      `json:"dailyLimit"`
	CooldownSeconds int      `json:"cooldownSeconds"`
}

// UnmarshalJSON 保持旧版严格配置语义，缺少安全字段不使用默认值放行。
func (p *Permissions) UnmarshalJSON(raw []byte) error {
	var wire struct {
		Enabled         *bool     `json:"enabled"`
		AllowDynamic    *bool     `json:"allowDynamic"`
		AllowedVideos   *[]string `json:"allowedVideos"`
		AllowedUsers    *[]string `json:"allowedUsers"`
		BlockedUsers    *[]string `json:"blockedUsers"`
		DailyLimit      *int      `json:"dailyLimit"`
		CooldownSeconds *int      `json:"cooldownSeconds"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&wire); err != nil {
		return err
	}
	if wire.Enabled == nil || wire.AllowedVideos == nil || wire.AllowedUsers == nil || wire.BlockedUsers == nil || wire.DailyLimit == nil || wire.CooldownSeconds == nil {
		return Fail("DATA", "权限配置缺少必需字段")
	}
	p.Enabled = *wire.Enabled
	p.AllowDynamic = wire.AllowDynamic != nil && *wire.AllowDynamic
	p.AllowedVideos = *wire.AllowedVideos
	p.AllowedUsers = *wire.AllowedUsers
	p.BlockedUsers = *wire.BlockedUsers
	p.DailyLimit = *wire.DailyLimit
	p.CooldownSeconds = *wire.CooldownSeconds
	return p.Validate()
}

func DefaultPermissions() Permissions {
	p := Permissions{AllowedVideos: []string{}, AllowedUsers: []string{}, BlockedUsers: []string{}, DailyLimit: 5, CooldownSeconds: 120}
	return p
}
func (p *Permissions) Validate() error {
	if p == nil || p.DailyLimit < 1 || p.DailyLimit > 30 || p.CooldownSeconds < 60 || p.CooldownSeconds > 86400 {
		return Fail("INPUT", "额度或冷却不符合要求")
	}
	if len(p.AllowedVideos) > 100 || len(p.AllowedUsers) > 100 || len(p.BlockedUsers) > 100 {
		return Fail("INPUT", "允许或禁止列表不能超过100项")
	}
	for _, s := range p.AllowedVideos {
		if !ValidBV(s) {
			return Fail("INPUT", "视频BV号不正确")
		}
	}
	for _, s := range append(append([]string{}, p.AllowedUsers...), p.BlockedUsers...) {
		if !ValidNumber(s) {
			return Fail("INPUT", "用户UID不正确")
		}
	}
	return nil
}

type Credentials struct {
	UserAgent string            `json:"userAgent,omitempty"`
	Cookies   map[string]string `json:"cookies"`
	UID       string            `json:"uid"`
}
type Account struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
}
type AccountResult struct {
	LoggedIn bool
	Account  Account
}
type Video struct {
	DurationSeconds int    `json:"durationSeconds,omitempty"`
	Description     string `json:"description,omitempty"`
	Aid             string `json:"aid"`
	BVID            string `json:"bvid"`
	Title           string `json:"title"`
	Owner           string `json:"owner"`
}
type ImportSessionInput struct {
	Cookie      string
	ExpectedUID string
	UserAgent   string
}

type CommentReceipt struct {
	ID      string
	State   *int
	Message string
}

type Visibility struct {
	RootState *int
	Found     bool
	State     *int
}

type Comment struct {
	CreatedAt  int64  `json:"createdAt,omitempty"`
	ReplyCount int    `json:"replyCount,omitempty"`
	RootState  *int   `json:"rootState,omitempty"`
	State      *int   `json:"state,omitempty"`
	ID         string `json:"id"`
	Root       string `json:"root"`
	Parent     string `json:"parent"`
	UID        string `json:"uid"`
	Name       string `json:"name"`
	Text       string `json:"text"`
}
type CommentPage struct {
	Comments []Comment
	Page     int
	Total    int
	HasMore  bool
}
type CommentsInput struct {
	BVID string
	Page *int
	Root *string
}
type CommentsResult struct {
	Video Video
	CommentPage
	EventID string
}
type DraftInput struct {
	BVID   string
	Root   string
	Parent string
	Text   string
}
type Draft struct {
	AutoCommentID       string `json:"autoCommentId,omitempty"`
	PlatformMessage     string `json:"platformMessage,omitempty"`
	RootPlatformState   *int   `json:"rootPlatformState,omitempty"`
	PlatformState       *int   `json:"platformState,omitempty"`
	VisibilityCheckedAt string `json:"visibilityCheckedAt,omitempty"`
	BVID                string `json:"bvid"`
	Root                string `json:"root"`
	Parent              string `json:"parent"`
	Text                string `json:"text"`
	ID                  string `json:"id"`
	Kind                string `json:"kind,omitempty"`
	CreatedAt           string `json:"createdAt"`
	Account             string `json:"account"`
	TargetUser          string `json:"targetUser"`
	TargetText          string `json:"targetText"`
	VideoTitle          string `json:"videoTitle"`
	Status              string `json:"status"`
	RPID                string `json:"rpid,omitempty"`
	Error               string `json:"error,omitempty"`
	AttemptAt           string `json:"attemptAt,omitempty"`
}
type Event struct {
	ID      string         `json:"id"`
	At      string         `json:"at"`
	Kind    string         `json:"kind"`
	Summary string         `json:"summary"`
	Data    map[string]any `json:"data"`
}
type Note struct {
	ID      string   `json:"id"`
	At      string   `json:"at"`
	Kind    string   `json:"kind"`
	Title   string   `json:"title"`
	Text    string   `json:"text"`
	Sources []string `json:"sources"`
}
type NoteInput struct {
	Kind    string
	Title   string
	Text    string
	Sources []string
}
type ContextResult struct {
	Persona          string
	PersonaTruncated bool
	Description      string
	Memories         []Note
	RecentEvents     []Event
}
type SearchResult struct {
	Events []Event
	Notes  []Note
}
type Overview struct {
	RobotStatus  string
	RobotEnabled bool
	AutoEnabled  bool
	AutoPaused   bool
	Persona      string
	Permissions  Permissions
	Account      *Account
	Drafts       []Draft
	Events       []Event
	Notes        []Note
	DataPath     string
}
type PollResult struct {
	Code        int
	Credentials *Credentials
}
type LoginResult struct {
	Code int
	AccountResult
}
type Dynamic struct {
	ID   string
	UID  string
	Text string
}
type Runtime struct {
	Origin string `json:"origin"`
	Token  string `json:"token"`
	PID    int    `json:"pid"`
}
type Launch struct {
	URL string `json:"url"`
}

func (d *DraftInput) Normalize() error {
	if d == nil {
		return Fail("INPUT", "缺少草稿参数")
	}
	d.Text = strings.TrimSpace(d.Text)
	if d.Root == "" {
		d.Root = "0"
	}
	if d.Parent == "" {
		d.Parent = "0"
	}
	if !ValidBV(d.BVID) || !ValidNumber(d.Root) || !ValidNumber(d.Parent) || !Text(d.Text, 1000) {
		return Fail("INPUT", "草稿参数不正确")
	}
	if d.Parent == "0" && d.Root != "0" {
		return Fail("TARGET", "一级评论不能指定楼层根ID")
	}
	if d.Parent != "0" && d.Root == "0" {
		d.Root = d.Parent
	}
	return nil
}
func Number(v any) string {
	switch n := v.(type) {
	case string:
		return n
	case fmt.Stringer:
		return n.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

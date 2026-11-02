package bilibili

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"gpt-robot/model"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ClientInterface interface {
	NativeStickers() []model.NativeSticker
	DownloadPrivateImage(context.Context, *model.ImageAttachment) (*model.ImageAttachment, error)
	SendPrivateImage(context.Context, string, string, *model.ImageAttachment, string) (string, error)
	SearchVideos(context.Context, string) ([]model.Video, error)
	WatchPrivate(context.Context, string, string, func(*model.PrivatePushEvent)) error
	PrivateMessages(context.Context, string, string) (*model.PrivatePage, error)
	SendPrivate(context.Context, string, string, string, string) (string, error)
	Status(context.Context) (model.Account, error)
	QR(context.Context) (string, string, error)
	Poll(context.Context, string) (model.PollResult, error)
	Video(context.Context, string) (model.Video, error)
	Comments(context.Context, string, int, string) (model.CommentPage, error)
	Target(context.Context, string, string, string) (model.Comment, error)
	Send(context.Context, string, string, string, string, string) (model.CommentReceipt, error)
	SendDynamic(context.Context, string) (string, error)
	Dynamic(context.Context, string) (model.Dynamic, error)
	Visible(context.Context, string, string, string) (model.Visibility, error)
}
type Client struct {
	credentials func(context.Context) (*model.Credentials, error)
	http        *http.Client
	mu          sync.Mutex
	next        time.Time
	paused      time.Time
	wbi         string
}

var _ ClientInterface = (*Client)(nil)

func New(credentials func(context.Context) (*model.Credentials, error), transport *http.Client) *Client {
	if transport == nil {
		transport = &http.Client{Timeout: 20 * time.Second}
	}
	copyClient := *transport
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c := &Client{credentials: credentials, http: &copyClient}
	return c
}
func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
func list(v any) []any { a, _ := v.([]any); return a }
func str(v any) string { return model.Number(v) }
func number(v any) int { var n int; fmt.Sscan(str(v), &n); return n }
func (c *Client) request(ctx context.Context, base, route string, params url.Values, write bool, payload any, pageURL ...string) (map[string]any, []*http.Cookie, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.paused) {
		return nil, nil, model.Fail("PAUSED", "平台请求已暂停，请五分钟后再试")
	}
	wait := time.Until(c.next)
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	c.next = time.Now().Add(1500 * time.Millisecond)
	u, err := url.Parse(base + route)
	if err != nil {
		return nil, nil, model.Internal(err)
	}
	var body io.Reader
	method := "GET"
	if write {
		method = "POST"
		if payload != nil {
			if upload, ok := payload.(*multipartPayload); ok {
				body = bytes.NewReader(upload.body)
			} else {
				b, e := json.Marshal(payload)
				if e != nil {
					return nil, nil, model.Internal(e)
				}
				body = bytes.NewReader(b)
			}
		} else {
			body = strings.NewReader(params.Encode())
		}
	} else {
		u.RawQuery = params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, nil, model.Internal(err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://www.bilibili.com/")
	creds, err := c.credentials(ctx)
	if err != nil {
		return nil, nil, err
	}
	if creds != nil {
		if creds.UserAgent != "" {
			req.Header.Set("User-Agent", creds.UserAgent)
		}
		for k, v := range creds.Cookies {
			req.AddCookie(&http.Cookie{Name: k, Value: v})
		}
	}
	if len(pageURL) > 0 {
		req.Header.Set("Referer", pageURL[0])
	}
	if write {
		req.Header.Set("Origin", "https://www.bilibili.com")
		if len(pageURL) > 0 && pageURL[0] == "https://message.bilibili.com/" {
			req.Header.Set("Origin", "https://message.bilibili.com")
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if payload != nil {
			if upload, ok := payload.(*multipartPayload); ok {
				req.Header.Set("Content-Type", upload.contentType)
			} else {
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Origin", "https://t.bilibili.com")
				req.Header.Set("Referer", "https://t.bilibili.com/")
			}
		}
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, nil, requestNetworkError(req, err, write)
	}
	defer res.Body.Close()
	if res.StatusCode == 412 || res.StatusCode == 429 {
		c.paused = time.Now().Add(5 * time.Minute)
	}
	if res.StatusCode != 200 {
		code := "PLATFORM_HTTP"
		if write {
			code = "SEND_UNKNOWN"
		}
		return nil, nil, model.Fail(code, fmt.Sprintf("B站返回HTTP %d，请停止操作并稍后检查", res.StatusCode))
	}
	var envelope struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	dec := json.NewDecoder(io.LimitReader(res.Body, 8<<20))
	dec.UseNumber()
	if err = dec.Decode(&envelope); err != nil {
		if write {
			return nil, nil, model.Fail("SEND_UNKNOWN", "平台回执无法解析，结果不明；请先回查")
		}
		return nil, nil, model.Fail("PLATFORM", "平台响应无法解析")
	}
	if envelope.Code != 0 {
		if envelope.Code == -412 || envelope.Code == -509 || envelope.Code == 12015 {
			c.paused = time.Now().Add(5 * time.Minute)
		}
		return nil, nil, model.Fail(fmt.Sprintf("BILI_%d", envelope.Code), fmt.Sprintf("B站操作未成功。错误码：%d", envelope.Code))
	}
	return envelope.Data, res.Cookies(), nil
}
func (c *Client) Status(ctx context.Context) (model.Account, error) {
	d, _, err := c.request(ctx, "https://api.bilibili.com", "/x/web-interface/nav", nil, false, nil)
	if err != nil {
		return model.Account{}, err
	}
	if d["isLogin"] != true {
		return model.Account{}, model.Fail("LOGIN", "B站登录已失效，请重新扫码")
	}
	c.mu.Lock()
	c.wbi = wbiKey(d)
	c.mu.Unlock()
	a := model.Account{UID: str(d["mid"]), Name: str(d["uname"])}
	return a, nil
}
func (c *Client) QR(ctx context.Context) (string, string, error) {
	d, _, err := c.request(ctx, "https://passport.bilibili.com", "/x/passport-login/web/qrcode/generate", nil, false, nil)
	return str(d["url"]), str(d["qrcode_key"]), err
}
func (c *Client) Poll(ctx context.Context, key string) (model.PollResult, error) {
	d, cookies, err := c.request(ctx, "https://passport.bilibili.com", "/x/passport-login/web/qrcode/poll", url.Values{"qrcode_key": {key}}, false, nil)
	if err != nil {
		return model.PollResult{}, err
	}
	result := model.PollResult{Code: number(d["code"])}
	if result.Code != 0 {
		return result, nil
	}
	saved := map[string]string{}
	for _, cookie := range cookies {
		if SessionCookieAllowed(cookie.Name) {
			saved[cookie.Name] = cookie.Value
		}
	}
	if saved["SESSDATA"] == "" || saved["bili_jct"] == "" || saved["DedeUserID"] == "" {
		return result, model.Fail("LOGIN", "扫码结果缺少必要凭证，未保存")
	}
	result.Credentials = &model.Credentials{UID: saved["DedeUserID"], Cookies: saved}
	return result, nil
}
func (c *Client) Video(ctx context.Context, bvid string) (model.Video, error) {
	d, _, err := c.request(ctx, "https://api.bilibili.com", "/x/web-interface/view", url.Values{"bvid": {bvid}}, false, nil)
	duration, _ := strconv.Atoi(str(d["duration"]))
	v := model.Video{DurationSeconds: duration, Description: model.Clip(str(d["desc"]), 500), Aid: str(d["aid"]), BVID: str(d["bvid"]), Title: str(d["title"]), Owner: str(obj(d["owner"])["mid"])}
	return v, err
}
func parseComment(raw any) model.Comment {
	d := obj(raw)
	member := obj(d["member"])
	id := str(d["rpid_str"])
	if id == "" {
		id = str(d["rpid"])
	}
	root := str(d["root_str"])
	if root == "" {
		root = str(d["root"])
	}
	if root == "" {
		root = "0"
	}
	parent := str(d["parent_str"])
	if parent == "" {
		parent = str(d["parent"])
	}
	if parent == "" {
		parent = "0"
	}
	uid := str(member["mid"])
	if uid == "" {
		uid = str(d["mid"])
	}
	result := model.Comment{CreatedAt: int64(number(d["ctime"])), ReplyCount: number(d["rcount"]), ID: id, Root: root, Parent: parent, UID: uid, Name: str(member["uname"]), Text: str(obj(d["content"])["message"])}
	if rawState, ok := d["state"]; ok {
		value := number(rawState)
		result.State = &value
	}
	return result
}
func (c *Client) commentData(ctx context.Context, aid string, page int, root string) (map[string]any, error) {
	route := "/x/v2/reply"
	p := url.Values{"type": {"1"}, "oid": {aid}, "pn": {fmt.Sprint(page)}, "ps": {"20"}, "sort": {"0"}}
	if root != "" && root != "0" {
		route += "/reply"
		p.Set("root", root)
	}
	d, _, err := c.request(ctx, "https://api.bilibili.com", route, p, false, nil)
	return d, err
}
func (c *Client) Comments(ctx context.Context, aid string, page int, root string) (model.CommentPage, error) {
	d, err := c.commentData(ctx, aid, page, root)
	if err != nil {
		return model.CommentPage{}, err
	}
	comments := []model.Comment{}
	for _, raw := range list(d["replies"]) {
		comments = append(comments, parseComment(raw))
	}
	total := number(obj(d["page"])["count"])
	result := model.CommentPage{Comments: comments, Page: page, Total: total, HasMore: page*20 < total}
	return result, nil
}
func (c *Client) Target(ctx context.Context, aid, root, parent string) (model.Comment, error) {
	if root == "0" {
		root = parent
	}
	d, err := c.commentData(ctx, aid, 1, root)
	if err != nil {
		return model.Comment{}, err
	}
	first := parseComment(d["root"])
	if first.ID == parent {
		return first, nil
	}
	for _, raw := range list(d["replies"]) {
		v := parseComment(raw)
		v.RootState = first.State
		if v.ID == parent {
			return v, nil
		}
	}
	pages := (number(obj(d["page"])["count"]) + 19) / 20
	if pages > 50 {
		pages = 50
	}
	for pn := 2; pn <= pages; pn++ {
		p, e := c.Comments(ctx, aid, pn, root)
		if e != nil {
			return model.Comment{}, e
		}
		for _, v := range p.Comments {
			v.RootState = first.State
			if v.ID == parent {
				return v, nil
			}
		}
	}
	return model.Comment{}, model.Fail("TARGET", "找不到目标评论或超出本次查找范围，请重新读取评论")
}

// Send对齐网页普通视频评论的业务字段；不复制一次性签名或浏览器校验令牌。
func (c *Client) Send(ctx context.Context, aid, text, root, parent, bvid string) (model.CommentReceipt, error) {
	cred, err := c.credentials(ctx)
	if err != nil {
		return model.CommentReceipt{}, err
	}
	if cred == nil {
		return model.CommentReceipt{}, model.Fail("LOGIN", "请先登录")
	}
	p := url.Values{"type": {"1"}, "oid": {aid}, "message": {text}, "csrf": {cred.Cookies["bili_jct"]}, "plat": {"1"}, "gaia_source": {"main_web"}, "statistics": {`{"appId":100,"platform":5}`}, "at_name_to_mid": {"{}"}}
	if parent != "" && parent != "0" {
		p.Set("root", root)
		p.Set("parent", parent)
	}
	referer := "https://www.bilibili.com/video/av" + aid + "/"
	if model.ValidBV(bvid) {
		referer = "https://www.bilibili.com/video/" + bvid + "/"
	}
	d, _, err := c.request(ctx, "https://api.bilibili.com", "/x/v2/reply/add", p, true, nil, referer)
	if err != nil {
		return model.CommentReceipt{}, err
	}
	reply := parseComment(d["reply"])
	id := str(d["rpid_str"])
	if id == "" {
		id = str(d["rpid"])
	}
	if id == "" {
		id = reply.ID
	}
	if id == "" {
		return model.CommentReceipt{}, model.Fail("SEND_UNKNOWN", "平台未返回评论ID，结果不明，请先回查")
	}
	receipt := model.CommentReceipt{ID: id, State: reply.State, Message: model.Clip(str(d["success_toast"]), 300)}
	return receipt, nil
}

// SessionCookieAllowed只保留实际浏览器提供的认证与设备字段，不生成设备或校验令牌。
func SessionCookieAllowed(name string) bool {
	switch name {
	case "SESSDATA", "bili_jct", "DedeUserID", "DedeUserID__ckMd5", "buvid3", "buvid4", "buvid_fp", "_uuid", "b_nut", "b_lsid", "sid":
		return true
	}
	return false
}
func (c *Client) SendDynamic(ctx context.Context, text string) (string, error) {
	cred, err := c.credentials(ctx)
	if err != nil {
		return "", err
	}
	if cred == nil {
		return "", model.Fail("LOGIN", "请先登录")
	}
	payload := map[string]any{"dyn_req": map[string]any{"content": map[string]any{"contents": []any{map[string]any{"raw_text": text, "type": 1, "biz_id": ""}}}, "scene": 1, "meta": map[string]any{"app_meta": map[string]any{"from": "create.dynamic.web", "mobi_app": "web"}}}}
	route := "/x/dynamic/feed/create/dyn?csrf=" + url.QueryEscape(cred.Cookies["bili_jct"])
	d, _, err := c.request(ctx, "https://api.bilibili.com", route, nil, true, payload)
	if err != nil {
		return "", err
	}
	id := str(d["dyn_id_str"])
	if id == "" {
		id = str(d["dyn_id"])
	}
	if id == "" {
		return "", model.Fail("SEND_UNKNOWN", "平台未返回动态ID，请先回查")
	}
	return id, nil
}
func (c *Client) Dynamic(ctx context.Context, id string) (model.Dynamic, error) {
	d, _, err := c.request(ctx, "https://api.bilibili.com", "/x/polymer/web-dynamic/v1/detail", url.Values{"id": {id}}, false, nil)
	item := obj(d["item"])
	modules := obj(item["modules"])
	v := model.Dynamic{ID: str(item["id_str"]), UID: str(obj(modules["module_author"])["mid"]), Text: str(obj(obj(modules["module_dynamic"])["desc"])["text"])}
	return v, err
}
func (c *Client) Visible(ctx context.Context, aid, root, id string) (model.Visibility, error) {
	if root == "0" {
		root = id
	}
	v, err := c.Target(ctx, aid, root, id)
	result := model.Visibility{Found: err == nil && v.ID == id, State: v.State, RootState: v.RootState}
	return result, err
}

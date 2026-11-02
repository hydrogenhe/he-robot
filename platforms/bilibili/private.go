package bilibili

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"gpt-robot/model"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// PrivateMessages 仅查询指定双人会话，不读取其他会话、联系人或未读总量。
func (c *Client) PrivateMessages(ctx context.Context, sender, peer string) (*model.PrivatePage, error) {
	cred, err := c.credentials(ctx)
	if err != nil {
		return nil, err
	}
	if cred == nil || cred.UID != sender || !model.ValidNumber(peer) || peer == sender {
		return nil, model.Fail("ACCOUNT", "私信账号不匹配")
	}
	params := url.Values{"talker_id": {peer}, "session_type": {"1"}, "size": {"20"}, "sender_device_id": {"1"}, "build": {"0"}, "mobi_app": {"web"}}
	data, _, err := c.request(ctx, "https://api.vc.bilibili.com", "/svr_sync/v1/svr_sync/fetch_session_msgs", params, false, nil, "https://message.bilibili.com/")
	if err != nil {
		return nil, err
	}
	result := &model.PrivatePage{Messages: []model.PrivateMessage{}, HasMore: number(data["has_more"]) != 0}
	for _, raw := range list(data["messages"]) {
		row := obj(raw)
		from, to := str(row["sender_uid"]), str(row["receiver_id"])
		if !((from == sender && to == peer) || (from == peer && to == sender)) || number(row["receiver_type"]) != 1 {
			continue
		}
		seq, _ := strconv.ParseInt(str(row["msg_seqno"]), 10, 64)
		stamp, _ := strconv.ParseInt(str(row["timestamp"]), 10, 64)
		message := model.PrivateMessage{Key: str(row["msg_key"]), Seq: seq, Sender: from, Receiver: to, Timestamp: stamp, Withdrawn: number(row["msg_status"]) != 0}
		if !message.Withdrawn && number(row["msg_type"]) == 1 {
			var content struct {
				Content string `json:"content"`
			}
			if json.Unmarshal([]byte(str(row["content"])), &content) == nil {
				message.Text = model.Clip(content.Content, 500)
			}
		}
		if !message.Withdrawn && (number(row["msg_type"]) == 2 || number(row["msg_type"]) == 6) {
			var content struct {
				URL       string `json:"url"`
				Width     int    `json:"width"`
				Height    int    `json:"height"`
				ImageType string `json:"imageType"`
			}
			if json.Unmarshal([]byte(str(row["content"])), &content) == nil && validImageURL(content.URL) {
				message.Image = &model.ImageAttachment{URL: content.URL, Width: content.Width, Height: content.Height, Format: content.ImageType}
			}
		}
		if model.ValidNumber(message.Key) && seq > 0 {
			result.Messages = append(result.Messages, message)
		}
	}
	return result, nil
}
func wbiPart(value string) string {
	u, err := url.Parse(value)
	if err != nil {
		return ""
	}
	name := path.Base(u.Path)
	return strings.TrimSuffix(name, path.Ext(name))
}
func wbiKey(data map[string]any) string {
	images := obj(data["wbi_img"])
	raw := wbiPart(str(images["img_url"])) + wbiPart(str(images["sub_url"]))
	if len(raw) < 64 {
		return ""
	}
	order := []int{46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35, 27, 43, 5, 49, 33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13}
	var key strings.Builder
	for _, index := range order {
		key.WriteByte(raw[index])
	}
	return key.String()
}
func (c *Client) SendPrivate(ctx context.Context, sender, peer, text, device string) (string, error) {
	if !model.Text(text, 500) {
		return "", model.Fail("INPUT", "私信正文不合法")
	}
	content, _ := json.Marshal(map[string]string{"content": text})
	return c.sendPrivateContent(ctx, sender, peer, "1", content, device)
}

func (c *Client) sendPrivateContent(ctx context.Context, sender, peer, kind string, content []byte, device string) (string, error) {
	cred, err := c.credentials(ctx)
	if err != nil {
		return "", err
	}
	if cred == nil || cred.UID != sender || !model.ValidNumber(peer) || peer == sender || !model.ValidID(device) {
		return "", model.Fail("ACCOUNT", "私信发送参数或账号不匹配")
	}
	c.mu.Lock()
	key := c.wbi
	c.mu.Unlock()
	if key == "" {
		return "", model.Fail("LOGIN", "请先核验账号以取得私信签名")
	}
	query := url.Values{"w_sender_uid": {sender}, "w_receiver_id": {peer}, "w_dev_id": {device}, "wts": {strconv.FormatInt(time.Now().Unix(), 10)}}
	sum := md5.Sum([]byte(query.Encode() + key))
	query.Set("w_rid", hex.EncodeToString(sum[:]))
	params := url.Values{"msg[sender_uid]": {sender}, "msg[receiver_id]": {peer}, "msg[receiver_type]": {"1"}, "msg[msg_type]": {kind}, "msg[msg_status]": {"0"}, "msg[timestamp]": {strconv.FormatInt(time.Now().Unix(), 10)}, "msg[new_face_version]": {"1"}, "msg[content]": {string(content)}, "msg[dev_id]": {device}, "csrf": {cred.Cookies["bili_jct"]}, "csrf_token": {cred.Cookies["bili_jct"]}, "build": {"0"}, "mobi_app": {"web"}}
	data, _, err := c.request(ctx, "https://api.vc.bilibili.com", "/web_im/v1/web_im/send_msg?"+query.Encode(), params, true, nil, "https://message.bilibili.com/")
	if err != nil {
		return "", err
	}
	messageKey := str(data["msg_key"])
	if !model.ValidNumber(messageKey) || messageKey == "0" {
		return "", model.Fail("SEND_UNKNOWN", "平台未返回私信ID，结果未知，不可重发")
	}
	return messageKey, nil
}

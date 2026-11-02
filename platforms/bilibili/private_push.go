package bilibili

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protowire"
	"gpt-robot/model"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// privateBroadcastPath 网页私信通知订阅路径；不订阅直播、弹幕或其他平台业务。
const privateBroadcastPath = "/bilibili.broadcast.message.im.Notify/WatchNotify"

// WatchPrivate 单次只读推送连接，只给绑定双人会话发无正文唤醒；重连由业务调度负责。
func (c *Client) WatchPrivate(ctx context.Context, sender, peer string, notify func(*model.PrivatePushEvent)) error {
	credentials, err := c.credentials(ctx)
	if err != nil {
		return err
	}
	if credentials == nil || credentials.UID != sender || !model.ValidNumber(peer) || peer == sender || notify == nil {
		return model.Fail("ACCOUNT", "推送账号或绑定会话不符")
	}
	headers := http.Header{"Origin": []string{"https://message.bilibili.com"}, "User-Agent": []string{"Mozilla/5.0"}}
	if credentials.UserAgent != "" {
		headers.Set("User-Agent", credentials.UserAgent)
	}
	request := &http.Request{Header: headers}
	for name, value := range credentials.Cookies {
		if SessionCookieAllowed(name) {
			request.AddCookie(&http.Cookie{
				Name:  name,
				Value: value,
			})
		}
	}
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 10 * time.Second,
		Subprotocols:     []string{"proto"},
	}
	if transport, ok := c.http.Transport.(*http.Transport); ok {
		dialer.NetDialContext = transport.DialContext
		dialer.TLSClientConfig = transport.TLSClientConfig
		dialer.Proxy = transport.Proxy
	}
	socket, response, err := dialer.DialContext(ctx, "wss://broadcast.chat.bilibili.com:7826/sub", headers)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err != nil {
		return model.Fail("PUSH_NETWORK", "私信推送连接未建立")
	}
	defer socket.Close()
	if socket.Subprotocol() != "proto" {
		return model.Fail("PUSH_PROTOCOL", "推送协议未被接受")
	}
	socket.SetReadLimit(64 * 1024)
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	go closePrivateSocket(run, socket)
	writer := privateBroadcastWriter{
		socket:   socket,
		sequence: 1,
	}
	auth := broadcastPut(nil, 1, []byte(model.ID()))
	if err := writer.send("/bilibili.broadcast.v1.Broadcast/Auth", "bilibili.broadcast.v1.AuthReq", auth); err != nil {
		return model.Fail("PUSH_NETWORK", "推送认证发送失败")
	}
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	messages := make(chan []byte, 1)
	failures := make(chan error, 1)
	go readPrivateBroadcast(run, socket, messages, failures)
	authenticated, subscribed := false, false
	subscribeSequence := uint64(0)
	ready := time.NewTimer(15 * time.Second)
	defer ready.Stop()
	for {
		select {
		case <-run.Done():
			return run.Err()
		case <-ready.C:
			if !subscribed {
				return model.Fail("PUSH_PROTOCOL", "推送订阅未确认")
			}
		case <-heartbeat.C:
			if authenticated {
				if err := writer.send("/bilibili.broadcast.v1.Broadcast/Heartbeat", "bilibili.broadcast.v1.HeartbeatReq", nil); err != nil {
					return model.Fail("PUSH_NETWORK", "推送心跳失败")
				}
			}
		case err := <-failures:
			var business *model.Error
			if errors.As(err, &business) {
				return business
			}
			return model.Fail("PUSH_NETWORK", "私信推送连接已断开")
		case raw := <-messages:
			if _, _, err := broadcastFields(raw, 1); err != nil {
				return model.Fail("PUSH_PROTOCOL", "推送数据帧无效")
			}
			path := string(broadcastBytes(raw, 2))
			options := broadcastBytes(raw, 1)
			if broadcastInt(options, 3) != 0 {
				ack := protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), broadcastInt(options, 1))
				ack = broadcastPut(ack, 2, broadcastBytes(options, 5))
				ack = broadcastPut(ack, 3, []byte(path))
				ack = protowire.AppendVarint(protowire.AppendTag(ack, 4, protowire.VarintType), broadcastInt(options, 7))
				if err := writer.send("/bilibili.broadcast.v1.Broadcast/MessageAck", "bilibili.broadcast.v1.MessageAckReq", ack); err != nil {
					return model.Fail("PUSH_NETWORK", "通知协议确认失败")
				}
			}

			if broadcastInt(broadcastBytes(options, 4), 1) != 0 {
				return model.Fail("PUSH_AUTH", "推送认证或订阅被拒绝")
			}
			switch path {
			case "/bilibili.broadcast.v1.Broadcast/Auth":
				if authenticated {
					continue
				}
				if broadcastInt(options, 2) != 1 {
					return model.Fail("PUSH_PROTOCOL", "推送认证回执不匹配")
				}
				authenticated = true
				body := broadcastPut(nil, 1, []byte(privateBroadcastPath))
				subscribeSequence = writer.sequence
				if err := writer.send("/bilibili.broadcast.v1.Broadcast/Subscribe", "bilibili.broadcast.v1.TargetPath", body); err != nil {
					return model.Fail("PUSH_NETWORK", "推送订阅发送失败")
				}
			case "/bilibili.broadcast.v1.Broadcast/Subscribe":
				if !authenticated || broadcastInt(options, 2) != subscribeSequence {
					return model.Fail("PUSH_PROTOCOL", "推送订阅回执不匹配")
				}
				subscribed = true
				ready.Stop()
				event := model.PrivatePushEvent{Connected: true}
				notify(&event)
			default:
				if !subscribed {
					continue
				}
				if strings.Contains(path, "/Heartbeat") {
					continue
				}
				any := broadcastBytes(raw, 3)
				bodyType := string(broadcastBytes(any, 1))
				known := strings.Contains(path, "im.Notify") || strings.HasSuffix(bodyType, "bilibili.broadcast.message.im.NotifyRsp")
				if !known {
					event := model.PrivatePushEvent{Connected: true, Observed: true, Path: model.Clip(path, 128), Metadata: "unrecognized_type"}
					notify(&event)
					continue
				}
				bodyCheck := broadcastBytes(broadcastBytes(raw, 3), 2)
				if strconv.FormatUint(broadcastInt(bodyCheck, 1), 10) == sender && broadcastInt(bodyCheck, 2) == 2 {
					return model.Fail("PUSH_AUTH", "私信推送会话已结束")
				}
				kind := privateBroadcastMatchKind(raw, sender, peer)
				matched := subscribed && kind != ""
				if !matched {
					payload := broadcastBytes(bodyCheck, 3)
					metadata := fmt.Sprintf("uid_present=%t cmd=%d payload_type=%d message=%t info=%t", broadcastInt(bodyCheck, 1) > 0, broadcastInt(bodyCheck, 2), broadcastInt(bodyCheck, 4), len(broadcastBytes(payload, 2)) > 0, len(broadcastBytes(payload, 3)) > 0)
					event := model.PrivatePushEvent{Connected: true, Observed: true, Path: model.Clip(path, 128), Metadata: metadata}
					notify(&event)
				}
				if matched {
					body := broadcastBytes(broadcastBytes(raw, 3), 2)
					payload := broadcastBytes(body, 3)
					message := broadcastBytes(payload, 2)
					key := fmt.Sprintf("%x", sha256.Sum256(payload))
					if kind == "hint" {
						key = ""
						if id := broadcastInt(options, 1); id > 0 {
							key = "notice:" + strconv.FormatUint(id, 10)
						}
					}
					if id := broadcastInt(message, 11); id > 0 {
						key = "message:" + strconv.FormatUint(id, 10)
						if broadcastInt(message, 12) > 0 || broadcastInt(message, 13) > 0 || broadcastInt(message, 15) > 0 {
							key += ":withdraw:" + strconv.FormatUint(broadcastInt(message, 15), 10)
						}
					}
					event := model.PrivatePushEvent{
						GenericHint: kind == "hint",
						Observed:    true, Path: model.Clip(path, 128), Metadata: "matched_" + kind,
						Key:       key,
						Connected: true,
						Wake:      true,
					}
					notify(&event)
				}
			}
		}
	}
}
func closePrivateSocket(ctx context.Context, socket *websocket.Conn) { <-ctx.Done(); socket.Close() }
func readPrivateBroadcast(ctx context.Context, socket *websocket.Conn, messages chan<- []byte, failures chan<- error) {
	window := time.Now()
	frames := 0
	for {
		socket.SetReadDeadline(time.Now().Add(55 * time.Second))
		kind, data, err := socket.ReadMessage()
		if err != nil {
			select {
			case failures <- err:
			case <-ctx.Done():
			}
			return
		}
		if time.Since(window) >= time.Second {
			window = time.Now()
			frames = 0
		}
		frames++
		if frames > 100 {
			select {
			case failures <- model.Fail("PUSH_RATE", "通知过密，退回受控查询"):
			case <-ctx.Done():
			}
			return
		}
		if kind != websocket.BinaryMessage {
			continue
		}
		select {
		case messages <- data:
		case <-ctx.Done():
			return
		}
	}
}

// privateBroadcastMatchKind 仅看通知元数据；无会话身份的账号未读变化只提示核查绑定大号。
func privateBroadcastMatchKind(raw []byte, sender, peer string) string {
	body := broadcastBytes(broadcastBytes(raw, 3), 2)
	uid, command, payloadType := broadcastInt(body, 1), broadcastInt(body, 2), broadcastInt(body, 4)
	if uid != 0 && strconv.FormatUint(uid, 10) != sender || command != 0 && command != 1 || payloadType > 1 {
		return ""
	}
	payload := broadcastBytes(body, 3)
	message := broadcastBytes(payload, 2)
	if len(message) > 0 {
		from, to := strconv.FormatUint(broadcastInt(message, 1), 10), strconv.FormatUint(broadcastInt(message, 3), 10)
		if from == peer && to == sender && broadcastInt(message, 2) <= 1 && (broadcastInt(message, 7) > 0 || broadcastInt(message, 12) > 0 || broadcastInt(message, 15) > 0) {
			return "direct"
		}
		return ""
	}
	info := broadcastBytes(payload, 3)
	if len(info) > 0 {
		if strconv.FormatUint(broadcastInt(info, 2), 10) == peer && broadcastInt(info, 3) <= 1 {
			return "direct"
		}
		return ""
	}
	commands, _, err := broadcastFields(payload, 4)
	if err != nil {
		return ""
	}
	generic, specific := false, false
	for _, command := range commands {
		unread, _, _ := broadcastFields(command, 1)
		if len(unread) > 0 {
			generic = true
		}
		for _, field := range []int{2, 4} {
			session := broadcastBytes(broadcastBytes(command, protowire.Number(field)), 1)
			if len(session) > 0 {
				specific = true
			}
			if strconv.FormatUint(broadcastInt(broadcastBytes(session, 1), 1), 10) == peer {
				return "direct"
			}
		}
	}
	if generic && !specific {
		return "hint"
	}
	return ""
}

var _ model.PrivatePushSource = (*Client)(nil)

type privateBroadcastWriter struct {
	socket   *websocket.Conn
	sequence uint64
}

func (w *privateBroadcastWriter) send(path, kind string, body []byte) error {
	w.socket.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err := w.socket.WriteMessage(websocket.BinaryMessage, broadcastPacket(w.sequence, path, kind, body))
	w.sequence++
	return err
}

package bilibili_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protowire"
	"gpt-robot/model"
	"gpt-robot/platforms/bilibili"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func pushBytes(number protowire.Number, value []byte) []byte {
	data := protowire.AppendTag(nil, number, protowire.BytesType)
	return protowire.AppendBytes(data, value)
}
func pushInt(number protowire.Number, value uint64) []byte {
	data := protowire.AppendTag(nil, number, protowire.VarintType)
	return protowire.AppendVarint(data, value)
}
func pushResponse(path string, sequence uint64, body []byte, ack ...bool) []byte {
	options := pushInt(2, sequence)
	if len(ack) > 0 && ack[0] {
		options = append(options, pushInt(1, 9001)...)
		options = append(options, pushInt(3, 1)...)
		options = append(options, pushBytes(5, []byte("server"))...)
	}
	data := pushBytes(1, options)
	data = append(data, pushBytes(2, []byte(path))...)
	any := append(pushBytes(1, []byte("test")), pushBytes(2, body)...)
	return append(data, pushBytes(3, any)...)
}
func TestPrivatePushAuthenticatesFiltersOtherAccountsAndKeepsUint64(t *testing.T) {
	for _, mode := range []string{"normal", "optional", "hint", "ack", "flood"} {
		t.Run(mode, func(t *testing.T) {
			failures := make(chan error, 1)
			ackDone := make(chan struct{})
			upgrader := websocket.Upgrader{
				Subprotocols: []string{"proto"},
				CheckOrigin:  func(*http.Request) bool { return true },
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.Header.Get("Cookie"), "SESSDATA=fake") || r.Header.Get("Origin") != "https://message.bilibili.com" {
					failures <- fmt.Errorf("auth headers missing")
					return
				}
				socket, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					failures <- err
					return
				}
				defer socket.Close()
				_, auth, err := socket.ReadMessage()
				if err != nil || !strings.Contains(string(auth), "Broadcast/Auth") {
					failures <- fmt.Errorf("auth request missing")
					return
				}
				socket.WriteMessage(websocket.BinaryMessage, pushResponse("/bilibili.broadcast.v1.Broadcast/Auth", 1, nil))
				_, subscribe, err := socket.ReadMessage()
				if err != nil || !strings.Contains(string(subscribe), "Notify/WatchNotify") {
					failures <- fmt.Errorf("subscription missing")
					return
				}
				socket.WriteMessage(websocket.BinaryMessage, pushResponse("/bilibili.broadcast.v1.Broadcast/Subscribe", 2, nil))
				if mode == "flood" {
					for i := 0; i < 200; i++ {
						if socket.WriteMessage(websocket.BinaryMessage, pushResponse("/bilibili.broadcast.v1.Broadcast/Heartbeat", 3, nil)) != nil {
							break
						}
					}
					socket.ReadMessage()
					return
				}
				for _, peer := range []uint64{456, 789} {
					if (mode == "hint" || mode == "ack") && peer == 456 {
						continue
					}
					message := append(pushInt(1, peer), pushInt(2, 1)...)
					message = append(message, pushInt(3, 123)...)
					message = append(message, pushBytes(6, []byte("private content must not leave transport"))...)
					message = append(message, pushInt(7, 20)...)
					message = append(message, pushInt(11, 99007199254740993)...)
					body := append(pushInt(1, 123), pushInt(2, 1)...)
					if mode == "optional" {
						body = nil
					}
					body = append(body, pushBytes(3, pushBytes(2, message))...)
					if mode == "hint" {
						body = append(pushInt(1, 123), pushInt(2, 1)...)
						body = append(body, pushBytes(3, pushBytes(4, pushBytes(1, nil)))...)
						body = append(body, pushInt(4, 1)...)
					}
					path := "/bilibili.broadcast.message.im.Notify/WatchNotify"
					if mode == "optional" {
						path = "/bilibili.broadcast.message.im.Notify/Notify"
					}
					socket.WriteMessage(websocket.BinaryMessage, pushResponse(path, 3, body, mode == "ack"))
				}
				if mode == "ack" {
					_, packet, err := socket.ReadMessage()
					if err != nil || !strings.Contains(string(packet), "/Broadcast/MessageAck") && !strings.Contains(string(packet), ".Broadcast/MessageAck") {
						failures <- fmt.Errorf("required transport ack missing")
					}
					close(ackDone)
					return
				}
				socket.ReadMessage()
			}))
			defer server.Close()
			pool := x509.NewCertPool()
			pool.AddCert(server.Certificate())
			transport := &http.Transport{TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				ServerName: "example.com",
			}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				if address != "broadcast.chat.bilibili.com:7826" {
					return nil, fmt.Errorf("unexpected target")
				}
				dialer := net.Dialer{}
				return dialer.DialContext(ctx, network, server.Listener.Addr().String())
			}}
			credentials := &model.Credentials{
				UID:     "123",
				Cookies: map[string]string{"SESSDATA": "fake", "DedeUserID": "123"},
			}
			client := bilibili.New(func(context.Context) (*model.Credentials, error) { return credentials, nil }, &http.Client{Transport: transport})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			wakes := []string{}
			err := client.WatchPrivate(ctx, "123", "789", func(event *model.PrivatePushEvent) {
				if event.Wake {
					wakes = append(wakes, event.Key)
					cancel()
				}
			})
			if mode == "flood" {
				var business *model.Error
				if !errors.As(err, &business) || business.Code != "PUSH_RATE" || len(wakes) != 0 {
					t.Fatal("notification flood not bounded", err, wakes)
				}
				return
			}
			expected := "message:99007199254740993"
			if mode == "hint" {
				expected = ""
			}
			if ctx.Err() == nil || len(wakes) != 1 || wakes[0] != expected {
				t.Fatal("push filter or integer precision", wakes, err)
			}
			if mode == "ack" {
				select {
				case <-ackDone:
				case <-time.After(time.Second):
					t.Fatal("ack server did not finish")
				}
			}
			select {
			case err := <-failures:
				t.Fatal(err)
			default:
			}
		})
	}

}

package bilibili_test

import (
	"context"
	"net/http"
	"testing"
)

func TestPrivateReadFiltersPeerAndPreservesLargeIDsAndWithdrawal(t *testing.T) {
	c := client(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.vc.bilibili.com" || r.URL.Query().Get("talker_id") != "789" || r.Header.Get("Referer") != "https://message.bilibili.com/" {
			t.Fatal("wrong private target")
		}
		return response(`{"code":0,"data":{"has_more":1,"messages":[{"sender_uid":789,"receiver_id":123,"receiver_type":1,"msg_type":1,"msg_status":0,"msg_seqno":1,"msg_key":999999999999999999,"timestamp":123,"content":"{\"content\":\"你好\"}"},{"sender_uid":456,"receiver_id":123,"receiver_type":1,"msg_seqno":2,"msg_key":22,"content":"{\"content\":\"陌生人\"}"},{"sender_uid":789,"receiver_id":123,"receiver_type":1,"msg_type":1,"msg_status":1,"msg_seqno":3,"msg_key":33,"content":"{\"content\":\"撤回正文\"}"}]}}`), nil
	})
	page, err := c.PrivateMessages(context.Background(), "123", "789")
	if err != nil || len(page.Messages) != 2 || page.Messages[0].Key != "999999999999999999" || page.Messages[0].Text != "你好" || page.Messages[1].Text != "" || !page.Messages[1].Withdrawn {
		t.Fatal(page, err)
	}
}
func TestPrivateWriteBoundAccountSignedFormAndMissingKeyUnknown(t *testing.T) {
	calls := 0
	c := client(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path == "/x/web-interface/nav" {
			return response(`{"code":0,"data":{"isLogin":true,"mid":123,"wbi_img":{"img_url":"https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png","sub_url":"https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png"}}}`), nil
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Method != "POST" || r.URL.Path != "/web_im/v1/web_im/send_msg" || r.Header.Get("Origin") != "https://message.bilibili.com" || r.URL.Query().Get("w_rid") == "" || r.PostForm.Get("msg[receiver_id]") != "789" || r.PostForm.Get("msg[sender_uid]") != "123" || r.PostForm.Get("csrf") != "csrf-test" || r.PostForm.Get("msg[content]") != `{"content":"一条私信"}` {
			t.Fatal("wrong private request")
		}
		return response(`{"code":0,"data":{}}`), nil
	})
	if _, err := c.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err := c.SendPrivate(context.Background(), "123", "789", "一条私信", "11111111-1111-4111-8111-111111111111")
	if err == nil || calls != 2 {
		t.Fatal("missing receipt silently succeeded or retried", err, calls)
	}
	if _, err := c.SendPrivate(context.Background(), "456", "789", "越权", "11111111-1111-4111-8111-111111111111"); err == nil || calls != 2 {
		t.Fatal("wrong account reached network")
	}
}

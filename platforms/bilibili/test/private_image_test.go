package bilibili_test

import (
	"bytes"
	"context"
	"encoding/json"
	"gpt-robot/model"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
)

// privateImageNav 模拟账号核验和WBI图片密钥，不请求真实平台。
const privateImageNav = `{"code":0,"data":{"isLogin":true,"mid":123,"wbi_img":{"img_url":"https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png","sub_url":"https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png"}}}`

func TestPrivateImageParsingDownloadAndUploadSendUseFixedHosts(t *testing.T) {
	buffer := &bytes.Buffer{}
	png.Encode(buffer, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	picture := buffer.Bytes()
	calls := []string{}
	c := client(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.URL.Host+req.URL.Path)
		switch req.URL.Path {
		case "/x/web-interface/nav":
			return response(privateImageNav), nil
		case "/svr_sync/v1/svr_sync/fetch_session_msgs":
			return response(`{"code":0,"data":{"messages":[{"sender_uid":789,"receiver_id":123,"receiver_type":1,"msg_type":2,"msg_status":0,"msg_seqno":1,"msg_key":123456,"timestamp":123,"content":"{\"url\":\"https://message.biliimg.com/bfs/im_new/source.png\",\"width\":8,\"height\":8}"}]}}`), nil
		case "/bfs/im_new/source.png":
			if req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
				t.Fatal("credentials sent to image CDN")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(picture)), Header: make(http.Header)}, nil
		case "/x/dynamic/feed/draw/upload_bfs":
			if req.URL.Host != "api.bilibili.com" {
				t.Fatal("upload host")
			}
			if err := req.ParseMultipartForm(3 << 20); err != nil {
				t.Fatal(err)
			}
			if req.FormValue("biz") != "im" || req.FormValue("category") != "daily" {
				t.Fatal("not private-image upload")
			}
			file, _, err := req.FormFile("file_up")
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := io.ReadAll(file)
			file.Close()
			if !bytes.Equal(raw, picture) {
				t.Fatal("uploaded original was modified")
			}
			return response(`{"code":0,"data":{"image_url":"https://i0.hdslb.com/bfs/album/uploaded.png"}}`), nil
		case "/web_im/v1/web_im/send_msg":
			req.ParseForm()
			if req.FormValue("msg[msg_type]") != "2" || req.FormValue("msg[receiver_id]") != "789" {
				t.Fatal("wrong image message")
			}
			var content map[string]any
			if json.Unmarshal([]byte(req.FormValue("msg[content]")), &content) != nil || content["url"] != "https://i0.hdslb.com/bfs/album/uploaded.png" || content["width"] != float64(8) {
				t.Fatal("wrong image content")
			}
			return response(`{"code":0,"data":{"msg_key":999999999999999999}}`), nil
		}
		t.Fatal("unexpected external request", req.URL)
		return nil, nil
	})
	page, err := c.PrivateMessages(context.Background(), "123", "789")
	if err != nil || len(page.Messages) != 1 || page.Messages[0].Image == nil {
		t.Fatal("image missing", page, err)
	}
	image, err := c.DownloadPrivateImage(context.Background(), page.Messages[0].Image)
	if err != nil || !bytes.Equal(image.Data, picture) {
		t.Fatal("download", err)
	}
	if _, err := c.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	id, err := c.SendPrivateImage(context.Background(), "123", "789", image, "11111111-1111-4111-8111-111111111111")
	if err != nil || id != "999999999999999999" || len(calls) != 5 {
		t.Fatal(id, err, calls)
	}
}

func TestPrivateImageRejectsUntrustedSourcesAndMalformedFiles(t *testing.T) {
	calls := 0
	c := client(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Path == "/x/web-interface/nav" {
			return response(privateImageNav), nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("not an image")), Header: make(http.Header)}, nil
	})
	for _, address := range []string{"http://127.0.0.1/private", "https://evil.example/bfs/a.png", "https://i0.hdslb.com.evil.example/bfs/a.png", "https://user@i0.hdslb.com/bfs/a.png", "https://i0.hdslb.com:443/bfs/a.png"} {
		input := model.ImageAttachment{URL: address}
		if _, err := c.DownloadPrivateImage(context.Background(), &input); err == nil || calls != 0 {
			t.Fatal("untrusted image fetched", address)
		}
	}
	input := model.ImageAttachment{URL: "https://i0.hdslb.com/bfs/emote/test.png"}
	if _, err := c.DownloadPrivateImage(context.Background(), &input); err == nil || calls != 1 {
		t.Fatal("malformed image accepted")
	}
}

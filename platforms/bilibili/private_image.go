package bilibili

import (
	"bytes"
	"context"
	"encoding/json"
	"gpt-robot/model"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

// privateImageMaxBytes 限制学习与上传图片的原始文件大小为2MiB。
const privateImageMaxBytes = model.StickerMaxBytes

type multipartPayload struct {
	body        []byte
	contentType string
}

func validImageURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, "/bfs/") {
		return false
	}
	switch u.Host {
	case "i0.hdslb.com", "i1.hdslb.com", "i2.hdslb.com", "message.biliimg.com":
		return true
	}
	return false
}

func imageMetadata(data []byte) (int, int, string, error) {
	if len(data) == 0 || len(data) > privateImageMaxBytes {
		return 0, 0, "", model.Fail("IMAGE_SIZE", "表情图片需小于2MiB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 2048 || config.Height > 2048 || config.Width*config.Height > 4194304 {
		return 0, 0, "", model.Fail("IMAGE_FORMAT", "只支持尺寸不超过2048像素的PNG/JPEG/GIF表情")
	}
	return config.Width, config.Height, format, nil
}

func (c *Client) DownloadPrivateImage(ctx context.Context, input *model.ImageAttachment) (*model.ImageAttachment, error) {
	if input == nil || !validImageURL(input.URL) {
		return nil, model.Fail("IMAGE_SOURCE", "只接受当前私聊中的B站图片来源")
	}
	address := "https:" + strings.TrimPrefix(strings.TrimPrefix(input.URL, "https:"), "http:")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, model.Fail("IMAGE_SOURCE", "图片地址无法解析")
	}
	// CDN读取不携带会话Cookie，也不跟随重定向。
	client := *c.http
	client.Jar = nil
	res, err := client.Do(req)
	if err != nil {
		return nil, model.Fail("IMAGE_DOWNLOAD", "表情图片下载失败，没有保存")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, model.Fail("IMAGE_DOWNLOAD", "图片已失效或暂不可读取，没有保存")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, privateImageMaxBytes+1))
	if err != nil {
		return nil, model.Fail("IMAGE_DOWNLOAD", "表情图片读取失败")
	}
	w, h, format, err := imageMetadata(data)
	if err != nil {
		return nil, err
	}
	result := &model.ImageAttachment{URL: address, Width: w, Height: h, Format: format, Data: data}
	return result, nil
}

func (c *Client) NativeStickers() []model.NativeSticker {
	stickers := []model.NativeSticker{
		{Choice: model.StickerChoice{ID: "native-smile", Label: "轻松微笑"}, TextCode: "[tv_微笑]"},
		{Choice: model.StickerChoice{ID: "native-happy", Label: "开心笑"}, TextCode: "[tv_笑哭]"},
		{Choice: model.StickerChoice{ID: "native-doge", Label: "俏皮玩笑"}, TextCode: "[tv_doge]"},
	}
	return stickers
}

func (c *Client) SendPrivateImage(ctx context.Context, sender, peer string, input *model.ImageAttachment, device string) (string, error) {
	cred, err := c.credentials(ctx)
	if err != nil {
		return "", err
	}
	if cred == nil || cred.UID != sender || !model.ValidNumber(peer) || peer == sender || !model.ValidID(device) || input == nil {
		return "", model.Fail("ACCOUNT", "图片发送范围不匹配")
	}
	c.mu.Lock()
	signed := c.wbi != ""
	c.mu.Unlock()
	if !signed {
		return "", model.Fail("LOGIN", "请先核验账号，未上传表情图片")
	}
	w, h, format, err := imageMetadata(input.Data)
	if err != nil {
		return "", err
	}
	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)
	extension := format
	if extension == "jpeg" {
		extension = "jpg"
	}
	file, err := writer.CreateFormFile("file_up", "sticker."+extension)
	if err != nil {
		return "", model.Internal(err)
	}
	if _, err = file.Write(input.Data); err != nil {
		return "", model.Internal(err)
	}
	for key, value := range map[string]string{"category": "daily", "biz": "im", "csrf": cred.Cookies["bili_jct"]} {
		if err = writer.WriteField(key, value); err != nil {
			return "", model.Internal(err)
		}
	}
	if err = writer.Close(); err != nil {
		return "", model.Internal(err)
	}
	payload := &multipartPayload{body: buffer.Bytes(), contentType: writer.FormDataContentType()}
	data, _, err := c.request(ctx, "https://api.bilibili.com", "/x/dynamic/feed/draw/upload_bfs", nil, true, payload)
	if err != nil {
		return "", model.Fail("IMAGE_UPLOAD", "表情图片上传失败，未提交图片私信，不自动重试")
	}
	address := str(data["image_url"])
	if !validImageURL(address) {
		return "", model.Fail("IMAGE_UPLOAD", "平台未返回可信图片地址，未提交图片私信")
	}
	content, _ := json.Marshal(map[string]any{"url": address, "width": w, "height": h, "imageType": format, "original": 1, "size": float64(len(input.Data)) / 1024})
	return c.sendPrivateContent(ctx, sender, peer, "2", content, device)
}

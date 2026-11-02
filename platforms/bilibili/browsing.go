package bilibili

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"gpt-robot/model"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var searchMarkup = regexp.MustCompile(`<[^>]*>`)
var _ model.VideoDiscovery = (*Client)(nil)

func (c *Client) SearchVideos(ctx context.Context, keyword string) ([]model.Video, error) {
	if !model.Text(keyword, 20) {
		return nil, model.Fail("INPUT", "搜索关键词应为1–20字")
	}
	c.mu.Lock()
	key := c.wbi
	c.mu.Unlock()
	if key == "" {
		if _, err := c.Status(ctx); err != nil {
			return nil, err
		}
		c.mu.Lock()
		key = c.wbi
		c.mu.Unlock()
	}
	if key == "" {
		return nil, model.Fail("LOGIN", "无法取得视频搜索签名，请重新核验登录")
	}
	keyword = strings.Map(func(r rune) rune {
		if strings.ContainsRune("!'()*", r) {
			return -1
		}
		return r
	}, keyword)
	params := url.Values{"search_type": {"video"}, "keyword": {keyword}, "page": {"1"}, "page_size": {"20"}, "wts": {strconv.FormatInt(time.Now().Unix(), 10)}}
	canonical := strings.ReplaceAll(params.Encode(), "+", "%20")
	sum := md5.Sum([]byte(canonical + key))
	params.Set("w_rid", hex.EncodeToString(sum[:]))
	data, _, err := c.request(ctx, "https://api.bilibili.com", "/x/web-interface/wbi/search/type", params, false, nil)
	if err != nil {
		return nil, err
	}
	results := []model.Video{}
	for _, raw := range list(data["result"]) {
		d := obj(raw)
		bv := str(d["bvid"])
		if !model.ValidBV(bv) {
			continue
		}
		v := model.Video{DurationSeconds: searchDuration(str(d["duration"])), BVID: bv, Aid: str(d["aid"]), Title: model.Clip(html.UnescapeString(searchMarkup.ReplaceAllString(str(d["title"]), "")), 120), Owner: str(d["mid"]), Description: model.Clip(html.UnescapeString(searchMarkup.ReplaceAllString(str(d["description"]), "")), 500)}
		results = append(results, v)
		if len(results) >= 20 {
			break
		}
	}
	return results, nil
}

// searchDuration 将搜索返回的分:秒或时:分:秒转换为秒；异常时长返回未知。
func searchDuration(value string) int {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}
	total := 0
	for i, part := range parts {
		if part == "" {
			return 0
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return 0
			}
		}
		n, err := strconv.Atoi(part)
		if err != nil || n > 100000 || (i > 0 && n >= 60) {
			return 0
		}
		total = total*60 + n
	}
	return total
}

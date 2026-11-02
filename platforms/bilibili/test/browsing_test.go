package bilibili_test

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
)

func TestKeywordSearchUsesOfficialSignedReadOnlyEndpoint(t *testing.T) {
	calls := 0
	c := client(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" {
			t.Fatal("unexpected write")
		}
		if r.URL.Path == "/x/web-interface/nav" {
			return response(`{"code":0,"data":{"isLogin":true,"mid":123,"uname":"fixture","wbi_img":{"img_url":"https://i0.hdslb.com/bfs/wbi/abcdefghijklmnopqrstuvwxyzABCDEF.png","sub_url":"https://i0.hdslb.com/bfs/wbi/GHIJKLMNOPQRSTUVWXYZ0123456789ab.png"}}}`), nil
		}
		if r.URL.Host != "api.bilibili.com" || r.URL.Path != "/x/web-interface/wbi/search/type" {
			t.Fatal(r.URL)
		}
		q := r.URL.Query()
		signature := q.Get("w_rid")
		q.Del("w_rid")
		raw := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ab"
		order := []int{46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35, 27, 43, 5, 49, 33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13}
		key := ""
		for _, i := range order {
			key += string(raw[i])
		}
		sum := md5.Sum([]byte(strings.ReplaceAll(q.Encode(), "+", "%20") + key))
		if q.Get("keyword") != "足球 比赛" || q.Get("search_type") != "video" || signature != hex.EncodeToString(sum[:]) {
			t.Fatal("invalid search signature")
		}
		return response(`{"code":0,"data":{"result":[{"bvid":"BV18VLu6JEit","aid":100,"title":"足球<em>比赛</em>","description":"公开简介","mid":321,"duration":"10:00"},{"bvid":"invalid","title":"discard"}]}}`), nil
	})
	rows, err := c.SearchVideos(context.Background(), "足球 比赛")
	if err != nil || len(rows) != 1 || rows[0].Title != "足球比赛" || rows[0].DurationSeconds != 600 || calls != 2 {
		t.Fatal(rows, err, calls)
	}
}

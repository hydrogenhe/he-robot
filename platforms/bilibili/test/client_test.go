package bilibili_test

import (
	"context"
	"gpt-robot/model"
	"gpt-robot/platforms/bilibili"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func client(fn roundTrip) *bilibili.Client {
	httpClient := &http.Client{Transport: fn}
	credentials := func(context.Context) (*model.Credentials, error) {
		c := &model.Credentials{UID: "123", Cookies: map[string]string{"SESSDATA": "fake", "bili_jct": "csrf-test"}}
		return c, nil
	}
	return bilibili.New(credentials, httpClient)
}
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func TestDynamicCSRFAndNoRetries(t *testing.T) {
	calls := 0
	c := client(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/x/dynamic/feed/create/dyn" || r.URL.Query().Get("csrf") != "csrf-test" || r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatal(r.URL, r.Header)
		}
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "正文") {
			t.Fatal(string(b))
		}
		return response(`{"code":0,"data":{"dyn_id":1264928161445969921}}`), nil
	})
	id, err := c.SendDynamic(context.Background(), "正文")
	if err != nil || id != "1264928161445969921" || calls != 1 {
		t.Fatal(id, err, calls)
	}
}
func TestCommentLargeIDsAndForm(t *testing.T) {
	c := client(func(r *http.Request) (*http.Response, error) {
		r.ParseForm()
		if r.URL.Path != "/x/v2/reply/add" || r.Form.Get("root") != "1234567890123456789" || r.Form.Get("parent") != "1234567890123456790" || r.Form.Get("csrf") != "csrf-test" {
			t.Fatal(r.Form)
		}
		return response(`{"code":0,"data":{"rpid":1234567890123456791}}`), nil
	})
	id, err := c.Send(context.Background(), "100", "测试", "1234567890123456789", "1234567890123456790", "BV161nZ6JE6b")
	if err != nil || id.ID != "1234567890123456791" {
		t.Fatal(id, err)
	}
}
func TestQRSetCookiePreservesEncodedValues(t *testing.T) {
	c := client(func(r *http.Request) (*http.Response, error) {
		res := response(`{"code":0,"data":{"code":0}}`)
		res.Header.Add("Set-Cookie", "SESSDATA=a%2Cb%3Dc; Path=/; HttpOnly")
		res.Header.Add("Set-Cookie", "bili_jct=test; Path=/")
		res.Header.Add("Set-Cookie", "DedeUserID=1234567890123456; Path=/")
		return res, nil
	})
	result, err := c.Poll(context.Background(), "key")
	if err != nil || result.Credentials.UID != "1234567890123456" || result.Credentials.Cookies["SESSDATA"] != "a%2Cb%3Dc" {
		t.Fatal(result, err)
	}
}
func TestUnknownMalformedWriteNeverRetries(t *testing.T) {
	calls := 0
	c := client(func(*http.Request) (*http.Response, error) { calls++; return response("broken"), nil })
	_, err := c.SendDynamic(context.Background(), "测试")
	business, ok := err.(*model.Error)
	if !ok || business.Code != "SEND_UNKNOWN" || calls != 1 {
		t.Fatal(err, calls)
	}
}

func TestPlatformState17IsPreserved(t *testing.T) {
	c := client(func(*http.Request) (*http.Response, error) {
		return response(`{"code":0,"data":{"root":{"rpid":1234567890123456791,"state":17,"member":{"mid":"123"},"content":{"message":"测试"}},"replies":[]}}`), nil
	})
	v, err := c.Visible(context.Background(), "100", "0", "1234567890123456791")
	if err != nil || !v.Found || v.State == nil || *v.State != 17 {
		t.Fatal(v, err)
	}
}

func TestHiddenRootStateIsPreservedForChild(t *testing.T) {
	c := client(func(*http.Request) (*http.Response, error) {
		return response(`{"code":0,"data":{"root":{"rpid":1001,"state":17},"replies":[{"rpid":1002,"state":0,"root":1001,"parent":1001}]}}`), nil
	})
	v, err := c.Visible(context.Background(), "100", "1001", "1002")
	if err != nil || !v.Found || v.State == nil || *v.State != 0 || v.RootState == nil || *v.RootState != 17 {
		t.Fatal(v, err)
	}
}

func TestWebCompatiblePrimaryCommentRequestAndReceipt(t *testing.T) {
	calls := 0
	transport := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/x/v2/reply/add" {
			t.Fatal("wrong endpoint")
		}
		if r.Header.Get("User-Agent") != "Captured browser agent" || r.Header.Get("Referer") != "https://www.bilibili.com/video/BV18VLu6JEit/" || r.Header.Get("Origin") != "https://www.bilibili.com" {
			t.Fatal("missing captured session context")
		}
		if _, err := r.Cookie("buvid3"); err != nil {
			t.Fatal("device cookie discarded")
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"root", "parent"} {
			if _, ok := r.PostForm[key]; ok {
				t.Fatal("primary comment sent root or parent")
			}
		}
		if r.PostForm.Get("gaia_source") != "main_web" || r.PostForm.Get("statistics") != `{"appId":100,"platform":5}` || r.PostForm.Get("at_name_to_mid") != "{}" || r.PostForm.Get("csrf") != "test-csrf" {
			t.Fatal("form differs from captured web request")
		}
		if r.URL.Query().Has("b_wet") || r.URL.Query().Has("w_rid") {
			t.Fatal("one-time browser tokens replayed")
		}
		return response(`{"code":0,"data":{"success_toast":"已提交审核","rpid_str":"1234567890123456791","reply":{"rpid_str":"1234567890123456791","state":17}}}`), nil
	})}
	credentials := func(context.Context) (*model.Credentials, error) {
		v := &model.Credentials{UID: "123", UserAgent: "Captured browser agent", Cookies: map[string]string{"SESSDATA": "test", "bili_jct": "test-csrf", "buvid3": "actual-device"}}
		return v, nil
	}
	c := bilibili.New(credentials, transport)
	receipt, err := c.Send(context.Background(), "116599185016606", "测试", "0", "0", "BV18VLu6JEit")
	if err != nil || receipt.ID != "1234567890123456791" || receipt.State == nil || *receipt.State != 17 || receipt.Message != "已提交审核" || calls != 1 {
		t.Fatal(receipt, err, calls)
	}
}
func TestQRPreservesServerDeviceCookiesWithoutInventingTokens(t *testing.T) {
	c := client(func(*http.Request) (*http.Response, error) {
		res := response(`{"code":0,"data":{"code":0}}`)
		for _, cookie := range []string{"SESSDATA=fake", "bili_jct=fake", "DedeUserID=123", "buvid3=device3", "buvid4=device4", "buvid_fp=actual-fp", "tracking_unrelated=discard"} {
			res.Header.Add("Set-Cookie", cookie+"; Path=/")
		}
		return res, nil
	})
	result, err := c.Poll(context.Background(), "key")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"buvid3", "buvid4", "buvid_fp"} {
		if result.Credentials.Cookies[key] == "" {
			t.Fatal("server device cookie lost")
		}
	}
	if _, ok := result.Credentials.Cookies["tracking_unrelated"]; ok {
		t.Fatal("unrelated tracking cookie retained")
	}
}

package middleware

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"gpt-robot/model"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func Equal(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func Secure(w http.ResponseWriter, r *http.Request, origin string) error {
	u, err := url.Parse(origin)
	if err != nil {
		return model.Internal(err)
	}
	if r.Host != u.Host {
		return &model.Error{Code: "HOST", Message: "无效请求地址", Status: 403}
	}
	if (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != origin) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return &model.Error{Code: "ORIGIN", Message: "拒绝其他网站访问本地服务", Status: 403}
	}
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	return nil
}
func Body(w http.ResponseWriter, r *http.Request) (json.RawMessage, error) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return nil, &model.Error{Code: "TYPE", Message: "需要JSON请求", Status: 415}
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 128000))
	if err != nil {
		return nil, &model.Error{Code: "SIZE", Message: "请求内容过大", Status: 413}
	}
	if !json.Valid(b) {
		return nil, model.Fail("JSON", "JSON格式错误")
	}
	return b, nil
}
func Decode(raw json.RawMessage, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return model.Fail("INPUT", "输入格式或字段不符合要求")
	}
	return nil
}

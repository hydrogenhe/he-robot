package bilibili

import (
	"errors"
	"gpt-robot/model"
	"log/slog"
	"net"
	"net/http"
	"syscall"
)

// networkCause 的可打印内容仅为类别；原始错误仍可通过errors.Is/As检查。
type networkCause struct {
	err  error
	kind string
}

func (e *networkCause) Error() string { return "B站连接失败（" + e.kind + "）" }
func (e *networkCause) Unwrap() error { return e.err }

// requestNetworkError 保留内部错误链；日志只输出固定端点和网络类别，
// 不输出原始URL、查询参数、Cookie、签名或私信正文。
func requestNetworkError(req *http.Request, err error, write bool) error {
	kind := "transport"
	var dns *net.DNSError
	var network net.Error
	switch {
	case errors.As(err, &dns):
		kind = "dns"
	case errors.As(err, &network) && network.Timeout():
		kind = "timeout"
	case errors.Is(err, syscall.ECONNREFUSED):
		kind = "connection-refused"
	case errors.Is(err, syscall.ECONNRESET):
		kind = "connection-reset"
	}
	code, message := "NETWORK", "无法连接B站，请检查网络"
	if write {
		code, message = "SEND_UNKNOWN", "发送连接中断，结果不明；请回查，不要重新发送"
	}
	slog.Warn("B站HTTP连接失败", "method", req.Method, "host", req.URL.Hostname(), "path", req.URL.Path, "kind", kind, "code", code)
	failure := &model.Error{
		Code:    code,
		Message: message,
		Status:  400,
		Err:     &networkCause{err: err, kind: kind},
	}
	return failure
}

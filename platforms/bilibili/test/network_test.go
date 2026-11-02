package bilibili_test

import (
	"bytes"
	"context"
	"errors"
	"gpt-robot/model"
	"log/slog"
	"net/http"
	"strings"
	"syscall"
	"testing"
)

func TestNetworkErrorPreservesCauseWithSafeEndpointDiagnostic(t *testing.T) {
	var log bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	calls := 0
	c := client(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, syscall.ECONNREFUSED
	})
	_, err := c.Status(context.Background())
	var failure *model.Error
	if !errors.As(err, &failure) || failure.Code != "NETWORK" || !errors.Is(err, syscall.ECONNREFUSED) || calls != 1 {
		t.Fatal("cause lost or network request retried", err, calls)
	}
	_, err = c.SendDynamic(context.Background(), "不应进入日志的正文")
	if !errors.As(err, &failure) || failure.Code != "SEND_UNKNOWN" || calls != 2 {
		t.Fatal("write retried or unknown state lost", err, calls)
	}
	text := log.String()
	if !strings.Contains(text, "path=/x/web-interface/nav") || !strings.Contains(text, "kind=connection-refused") || strings.Contains(text, "csrf-test") || strings.Contains(text, "不应进入日志的正文") || strings.Contains(text, "csrf=") {
		t.Fatal("unsafe or missing diagnostic")
	}
}

package httpinit

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

func (s *Server) requestShutdown() { s.stopOnce.Do(s.closeStopSignal) }
func (s *Server) closeStopSignal() { close(s.stopping) }
func (s *Server) runBackground(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	if s.Background != nil {
		s.Background(ctx)
	}
}
func (s *Server) serve(done chan<- error) { done <- s.HTTP.Serve(s.Listener) }

// Run 等待正常关闭通知，先取消自动任务，再收尾HTTP和业务账本；用户关闭正常退出，不触发崩溃重启。
func (s *Server) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	backgroundDone := make(chan struct{})
	serveDone := make(chan error, 1)
	go s.runBackground(runCtx, backgroundDone)
	go s.serve(serveDone)
	var serveErr error
	select {
	case serveErr = <-serveDone:
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
	case <-ctx.Done():
	case <-s.stopping:
	}
	cancel()
	cleanup, finish := context.WithTimeout(context.Background(), 25*time.Second)
	defer finish()
	if err := s.HTTP.Shutdown(cleanup); err != nil {
		slog.Warn("服务关闭等待超时，停止剩余HTTP连接", "error", err)
		if closeErr := s.HTTP.Close(); closeErr != nil {
			slog.Warn("关闭剩余HTTP连接失败", "error", closeErr)
		}
	}
	select {
	case <-backgroundDone:
	case <-cleanup.Done():
		slog.Warn("自动任务收尾超时，未完成发送将在下次启动标记未知，不自动重发")
	}
	return serveErr
}

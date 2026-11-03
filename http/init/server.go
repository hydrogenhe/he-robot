package httpinit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"gpt-robot/core"
	"gpt-robot/http/handler"
	"gpt-robot/model"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

type Config struct {
	Root    string
	Address string
}
type Server struct {
	stopping   chan struct{}
	stopOnce   sync.Once
	Background func(context.Context)
	HTTP       *http.Server
	Listener   net.Listener
	Origin     string
	OpenURL    string
}

type ServerInterface interface {
	Run(context.Context) error
}

var _ ServerInterface = (*Server)(nil)

func secret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Init先占用端口，再初始化资料；重复启动不会修改正在使用的运行账本。
func Init(ctx context.Context, cfg *Config, service core.ServiceInterface) (*Server, error) {
	if cfg == nil {
		return nil, model.Fail("CONFIG", "缺少服务配置")
	}
	host, _, err := net.SplitHostPort(cfg.Address)
	if err != nil || host != "127.0.0.1" {
		return nil, model.Fail("CONFIG", "工作台只允许绑定127.0.0.1")
	}
	managed := service == nil
	if service == nil {
		service = core.NewService(cfg.Root, nil)
	}
	listener, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			listener.Close()
		}
	}()
	if err = service.Initialize(ctx); err != nil {
		return nil, err
	}
	values := make([]string, 4)
	for i := range values {
		values[i], err = secret()
		if err != nil {
			return nil, err
		}
	}
	origin := "http://" + listener.Addr().String()
	store := core.NewStore(cfg.Root)
	values[1], err = persistentWebSession(ctx, store, origin, values[1])
	if err != nil {
		return nil, err
	}
	app := &Server{stopping: make(chan struct{})}
	opt := handler.WorkbenchOptions{Shutdown: app.requestShutdown, Origin: origin, Bootstrap: values[0], Session: values[1], CSRF: values[2], MCP: values[3]}
	var actions handler.ActionsInterface = handler.NewActions(service)
	background := service.RunAutomation
	if managed {
		primary := service.(*core.Service)
		manager, managerErr := core.NewRobotManager(ctx, cfg.Root, primary)
		if managerErr != nil {
			return nil, managerErr
		}
		actions = handler.NewRobotActions(manager, actions)
		background = manager.Run
	}

	workbench := handler.NewWorkbench(actions, &opt)
	router := RegisterRoutes(workbench, origin)
	httpServer := &http.Server{Handler: router, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32768}
	app.Background = background
	app.HTTP, app.Listener, app.Origin, app.OpenURL = httpServer, listener, origin, origin+"/"
	runtime := model.Runtime{Origin: origin, Token: values[3], PID: os.Getpid()}
	launch := model.Launch{URL: app.OpenURL}
	if err = store.Write(ctx, ".private/runtime.json", runtime); err != nil {
		return nil, err
	}
	if err = store.Write(ctx, ".private/ui-launch.json", launch); err != nil {
		return nil, err
	}
	ok = true
	return app, nil
}
func DefaultConfig(root string) *Config {
	cfg := &Config{Root: root, Address: fmt.Sprintf("127.0.0.1:%d", 3217)}
	return cfg
}

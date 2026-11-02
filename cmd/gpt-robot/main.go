package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"gpt-robot/app"
	"gpt-robot/businesshost"
	httpinit "gpt-robot/http/init"
	"gpt-robot/mcpbridge"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if err := run(); err != nil {
		slog.Error("GPT Robot停止", "error", err)
		os.Exit(1)
	}
}
func run() error {
	command := "serve"
	args := os.Args[1:]
	if len(args) > 0 && args[0][0] != '-' {
		command = args[0]
		args = args[1:]
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	rootFlag := flags.String("root", "", "角色项目目录")
	open := flags.Bool("open", false, "打开本地工作台")
	video := flags.String("video", "", "只读业务视频BV号")
	memory := flags.String("memory", "", "只读业务记忆关键词")
	address := flags.String("listen", "127.0.0.1:3217", "回环监听地址")
	if err := flags.Parse(args); err != nil {
		return err
	}
	root, err := app.Root(*rootFlag)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch command {
	case "read":
		input := businesshost.ReadInput{Root: root, Video: *video, MemoryQuery: *memory}
		result, err := app.ReadBusiness(ctx, &input)
		if result != nil {
			if encodeErr := json.NewEncoder(os.Stdout).Encode(result); encodeErr != nil {
				return encodeErr
			}
		}
		return err
	case "mcp":
		return mcpbridge.Run(ctx, root)
	case "config":
		text, err := app.MCPConfig(root)
		if err != nil {
			return err
		}
		fmt.Print(text)
		return nil
	case "open":
		return app.OpenExisting(ctx, root)
	case "serve":
	default:
		return fmt.Errorf("未知命令：%s", command)
	}
	if *open {
		if err = app.OpenExisting(ctx, root); err == nil {
			return nil
		}
	}
	cfg := httpinit.DefaultConfig(root)
	cfg.Address = *address
	server, err := httpinit.Init(ctx, cfg, nil)
	if err != nil {
		return err
	}
	fmt.Println("GPT Robot · Go本地工作台\n网页地址（可收藏，重启后不变）：\n" + server.OpenURL + "\n保持终端运行；Ctrl+C停止。")
	if *open {
		if err = app.Open(server.OpenURL); err != nil {
			slog.Warn("无法自动打开浏览器，请使用终端启动链接")
		}
	}
	return server.Run(ctx)
}

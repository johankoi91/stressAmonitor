package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"rtcstress/internal/agent"
	"rtcstress/internal/control"
	"rtcstress/internal/monitor"
	"rtcstress/internal/wire"
	"rtcstress/internal/worker"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run() error {
	mode := "serve"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	switch mode {
	case "version":
		fmt.Println("rtc-stress-console " + wire.Version)
		return nil
	case "worker":
		return worker.Serve(ctx, os.Stdin, os.Stdout)
	case "check":
		var req worker.Request
		if err := json.NewDecoder(io.LimitReader(os.Stdin, 8<<20)).Decode(&req); err != nil {
			return err
		}
		if err := worker.Check(req); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": true, "time": time.Now().UTC()})
	case "serve":
		args := []string{}
		if len(os.Args) > 2 {
			args = os.Args[2:]
		}
		return serve(ctx, args)
	case "agent":
		flags := flag.NewFlagSet("agent", flag.ContinueOnError)
		path := flags.String("config", "/etc/rtc-stress-agent.json", "节点配置文件")
		if err := flags.Parse(os.Args[2:]); err != nil {
			return err
		}
		options, err := agent.LoadOptions(*path)
		if err != nil {
			return err
		}
		stopNetdata := monitor.Supervise(ctx)
		defer stopNetdata()
		return agent.Run(ctx, options)
	default:
		return fmt.Errorf("未知命令 %s；使用 serve、agent、worker、check 或 version", mode)
	}
}

func serve(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := flags.String("listen", "127.0.0.1:8092", "HTTP 监听地址")
	dataDir := flags.String("data-dir", ".local", "私有配置与任务数据目录")
	frontend := flags.String("frontend", "../frontend/dist", "独立 React 构建目录")
	defaultsDir := flags.String("defaults-dir", "/opt/rtc-stress-console/defaults", "首次部署的默认配置与音源目录")
	nodeGrace := flags.Int("node-grace", 45, "节点断线重连宽限秒数")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := os.MkdirAll(*dataDir, 0700); err != nil {
		return err
	}
	hub := control.NewHub(nil, time.Duration(*nodeGrace)*time.Second)
	history, err := monitor.OpenStore(filepath.Join(*dataDir, "telemetry.json"))
	if err != nil {
		return err
	}
	hub.Telemetry = history
	defer func() {
		hub.Close()
		if err := history.Save(); err != nil {
			log.Printf("保存监控历史失败: %v", err)
		}
	}()
	stopNetdata := monitor.Supervise(ctx)
	defer stopNetdata()
	metricsCtx, stopMetrics := context.WithCancel(ctx)
	defer stopMetrics()
	go monitor.NewCollector("").Run(metricsCtx, func(sample monitor.Sample) { hub.Telemetry.Add("A", sample) })
	transport := &control.ClusterTransport{Hub: hub}
	manager, err := control.NewManager(*dataDir, transport, *defaultsDir)
	if err != nil {
		return err
	}
	defer manager.Close()
	handler := (&control.HTTP{Manager: manager, FrontendDir: *frontend, Hub: hub}).Handler()
	server := control.Server(handler, *listen)
	go func() {
		<-ctx.Done()
		manager.Close()
		wait, cancelWait := context.WithTimeout(context.Background(), time.Duration(*nodeGrace+15)*time.Second)
		_ = manager.Wait(wait)
		cancelWait()
		hub.Close()
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("RTC 压测控制台 http://%s", *listen)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

package monitor

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Supervise owns Netdata inside the same application container and restarts it if it exits.
func Supervise(parent context.Context) func() {
	ctx, cancel := context.WithCancel(parent)
	binary := os.Getenv("RTC_NETDATA_BINARY")
	if binary == "" {
		binary = "/opt/netdata/bin/netdata"
	}
	config := os.Getenv("RTC_NETDATA_CONFIG")
	if config == "" {
		config = "/opt/netdata/etc/netdata/netdata.conf"
	}
	if _, err := os.Stat(binary); err != nil {
		return cancel
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			cmd := exec.Command(binary, "-D", "-s", "/host", "-c", config)
			for _, item := range os.Environ() {
				if !strings.HasPrefix(item, "LD_LIBRARY_PATH=") && !strings.HasPrefix(item, "LD_PRELOAD=") {
					cmd.Env = append(cmd.Env, item)
				}
			}
			cmd.Env = append(cmd.Env, "DO_NOT_TRACK=1", "DISABLE_TELEMETRY=1")
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Stdout = os.Stderr
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				log.Printf("Netdata 启动失败: %v", err)
			} else {
				wait := make(chan error, 1)
				go func() { wait <- cmd.Wait() }()
				select {
				case err := <-wait:
					log.Printf("Netdata 退出，将自动重启: %v", err)
				case <-ctx.Done():
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
					select {
					case <-wait:
					case <-time.After(5 * time.Second):
						_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
						<-wait
					}
					return
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
		}
	}()
	return func() { cancel(); <-done }
}

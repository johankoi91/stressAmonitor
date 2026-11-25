package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"rtcstress/internal/scenario"
)

type Request struct {
	TaskID  string            `json:"taskId"`
	Config  scenario.Config   `json:"config"`
	Plan    scenario.HostPlan `json:"plan"`
	WorkDir string            `json:"workDir"`
	Binary  string            `json:"binary,omitempty"`
	Assets  []AudioAsset      `json:"assets,omitempty"`
}
type AudioAsset struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Extension string `json:"extension"`
}
type Command struct {
	Type    string    `json:"type"`
	At      time.Time `json:"at,omitempty"`
	DelayMS int       `json:"delayMs,omitempty"`
}
type Event struct {
	SDK      *SDKStats `json:"sdk,omitempty"`
	Type     string    `json:"type"`
	Host     string    `json:"host"`
	Process  string    `json:"process,omitempty"`
	Channel  string    `json:"channel,omitempty"`
	UID      uint64    `json:"uid,omitempty"`
	PID      int       `json:"pid,omitempty"`
	Message  string    `json:"message,omitempty"`
	At       time.Time `json:"at"`
	Source   *int      `json:"source,omitempty"`
	Bytes    uint64    `json:"bytes,omitempty"`
	TX       uint64    `json:"tx,omitempty"`
	RX       uint64    `json:"rx,omitempty"`
	Snapshot []Event   `json:"snapshot,omitempty"`
}

type emitter struct {
	mu      sync.Mutex
	encoder *json.Encoder
	host    string
}

func (e *emitter) send(v Event) {
	if v.Type == "log" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	v.Host = e.host
	if v.At.IsZero() {
		v.At = time.Now().UTC()
	}
	_ = e.encoder.Encode(v)
}

var connectedRE = regexp.MustCompile(`onConnected: id \d+, channelId ([^, ]+), localUserId (\d+), reason`)
var disconnectedRE = regexp.MustCompile(`onDisconnected: id \d+, channelId ([^, ]+), localUserId (\d+), reason`)
var trafficRE = regexp.MustCompile(`conn:\d+ audio\(tx,rx\): (\d+),(\d+)`)

func connectionEvent(line string) (Event, bool) {
	for _, item := range []struct {
		re   *regexp.Regexp
		kind string
	}{{connectedRE, "connected"}, {disconnectedRE, "disconnected"}} {
		if m := item.re.FindStringSubmatch(line); m != nil {
			uid, err := strconv.ParseUint(m[2], 10, 64)
			if err == nil {
				return Event{Type: item.kind, Channel: m[1], UID: uid}, true
			}
		}
	}
	return Event{}, false
}

func Check(req Request) error {
	if err := req.Config.Validate(true); err != nil {
		return err
	}
	if !filepath.IsAbs(req.WorkDir) {
		return errors.New("Worker 工作目录须为绝对路径")
	}
	binary := filepath.Join(req.WorkDir, "sample_send_h264_pcm")
	if req.Config.Engine == "go-sdk" {
		binary = sdkWorkerBinary()
	}
	if req.Binary != "" {
		binary = req.Binary
	}
	info, e := os.Stat(binary)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return errors.New("Native 二进制不可执行")
	}
	if req.Config.AudioInputMode == "opus" {
		for _, path := range req.Config.AudioFiles {
			audio, err := ReadOpus(path, req.WorkDir)
			if err != nil {
				return err
			}
			if audio.SampleRate != req.Config.SampleRate || audio.Channels != req.Config.AudioChannels {
				return errors.New("Opus audio format does not match task")
			}
		}
	}
	if req.Plan.HasPublishers() && req.Config.AudioInputMode != "opus" {
		a, e := ReadAudio(req.Config.AudioFiles[0], req.WorkDir, req.Config.SampleRate, req.Config.AudioChannels)
		if e != nil {
			return fmt.Errorf("第一路音频: %w", e)
		}
		b, e := ReadAudio(req.Config.AudioFiles[1], req.WorkDir, req.Config.SampleRate, req.Config.AudioChannels)
		if e != nil {
			return fmt.Errorf("第二路音频: %w", e)
		}
		if !DifferentAudio(a, b) {
			return errors.New("两路音频内容相同，请选择不同素材")
		}
	}
	return nil
}

// Serve owns one task control stream. Closing that stream cancels every child process.
func Serve(parent context.Context, in io.Reader, out io.Writer) error {
	decoder := json.NewDecoder(io.LimitReader(in, 8<<20))
	var req Request
	if e := decoder.Decode(&req); e != nil {
		return e
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`).MatchString(req.TaskID) {
		return errors.New("任务 ID 无效")
	}
	e := &emitter{encoder: json.NewEncoder(out), host: req.Plan.Host}
	if err := Check(req); err != nil {
		e.send(Event{Type: "error", Message: err.Error()})
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	control := make(chan Command, 4)
	go func() {
		defer close(control)
		for {
			var c Command
			if err := decoder.Decode(&c); err != nil {
				cancel()
				return
			}
			select {
			case control <- c:
			case <-ctx.Done():
				return
			}
		}
	}()
	return run(ctx, cancel, req, e, control)
}

func run(ctx context.Context, cancel context.CancelFunc, req Request, e *emitter, control <-chan Command) error {
	if req.Config.Engine == "go-sdk" {
		return runSDKProcesses(ctx, cancel, req, e, control)
	}
	dir := filepath.Join(req.WorkDir, "console-runs", req.TaskID, req.Plan.Host)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	var sources [2][]byte
	if req.Plan.HasPublishers() {
		for i := 0; i < 2; i++ {
			data, err := ReadAudio(req.Config.AudioFiles[i], req.WorkDir, req.Config.SampleRate, req.Config.AudioChannels)
			if err != nil {
				return err
			}
			sources[i] = data
		}
	}
	var epoch atomic.Int64
	var processWG, feedWG sync.WaitGroup
	errorsCh := make(chan error, 1)
	fail := func(err error) {
		select {
		case errorsCh <- err:
		default:
		}
		cancel()
	}
	started := false
	defer func() {
		cancel()
		processWG.Wait()
		feedWG.Wait()
		e.send(Event{Type: "stopped", Message: "本机任务进程已退出"})
	}()
	for _, plan := range req.Plan.Processes {
		if ctx.Err() != nil {
			break
		}
		pipes := map[string]string{}
		for _, connection := range plan.Connections {
			if connection.AudioSource < 0 {
				continue
			}
			path := filepath.Join(dir, fmt.Sprintf("%s-%d.pcm.fifo", connection.Channel, connection.UID))
			if err := makeFIFO(path); err != nil {
				return err
			}
			pipes[scenario.Key(connection.Channel, connection.UID)] = path
			feedWG.Add(1)
			go func(u scenario.Connection, path string) {
				defer feedWG.Done()
				if err := feedPCM(ctx, path, sources[u.AudioSource], req.Config, u.AudioSource, &epoch, func(n uint64) {
					e.send(Event{Type: "audio", Channel: u.Channel, UID: u.UID, Source: &u.AudioSource, Bytes: n})
				}, nil); err != nil && ctx.Err() == nil {
					fail(fmt.Errorf("音频管道 %s/%d: %w", u.Channel, u.UID, err))
				}
			}(connection, path)
		}
		binary := filepath.Join(req.WorkDir, "sample_send_h264_pcm")
		if req.Binary != "" {
			binary = req.Binary
		}
		cmd := exec.Command(binary, scenario.NativeArgs(req.Config, plan, pipes)...)
		cmd.Dir = req.WorkDir
		cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+req.WorkDir+":"+os.Getenv("LD_LIBRARY_PATH"))
		prepareProcess(cmd)
		reader, writer := io.Pipe()
		cmd.Stdout = writer
		cmd.Stderr = writer
		if err := cmd.Start(); err != nil {
			_ = reader.Close()
			_ = writer.Close()
			return err
		}
		started = true
		done := make(chan struct{})
		ready := make(chan struct{})
		processWG.Add(1)
		e.send(Event{Type: "process_started", Process: plan.ID, PID: cmd.Process.Pid})
		go func(plan scenario.Process) {
			defer processWG.Done()
			scanDone := make(chan struct{})
			go func() {
				defer close(scanDone)
				defer reader.Close()
				joined := make(map[string]bool, len(plan.Connections))
				for _, connection := range plan.Connections {
					joined[scenario.Key(connection.Channel, connection.UID)] = false
				}
				connectedCount := 0
				readySent := false
				scan := bufio.NewScanner(reader)
				scan.Buffer(make([]byte, 4096), 1<<20)
				for scan.Scan() {
					line := scan.Text()
					if v, ok := connectionEvent(line); ok {
						v.Process = plan.ID
						e.send(v)
						key := scenario.Key(v.Channel, v.UID)
						if previous, expected := joined[key]; expected {
							connected := v.Type == "connected"
							if connected && !previous {
								connectedCount++
							}
							if !connected && previous {
								connectedCount--
							}
							joined[key] = connected
							if connectedCount == len(joined) && !readySent {
								close(ready)
								readySent = true
							}
						}
					}
					if match := trafficRE.FindStringSubmatch(line); match != nil {
						tx, _ := strconv.ParseUint(match[1], 10, 64)
						rx, _ := strconv.ParseUint(match[2], 10, 64)
						e.send(Event{Type: "traffic", Process: plan.ID, TX: tx, RX: rx})
					}
					if strings.Contains(line, "APP_LOG_FATAL") || strings.Contains(line, "Failed to open audio file") {
						fail(errors.New(strings.ReplaceAll(line, req.Config.Token, "[凭据已隐藏]")))
					}
				}
				if err := scan.Err(); err != nil && ctx.Err() == nil {
					fail(err)
				}
			}()
			go func() {
				select {
				case <-ctx.Done():
					stopGroup(cmd, done)
				case <-done:
				}
			}()
			err := cmd.Wait()
			close(done)
			_ = writer.Close()
			<-scanDone
			e.send(Event{Type: "process_exited", Process: plan.ID, PID: cmd.Process.Pid})
			if ctx.Err() == nil {
				fail(fmt.Errorf("%s 提前退出: %v", plan.ID, err))
			}
		}(plan)
		// A Native process can create hundreds of SDK threads while joining.
		// Wait for its actual connection callbacks before initializing the next
		// process, rather than overlapping every heavy SDK initialization.
		if err := waitProcessReady(ctx, cancel, ready, errorsCh, control, time.Duration(req.Config.ReadyTimeoutSec)*time.Second); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			err = fmt.Errorf("%s 入频道阶段: %w", plan.ID, err)
			e.send(Event{Type: "error", Process: plan.ID, Message: err.Error()})
			return err
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Duration(req.Config.LaunchIntervalMS) * time.Millisecond):
		}
	}
	if !started && ctx.Err() == nil {
		return errors.New("进程计划为空")
	}
	e.send(Event{Type: "launched", Message: "所有本机进程已创建，等待真实入频道回调"})
	for {
		select {
		case c, ok := <-control:
			if !ok {
				return nil
			}
			switch c.Type {
			case "stop":
				cancel()
				return nil
			case "begin":
				if c.At.IsZero() && c.DelayMS >= 0 && c.DelayMS <= 60000 {
					c.At = time.Now().UTC().Add(time.Duration(c.DelayMS) * time.Millisecond)
				}
				if c.At.IsZero() || c.At.Before(time.Now().Add(-time.Second)) {
					return errors.New("音频起始时间无效")
				}
				if !epoch.CompareAndSwap(0, c.At.UnixNano()) {
					return errors.New("本任务音频已经启动")
				}
				e.send(Event{Type: "armed", At: c.At, Message: "音频发送时间已设置"})
			}
		case err := <-errorsCh:
			e.send(Event{Type: "error", Message: err.Error()})
			return err
		case <-ctx.Done():
			select {
			case err := <-errorsCh:
				e.send(Event{Type: "error", Message: err.Error()})
				return err
			default:
				return nil
			}
		}
	}
}

// Startup must consume stop commands even while waiting for a Native callback.
func waitProcessReady(ctx context.Context, cancel context.CancelFunc, ready <-chan struct{}, failures <-chan error, control <-chan Command, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ready:
		return nil
	case err := <-failures:
		return err
	case <-ctx.Done():
		select {
		case err := <-failures:
			return err
		default:
			return ctx.Err()
		}
	case command, ok := <-control:
		if !ok || command.Type == "stop" {
			cancel()
			return context.Canceled
		}
		return fmt.Errorf("入频道完成前收到不支持的命令 %s", command.Type)
	case <-timer.C:
		return errors.New("等待本进程 SDK 入频道回调超时")
	}
}

package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"rtcstress/internal/scenario"
	"sync"
	"syscall"
	"time"
)

type SDKStats struct {
	AudioInputMode    string `json:"audioInputMode"`
	EncodingEnabled   bool   `json:"encodingEnabled"`
	EncodedSent       uint64 `json:"encodedSent"`
	AudioFramesSent   uint64 `json:"audioFramesSent"`
	AudioSentMicros   uint64 `json:"audioSentMicros"`
	PayloadMatched    uint64 `json:"payloadMatched"`
	PayloadMismatched uint64 `json:"payloadMismatched"`

	EncodedCodecTypes     []int    `json:"encodedCodecTypes,omitempty"`
	AudioScenario         string   `json:"audioScenario"`
	OpusPacketUSMin       int      `json:"opusPacketUsMin"`
	OpusPacketUSMax       int      `json:"opusPacketUsMax"`
	OpusMonoConnections   int      `json:"opusMonoConnections"`
	OpusStereoConnections int      `json:"opusStereoConnections"`
	PID                   int      `json:"pid"`
	RSSBytes              uint64   `json:"rssBytes"`
	GoHeapBytes           uint64   `json:"goHeapBytes"`
	Threads               int      `json:"threads"`
	Version               string   `json:"version"`
	Layout                string   `json:"layout"`
	Services              int      `json:"services"`
	Connections           int      `json:"connections"`
	ServiceIDs            []string `json:"serviceIds,omitempty"`
	PCMSent               uint64   `json:"pcmSent"`
	EncodedReceived       uint64   `json:"encodedReceived"`
	Packets               uint64   `json:"packets"`
	ActiveStreams         int      `json:"activeStreams"`
	ReceivedStreams       int      `json:"receivedStreams"`
	ExpectedStreams       int      `json:"expectedStreams"`
	SendErrors            uint64   `json:"sendErrors"`
	SendBitrateKbps       int      `json:"sendBitrateKbps"`
	ReceiveBitrateKbps    int      `json:"receiveBitrateKbps"`
	DecodeEnabled         bool     `json:"decodeEnabled"`
}

func sdkWorkerBinary() string {
	if path := os.Getenv("RTC_SDK_WORKER_BINARY"); path != "" {
		return path
	}
	return "/opt/rtc-stress-console/rtc-sdk-worker"
}

type sdkChild struct {
	mu    sync.Mutex
	input io.WriteCloser
	cmd   *exec.Cmd
	done  chan struct{}
}

func (c *sdkChild) send(command Command) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return json.NewEncoder(c.input).Encode(command)
}
func (c *sdkChild) stop() {
	_ = c.send(Command{Type: "stop"})
	select {
	case <-c.done:
		return
	case <-time.After(6 * time.Second):
	}
	_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGINT)
	select {
	case <-c.done:
		return
	case <-time.After(5 * time.Second):
	}
	_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
	<-c.done
}

func runSDKProcesses(ctx context.Context, cancel context.CancelFunc, request Request, events *emitter, control <-chan Command) error {
	dir := filepath.Join(request.WorkDir, "console-runs", request.TaskID, request.Plan.Host)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	failures := make(chan error, 1)
	fail := func(err error) {
		select {
		case failures <- err:
		default:
		}
		cancel()
	}
	children := make([]*sdkChild, 0, len(request.Plan.Processes))
	defer func() {
		cancel()
		var group sync.WaitGroup
		for _, child := range children {
			group.Add(1)
			go func(child *sdkChild) { defer group.Done(); child.stop() }(child)
		}
		group.Wait()
		events.send(Event{Type: "stopped", Message: "Go SDK 压测进程已退出"})
	}()
	for _, process := range request.Plan.Processes {
		if ctx.Err() != nil {
			break
		}
		eventRead, eventWrite, err := os.Pipe()
		if err != nil {
			return err
		}
		binary := sdkWorkerBinary()
		if request.Binary != "" {
			binary = request.Binary
		}
		cmd := exec.Command(binary)
		cmd.Dir = request.WorkDir
		cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH=/opt/rtc-stress-console/sdk", "RTC_TASK_ID="+request.TaskID)
		cmd.ExtraFiles = []*os.File{eventWrite}
		// Nil stdout/stderr send unstructured SDK output directly to the null device.
		prepareProcess(cmd)
		input, err := cmd.StdinPipe()
		if err != nil {
			eventRead.Close()
			eventWrite.Close()
			return err
		}
		child := &sdkChild{input: input, cmd: cmd, done: make(chan struct{})}
		if err := cmd.Start(); err != nil {
			input.Close()
			eventRead.Close()
			eventWrite.Close()
			return err
		}
		eventWrite.Close()
		children = append(children, child)
		events.send(Event{Type: "process_started", Process: process.ID, PID: cmd.Process.Pid})
		ready := make(chan struct{})
		decoded := make(chan struct{})
		go func() {
			defer close(decoded)
			defer eventRead.Close()
			expected := map[string]bool{}
			for _, connection := range process.Connections {
				expected[connection.Channel+"/"+fmt.Sprint(connection.UID)] = false
			}
			count := 0
			sent := false
			decoder := json.NewDecoder(eventRead)
			for {
				var event Event
				if err := decoder.Decode(&event); err != nil {
					return
				}
				event.Process = process.ID
				events.send(event)
				if event.Type == "error" && ctx.Err() == nil {
					fail(errors.New(process.ID + ": " + event.Message))
				}
				if event.Type == "connected" {
					key := event.Channel + "/" + fmt.Sprint(event.UID)
					if previous, ok := expected[key]; ok && !previous {
						expected[key] = true
						count++
					}
					if count == len(expected) && !sent {
						close(ready)
						sent = true
					}
				}
			}
		}()
		go func() {
			err := cmd.Wait()
			_ = input.Close()
			select {
			case <-decoded:
			case <-time.After(2 * time.Second):
				_ = eventRead.Close()
				<-decoded
			}
			close(child.done)
			events.send(Event{Type: "process_exited", Process: process.ID, PID: cmd.Process.Pid})
			if ctx.Err() == nil {
				fail(fmt.Errorf("%s Go SDK进程提前退出: %v", process.ID, err))
			}
		}()
		childRequest := request
		childRequest.Plan.Processes = []scenario.Process{process}
		child.mu.Lock()
		err = json.NewEncoder(input).Encode(childRequest)
		child.mu.Unlock()
		if err != nil {
			return err
		}
		if err := waitProcessReady(ctx, cancel, ready, failures, control, time.Duration(request.Config.ReadyTimeoutSec)*time.Second); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("%s: %w", process.ID, err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Duration(request.Config.LaunchIntervalMS) * time.Millisecond):
		}
	}
	events.send(Event{Type: "launched", Message: "Go SDK 进程已创建"})
	for {
		select {
		case <-ctx.Done():
			select {
			case err := <-failures:
				return err
			default:
				return nil
			}
		case err := <-failures:
			return err
		case command, ok := <-control:
			if !ok || command.Type == "stop" {
				return nil
			}
			if command.Type == "begin" {
				if command.At.IsZero() {
					command.At = time.Now().UTC().Add(time.Duration(command.DelayMS) * time.Millisecond)
					command.DelayMS = 0
				}
				for _, child := range children {
					if err := child.send(command); err != nil {
						return err
					}
				}
			}
		}
	}
}

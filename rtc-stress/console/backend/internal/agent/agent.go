package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"rtcstress/internal/monitor"
	"rtcstress/internal/scenario"
	"rtcstress/internal/wire"
	"rtcstress/internal/worker"
)

type Options struct {
	Node   string `json:"node"`
	Center string `json:"center"`

	WorkDir      string                                                                              `json:"workDir"`
	GraceSeconds int                                                                                 `json:"graceSeconds"`
	Check        func(worker.Request) error                                                          `json:"-"`
	Start        func(context.Context, worker.Request, func(worker.Event)) (wire.TaskSession, error) `json:"-"`
}
type execution struct {
	request worker.Request
	state   *wire.State
	session wire.TaskSession
}
type Agent struct {
	telemetry *monitor.Sample
	mu        sync.Mutex
	options   Options
	bootID    string
	peer      *wire.Peer
	active    *execution
	last      *wire.State
	recent    map[string]wire.Snapshot
	order     []string
	out       chan wire.Frame
	ctx       context.Context
	grace     *time.Timer
}

func Run(ctx context.Context, options Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if !scenario.ValidID(options.Node) || options.Node == "A" {
		return errors.New("Agent 须使用唯一的节点标识，A 保留给中心")
	}
	u, err := url.Parse(options.Center)
	if err != nil || u.Scheme != "ws" || u.Host == "" {
		return errors.New("中心地址须为 ws://IP:端口/ws/agent")
	}
	if options.WorkDir == "" {
		options.WorkDir = "/home/bbt"
	}
	if options.GraceSeconds <= 0 {
		options.GraceSeconds = 45
	}
	var b [16]byte
	if _, err = rand.Read(b[:]); err != nil {
		return err
	}
	if options.Check == nil {
		options.Check = worker.Check
	}
	if options.Start == nil {
		options.Start = func(ctx context.Context, r worker.Request, f func(worker.Event)) (wire.TaskSession, error) {
			return wire.StartExecution(ctx, r, f)
		}
	}
	a := &Agent{options: options, bootID: hex.EncodeToString(b[:]), recent: map[string]wire.Snapshot{}, out: make(chan wire.Frame, 512), ctx: ctx, last: wire.NewState("")}
	go monitor.NewCollector("").Run(ctx, func(sample monitor.Sample) { a.mu.Lock(); a.telemetry = &sample; a.mu.Unlock() })
	defer func() {
		a.mu.Lock()
		if a.grace != nil {
			a.grace.Stop()
		}
		peer := a.peer
		active := a.active
		a.mu.Unlock()
		if peer != nil {
			_ = peer.Conn.Close()
		}
		if active != nil && active.session != nil {
			_ = active.session.Stop()
		}
	}()
	backoff := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err = a.connect(ctx)
		if ctx.Err() != nil {
			return nil
		}
		log.Printf("节点 %s 与中心连接中断，将自动重连: %v", options.Node, err)
		a.armGrace()
		if time.Since(started) > 10*time.Second {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff < 8*time.Second {
			backoff *= 2
		}
	}
	return nil
}

func (a *Agent) connect(ctx context.Context) error {
	u, _ := url.Parse(a.options.Center)
	q := u.Query()
	q.Set("node", a.options.Node)
	u.RawQuery = q.Encode()
	conn, _, err := (&websocket.Dialer{HandshakeTimeout: 10 * time.Second}).DialContext(ctx, u.String(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	peer := &wire.Peer{Conn: conn}
	conn.SetReadLimit(8 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(30 * time.Second)) })
	conn.SetPingHandler(func(value string) error {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		return conn.WriteControl(websocket.PongMessage, []byte(value), time.Now().Add(5*time.Second))
	})
	a.mu.Lock()
	snapshot := a.snapshotLocked()
	telemetry := a.telemetry
	a.peer = peer
	if a.grace != nil {
		a.grace.Stop()
		a.grace = nil
	}
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		if a.peer == peer {
			a.peer = nil
		}
		a.mu.Unlock()
	}()
	if err = peer.Send(wire.Frame{Type: "hello", Node: a.options.Node, BootID: a.bootID, Version: wire.Version, WorkDir: a.options.WorkDir, Snapshot: &snapshot, Telemetry: telemetry}); err != nil {
		return err
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		metricsTicker := time.NewTicker(time.Second)
		defer metricsTicker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				_ = conn.Close()
				return
			case <-metricsTicker.C:
				a.mu.Lock()
				sample := a.telemetry
				a.mu.Unlock()
				if sample != nil {
					if peer.Send(wire.Frame{Type: "telemetry", Telemetry: sample}) != nil {
						_ = conn.Close()
						return
					}
				}
			case frame := <-a.out:
				if peer.Send(frame) != nil {
					_ = conn.Close()
					return
				}
			case <-ticker.C:
				a.mu.Lock()
				snapshot := a.snapshotLocked()
				activeID := ""
				if a.active != nil {
					activeID = a.active.request.TaskID
				}
				a.mu.Unlock()
				if peer.Send(wire.Frame{Type: "heartbeat", Node: a.options.Node, TaskID: activeID}) != nil {
					_ = conn.Close()
					return
				}
				if snapshot.TaskID != "" {
					if peer.Send(wire.Frame{Type: "snapshot", TaskID: snapshot.TaskID, Snapshot: &snapshot}) != nil {
						_ = conn.Close()
						return
					}
				}
			}
		}
	}()
	for {
		var frame wire.Frame
		if err = conn.ReadJSON(&frame); err != nil {
			return err
		}
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		a.handle(frame)
	}
}

func (a *Agent) snapshotLocked() wire.Snapshot {
	if a.active != nil {
		return a.active.state.Snapshot(a.options.Node)
	}
	return a.last.Snapshot(a.options.Node)
}
func (a *Agent) publish(frame wire.Frame) {
	if frame.Event != nil && frame.Event.Type == "log" {
		return
	}
	frame.Node = a.options.Node
	select {
	case a.out <- frame:
	default: /* periodic snapshots repair any dropped state events */
	}
}

func (a *Agent) handle(frame wire.Frame) {
	switch frame.Type {
	case "check":
		result := wire.Frame{Type: "result", RequestID: frame.RequestID}
		if frame.Request == nil {
			result.Error = "检查请求为空"
		} else {
			request := *frame.Request
			request.WorkDir = a.options.WorkDir
			request.Binary = ""
			if request.Plan.TargetNode() != a.options.Node {
				result.Error = "检查节点不匹配"
			} else if prepared, err := a.prepareAudio(request); err != nil {
				result.Error = err.Error()
			} else if err = a.options.Check(prepared); err != nil {
				result.Error = err.Error()
			}
		}
		a.publish(result)
	case "dispatch":
		a.dispatch(frame)
	case "stop", "command":
		a.mu.Lock()
		active := a.active
		var session wire.TaskSession
		if active != nil {
			session = active.session
		}
		old, completed := a.recent[frame.TaskID]
		a.mu.Unlock()
		if active == nil || active.request.TaskID != frame.TaskID {
			if completed {
				a.publish(wire.Frame{Type: "snapshot", TaskID: frame.TaskID, Snapshot: &old})
			}
			return
		}
		if frame.Type == "stop" {
			go func() {
				if session != nil {
					_ = session.Stop()
				}
			}()
		} else if frame.Command != nil && session != nil {
			if err := session.Send(*frame.Command); err != nil {
				a.publish(wire.Frame{Type: "event", TaskID: frame.TaskID, Event: &worker.Event{Type: "error", Host: a.options.Node, At: time.Now().UTC(), Message: err.Error()}})
			}
		}
	case "sync":
		a.mu.Lock()
		snapshot := a.snapshotLocked()
		a.mu.Unlock()
		a.publish(wire.Frame{Type: "snapshot", TaskID: snapshot.TaskID, Snapshot: &snapshot})
	}
}

func (a *Agent) dispatch(frame wire.Frame) {
	if frame.Request == nil || frame.TaskID == "" || frame.Request.TaskID != frame.TaskID || frame.Request.Plan.TargetNode() != a.options.Node {
		a.publish(wire.Frame{Type: "finished", TaskID: frame.TaskID, Error: "任务身份或节点不匹配"})
		return
	}
	a.mu.Lock()
	if previous, ok := a.recent[frame.TaskID]; ok {
		a.mu.Unlock()
		a.publish(wire.Frame{Type: "snapshot", TaskID: frame.TaskID, Snapshot: &previous})
		return
	}
	if a.active != nil {
		if a.active.request.TaskID == frame.TaskID {
			snapshot := a.snapshotLocked()
			a.mu.Unlock()
			a.publish(wire.Frame{Type: "snapshot", TaskID: frame.TaskID, Snapshot: &snapshot})
			return
		}
		a.mu.Unlock()
		a.publish(wire.Frame{Type: "finished", TaskID: frame.TaskID, Error: "节点已有其他任务"})
		return
	}
	request := *frame.Request
	run := &execution{request: request, state: wire.NewState(request.TaskID)}
	a.active = run
	a.mu.Unlock()
	prepared, prepareErr := a.prepareAudio(request)
	if prepareErr != nil {
		a.finished(run, prepareErr)
		return
	}
	request = prepared
	a.mu.Lock()
	run.request = request
	a.mu.Unlock()
	session, err := a.options.Start(a.ctx, request, func(event worker.Event) {
		if event.Type == "log" {
			return
		}
		event.Host = request.Plan.Host
		a.mu.Lock()
		run.state.Apply(event)
		sequence := run.state.Sequence
		a.mu.Unlock()
		a.publish(wire.Frame{Type: "event", TaskID: request.TaskID, Sequence: sequence, Event: &event})
	})
	if err != nil {
		a.finished(run, err)
		return
	}
	a.mu.Lock()
	run.session = session
	a.mu.Unlock()
	go func() { err := <-session.Done(); a.finished(run, err) }()
}

func (a *Agent) finished(run *execution, err error) {
	a.mu.Lock()
	run.state.Finished = true
	if err != nil {
		run.state.Error = err.Error()
	}
	snapshot := run.state.Snapshot(a.options.Node)
	a.last = run.state
	a.recent[run.request.TaskID] = snapshot
	a.order = append(a.order, run.request.TaskID)
	if len(a.order) > 16 {
		delete(a.recent, a.order[0])
		a.order = a.order[1:]
	}
	if a.active == run {
		a.active = nil
	}
	a.mu.Unlock()
	a.publish(wire.Frame{Type: "snapshot", TaskID: run.request.TaskID, Snapshot: &snapshot})
	a.publish(wire.Frame{Type: "finished", TaskID: run.request.TaskID, Error: snapshot.Error})
}

func (a *Agent) armGrace() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.grace != nil {
		return
	}
	a.grace = time.AfterFunc(time.Duration(a.options.GraceSeconds)*time.Second, func() {
		a.mu.Lock()
		active := a.active
		var session wire.TaskSession
		if active != nil {
			session = active.session
		}
		offline := a.peer == nil
		a.mu.Unlock()
		if offline && session != nil {
			log.Printf("节点 %s 重连超时，停止本机任务", a.options.Node)
			_ = session.Stop()
		}
	})
}

func LoadOptions(path string) (Options, error) {
	var options Options
	data, err := os.ReadFile(path)
	if err != nil {
		return options, err
	}
	if err = json.Unmarshal(data, &options); err != nil {
		return options, fmt.Errorf("节点配置无效: %w", err)
	}
	return options, nil
}

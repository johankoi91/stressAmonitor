package control

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"rtcstress/internal/agent"
	"rtcstress/internal/scenario"
	"rtcstress/internal/wire"
	"rtcstress/internal/worker"
)

type fakeNodeRun struct {
	done chan error
	once sync.Once
	stop func()
}

func (f *fakeNodeRun) Done() <-chan error        { return f.done }
func (f *fakeNodeRun) Send(worker.Command) error { return nil }
func (f *fakeNodeRun) Stop() error {
	f.once.Do(func() {
		if f.stop != nil {
			f.stop()
		}
		f.done <- nil
		close(f.done)
	})
	return nil
}
func eventually(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition timed out")
}

func TestWebSocketReconnectSynchronizesWithoutRestart(t *testing.T) {
	hub := NewHub([]string{"B", "C"}, 5*time.Second)
	defer hub.Close()
	server := httptest.NewServer(hub)
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/agent"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	agentDone := make(chan error, 1)
	var starts atomic.Int32
	go func() {
		agentDone <- agent.Run(ctx, agent.Options{Node: "B", Center: url, WorkDir: "/tmp", GraceSeconds: 5, Check: func(worker.Request) error { return nil }, Start: func(ctx context.Context, req worker.Request, emit func(worker.Event)) (wire.TaskSession, error) {
			starts.Add(1)
			emit(worker.Event{Type: "log", Message: "discard this legacy worker log"})
			fake := &fakeNodeRun{done: make(chan error, 1)}
			for _, p := range req.Plan.Processes {
				emit(worker.Event{Type: "process_started", Process: p.ID, PID: 12345, At: time.Now().UTC()})
				for _, c := range p.Connections {
					emit(worker.Event{Type: "connected", Process: p.ID, Channel: c.Channel, UID: c.UID, At: time.Now().UTC()})
				}
			}
			emit(worker.Event{Type: "traffic", RX: 321, At: time.Now().UTC()})
			fake.stop = func() {
				for _, p := range req.Plan.Processes {
					emit(worker.Event{Type: "process_exited", Process: p.ID, PID: 12345, At: time.Now().UTC()})
				}
			}
			return fake, nil
		}})
	}()
	eventually(t, func() bool { return hub.Ready("B") })
	config := scenario.Defaults()
	config.Channels = 3
	plan, _ := scenario.Build(config)
	request := worker.Request{TaskID: "ws-task-1", Config: config, Plan: plan.Hosts[1], WorkDir: "/tmp"}
	if err := hub.Check(ctx, "B", request); err != nil {
		t.Fatal(err)
	}
	events := make(chan worker.Event, 100)
	var uploadedLogs atomic.Int32
	session, err := hub.Start("B", request, func(e worker.Event) {
		if e.Type == "log" {
			uploadedLogs.Add(1)
		}
		events <- e
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := 0
	for joined < 3 {
		select {
		case e := <-events:
			if e.Type == "connected" {
				joined++
			}
		case <-time.After(5 * time.Second):
			t.Fatal("missing connection event")
		}
	}
	// A duplicate dispatch must never start a second Native execution.
	if err = hub.send("B", wire.Frame{Type: "dispatch", TaskID: request.TaskID, Request: &request}); err != nil {
		t.Fatal(err)
	}
	cluster := &ClusterTransport{Hub: hub}
	if err = cluster.Disconnect("B"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { nodes := hub.Nodes(); return nodes[1].Online && nodes[1].Reconnects >= 1 })
	replayed := false
	deadline := time.After(5 * time.Second)
	for !replayed {
		select {
		case e := <-events:
			if e.Type == "snapshot" {
				var hasPID, hasTraffic bool
				for _, v := range e.Snapshot {
					hasPID = hasPID || (v.Type == "process_started" && v.PID == 12345)
					hasTraffic = hasTraffic || (v.Type == "traffic" && v.RX == 321)
				}
				replayed = hasPID && hasTraffic
			}
		case <-deadline:
			t.Fatal("missing authoritative snapshot")
		}
	}
	if starts.Load() != 1 {
		t.Fatalf("task restarted on reconnect: %d", starts.Load())
	}
	if uploadedLogs.Load() != 0 {
		t.Fatal("runtime logs were uploaded over WebSocket")
	}
	// Stop issued while the socket is down must be delivered again after reconnect.
	if err = cluster.Disconnect("B"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return !hub.Ready("B") })
	stopped := make(chan error, 1)
	go func() { stopped <- session.Stop() }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("offline stop was not delivered after reconnect")
	}
	if starts.Load() != 1 {
		t.Fatal("stop recovery restarted the task")
	}
	eventually(t, func() bool { return hub.Nodes()[1].ActiveTaskID == "" })
	// Replaying an already finished task is also idempotent.
	_ = hub.send("B", wire.Frame{Type: "dispatch", TaskID: request.TaskID, Request: &request})
	time.Sleep(50 * time.Millisecond)
	if starts.Load() != 1 {
		t.Fatal("finished task ran again")
	}
	cancel()
	select {
	case err := <-agentDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("agent did not exit")
	}
}

func TestNodeIDValidationAndDisconnectDeadline(t *testing.T) {
	hub := NewHub([]string{"B"}, 100*time.Millisecond)
	defer hub.Close()
	server := httptest.NewServer(hub)
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/?node=B"
	for _, id := range []string{"A", "all", "bad/id", ""} {
		conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/?node="+id, nil)
		if err == nil {
			conn.Close()
			t.Fatalf("reserved/invalid node accepted: %q", id)
		}
		if response == nil || response.StatusCode != http.StatusBadRequest {
			t.Fatalf("wrong validation response for %q", id)
		}
	}
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = conn.WriteJSON(wire.Frame{Type: "hello", Node: "B", BootID: "boot-1", Snapshot: &wire.Snapshot{}}); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return hub.Ready("B") })
	c := scenario.Defaults()
	c.Channels = 1
	p, _ := scenario.Build(c)
	session, err := hub.Start("B", worker.Request{TaskID: "deadline-test", Config: c, Plan: p.Hosts[1]}, func(worker.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	select {
	case err := <-session.Done():
		if err == nil || !strings.Contains(err.Error(), "未重连") {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("offline session not terminated")
	}
}

func TestSnapshotDoesNotDoubleCountOrReviveExitedProcess(t *testing.T) {
	m := &Manager{}
	r := &runState{task: Task{State: "running", Plan: scenario.Plan{Hosts: []scenario.HostPlan{{Host: "B", Processes: []scenario.Process{{ID: "B-001", Connections: []scenario.Connection{{Channel: "x", UID: 1}}}}}}}}, expected: map[string]bool{"B/x/1": true}, connected: map[string]bool{}, joined: map[string]bool{}, processes: map[string]ProcessState{"B-001": {ID: "B-001"}}, audio: map[string]uint64{}, tx: map[string]uint64{}, rx: map[string]uint64{}, firstAudio: map[string]time.Time{}, notify: make(chan struct{}, 1), failure: make(chan error, 1)}
	snapshot := worker.Event{Type: "snapshot", Host: "B", Snapshot: []worker.Event{{Type: "connected", Host: "B", Channel: "x", UID: 1}, {Type: "process_exited", Host: "B", Process: "B-001"}, {Type: "traffic", Host: "B", RX: 999}}}
	m.event(r, snapshot)
	m.event(r, snapshot)
	if r.rx["B"] != 999 || r.connected["B/x/1"] || !r.joined["B/x/1"] {
		t.Fatal("snapshot double counted traffic or revived a stopped UID")
	}
}

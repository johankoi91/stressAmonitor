package control

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"rtcstress/internal/monitor"
	"rtcstress/internal/scenario"
	"rtcstress/internal/wire"
	"rtcstress/internal/worker"
	"sort"
)

type NodeState struct {
	ID           string     `json:"id"`
	Online       bool       `json:"online"`
	Mode         string     `json:"mode"`
	Version      string     `json:"version"`
	BootID       string     `json:"bootId,omitempty"`
	ConnectedAt  *time.Time `json:"connectedAt,omitempty"`
	LastSeenAt   *time.Time `json:"lastSeenAt,omitempty"`
	ActiveTaskID string     `json:"activeTaskId,omitempty"`
	Reconnects   int        `json:"reconnects"`
	LastError    string     `json:"lastError,omitempty"`
	Address      string     `json:"address"`
	WorkDir      string     `json:"workDir"`
}
type nodeLink struct {
	state      NodeState
	peer       *wire.Peer
	generation uint64
}
type rpcWait struct {
	node   string
	result chan wire.Frame
}
type Hub struct {
	Telemetry *monitor.Store
	mu        sync.Mutex
	nodes     map[string]*nodeLink
	sessions  map[string]*remoteSession
	pending   map[string]rpcWait
	ids       atomic.Uint64
	grace     time.Duration
	closed    bool
}

func NewHub(knownNodes []string, grace time.Duration) *Hub {
	if grace <= 0 {
		grace = 45 * time.Second
	}
	nodes := map[string]*nodeLink{}
	for _, id := range knownNodes {
		if scenario.ValidID(id) && id != "A" {
			nodes[id] = &nodeLink{state: NodeState{ID: id, Mode: "websocket"}}
		}
	}
	return &Hub{Telemetry: monitor.NewStore(), nodes: nodes, sessions: map[string]*remoteSession{}, pending: map[string]rpcWait{}, grace: grace}
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	node := r.URL.Query().Get("node")
	if !scenario.ValidID(node) || node == "A" {
		http.Error(w, "节点标识无效，A 保留给中心", http.StatusBadRequest)
		return
	}
	upgrade := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "" }, HandshakeTimeout: 10 * time.Second}
	conn, err := upgrade.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	peer := &wire.Peer{Conn: conn}
	defer conn.Close()
	conn.SetReadLimit(8 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	var hello wire.Frame
	if conn.ReadJSON(&hello) != nil || hello.Type != "hello" || hello.Node != node || hello.BootID == "" {
		return
	}
	now := time.Now().UTC()
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	link := h.nodes[node]
	if link == nil {
		link = &nodeLink{state: NodeState{ID: node, Mode: "websocket"}}
		h.nodes[node] = link
	}
	old := link.peer
	if link.state.BootID != "" {
		link.state.Reconnects++
	}
	link.generation++
	generation := link.generation
	link.peer = peer
	link.state.Online = true
	link.state.Version = hello.Version
	link.state.Address = r.RemoteAddr
	if address, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		link.state.Address = address
	}
	link.state.WorkDir = hello.WorkDir
	link.state.BootID = hello.BootID
	link.state.ConnectedAt = &now
	link.state.LastSeenAt = &now
	link.state.LastError = ""
	session := h.sessions[node]
	h.mu.Unlock()
	if old != nil {
		_ = old.Conn.Close()
	}
	defer h.disconnected(node, peer, generation)
	if err = peer.Send(wire.Frame{Type: "welcome", Node: node}); err != nil {
		return
	}
	if session != nil && session.bootID != hello.BootID {
		session.finish(errors.New("节点 " + node + " 已重启，原任务无法继续"))
	}
	h.handle(node, peer, wire.Frame{Type: "snapshot", Snapshot: hello.Snapshot})
	if hello.Telemetry != nil {
		h.handle(node, peer, wire.Frame{Type: "telemetry", Telemetry: hello.Telemetry})
	}
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(30 * time.Second)) })
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if peer.Ping() != nil {
					_ = conn.Close()
					return
				}
			}
		}
	}()
	for {
		var frame wire.Frame
		if err := conn.ReadJSON(&frame); err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		h.handle(node, peer, frame)
	}
}

func (h *Hub) handle(node string, peer *wire.Peer, frame wire.Frame) {
	h.mu.Lock()
	link := h.nodes[node]
	if link.peer != peer {
		h.mu.Unlock()
		return
	}
	now := time.Now().UTC()
	link.state.LastSeenAt = &now
	if frame.Type == "heartbeat" {
		link.state.ActiveTaskID = frame.TaskID
	}
	session := h.sessions[node]
	if frame.Type == "result" {
		wait, ok := h.pending[frame.RequestID]
		if ok && wait.node == node {
			delete(h.pending, frame.RequestID)
			select {
			case wait.result <- frame:
			default:
			}
		}
		h.mu.Unlock()
		return
	}
	if frame.Type == "snapshot" && frame.Snapshot != nil {
		if frame.Snapshot.Finished {
			link.state.ActiveTaskID = ""
		} else {
			link.state.ActiveTaskID = frame.Snapshot.TaskID
		}
	}
	if frame.Type == "finished" && session != nil && session.taskID == frame.TaskID {
		link.state.ActiveTaskID = ""
	}
	h.mu.Unlock()
	switch frame.Type {
	case "telemetry":
		if frame.Telemetry != nil {
			h.Telemetry.Add(node, *frame.Telemetry)
		}
	case "snapshot":
		if frame.Snapshot == nil {
			return
		}
		snapshot := frame.Snapshot
		if session == nil {
			if snapshot.TaskID != "" && !snapshot.Finished {
				_ = peer.Send(wire.Frame{Type: "stop", TaskID: snapshot.TaskID})
			}
			return
		}
		if snapshot.TaskID == session.taskID {
			session.snapshot(*snapshot)
			if !snapshot.Finished && session.isStopping() {
				_ = peer.Send(wire.Frame{Type: "stop", TaskID: session.taskID})
			}
		} else if session.isStopping() {
			if snapshot.TaskID == "" || snapshot.Finished {
				session.finish(nil)
			} else {
				session.finish(errors.New("节点已有其他任务，无法确认原任务退出"))
			}
		} else {
			_ = peer.Send(wire.Frame{Type: "dispatch", TaskID: session.taskID, Request: &session.request})
		}
	case "event":
		if session != nil && frame.TaskID == session.taskID && frame.Event != nil {
			session.event(frame.Sequence, *frame.Event)
		}
	case "finished":
		if session != nil && frame.TaskID == session.taskID {
			var err error
			if frame.Error != "" {
				err = errors.New(frame.Error)
			}
			session.finish(err)
		}
	}
}

func (h *Hub) disconnected(node string, peer *wire.Peer, generation uint64) {
	h.mu.Lock()
	link := h.nodes[node]
	if link.peer != peer {
		h.mu.Unlock()
		return
	}
	link.peer = nil
	link.state.Online = false
	link.state.LastError = "连接中断，等待节点重连"
	session := h.sessions[node]
	h.mu.Unlock()
	if session != nil {
		session.receive(worker.Event{Type: "node_offline", Host: session.request.Plan.Host, At: time.Now().UTC(), Message: "WebSocket 已断开，等待重连同步"})
	}
	time.AfterFunc(h.grace, func() {
		h.mu.Lock()
		expired := h.nodes[node].peer == nil && h.nodes[node].generation == generation
		active := h.sessions[node]
		h.mu.Unlock()
		if expired && active != nil {
			active.finish(fmt.Errorf("节点 %s 超过 %s 未重连", node, h.grace))
		}
	})
}

func (h *Hub) Ready(node string) bool {
	if node == "A" {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.nodes[node] != nil && h.nodes[node].state.Online
}
func (h *Hub) Nodes() []NodeState {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []NodeState{{ID: "A", Online: true, Mode: "local", Version: wire.Version}}
	ids := make([]string, 0, len(h.nodes))
	for id := range h.nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out = append(out, h.nodes[id].state)
	}
	return out
}
func (h *Hub) send(node string, frame wire.Frame) error {
	h.mu.Lock()
	link := h.nodes[node]
	var peer *wire.Peer
	if link != nil {
		peer = link.peer
	}
	h.mu.Unlock()
	if peer == nil {
		return fmt.Errorf("节点 %s 未连接到中心", node)
	}
	return peer.Send(frame)
}

func (h *Hub) Check(ctx context.Context, node string, req worker.Request) error {
	id := "check-" + strconv.FormatUint(h.ids.Add(1), 10)
	response := make(chan wire.Frame, 1)
	h.mu.Lock()
	h.pending[id] = rpcWait{node, response}
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.pending, id); h.mu.Unlock() }()
	if err := h.send(node, wire.Frame{Type: "check", RequestID: id, Request: &req}); err != nil {
		return err
	}
	timeout := 25 * time.Second
	if len(req.Assets) > 0 {
		timeout = 180 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case frame := <-response:
		if frame.Error != "" {
			return errors.New(node + ": " + frame.Error)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("节点 %s 环境检查超时", node)
	}
}

func (h *Hub) Start(node string, req worker.Request, receive func(worker.Event)) (wire.TaskSession, error) {
	h.mu.Lock()
	link := h.nodes[node]
	if link == nil || link.peer == nil || !link.state.Online {
		h.mu.Unlock()
		return nil, fmt.Errorf("节点 %s 不在线", node)
	}
	if h.sessions[node] != nil || link.state.ActiveTaskID != "" {
		h.mu.Unlock()
		return nil, fmt.Errorf("节点 %s 已有任务", node)
	}
	s := &remoteSession{hub: h, node: node, taskID: req.TaskID, bootID: link.state.BootID, request: req, receive: receive, done: make(chan error, 1)}
	h.sessions[node] = s
	link.state.ActiveTaskID = req.TaskID
	h.mu.Unlock()
	if err := h.send(node, wire.Frame{Type: "dispatch", TaskID: req.TaskID, Request: &req}); err != nil {
		s.finish(err)
		return nil, err
	}
	return s, nil
}

func (h *Hub) Close() {
	h.mu.Lock()
	h.closed = true
	peers := []*wire.Peer{}
	for _, link := range h.nodes {
		if link.peer != nil {
			peers = append(peers, link.peer)
		}
	}
	h.mu.Unlock()
	for _, peer := range peers {
		_ = peer.Conn.Close()
	}
}

type remoteSession struct {
	hub                  *Hub
	node, taskID, bootID string
	request              worker.Request
	receive              func(worker.Event)
	mu                   sync.Mutex
	sequence             uint64
	done                 chan error
	once                 sync.Once
	result               error
	stopping             bool
}

func (s *remoteSession) Done() <-chan error { return s.done }
func (s *remoteSession) Send(command worker.Command) error {
	return s.hub.send(s.node, wire.Frame{Type: "command", TaskID: s.taskID, Command: &command})
}
func (s *remoteSession) Stop() error {
	s.mu.Lock()
	s.stopping = true
	s.mu.Unlock()
	select {
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.result
	default:
	}
	timeout := time.NewTimer(s.hub.grace + 5*time.Second)
	defer timeout.Stop()
	retry := time.NewTicker(time.Second)
	defer retry.Stop()
	_ = s.hub.send(s.node, wire.Frame{Type: "stop", TaskID: s.taskID})
	for {
		select {
		case <-s.done:
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.result
		case <-retry.C:
			_ = s.hub.send(s.node, wire.Frame{Type: "stop", TaskID: s.taskID})
		case <-timeout.C:
			err := fmt.Errorf("节点 %s 未确认任务退出", s.node)
			s.finish(err)
			return err
		}
	}
}
func (s *remoteSession) isStopping() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.stopping }
func (s *remoteSession) finish(err error) {
	s.once.Do(func() {
		s.mu.Lock()
		s.result = err
		s.mu.Unlock()
		s.hub.mu.Lock()
		if s.hub.sessions[s.node] == s {
			delete(s.hub.sessions, s.node)
		}
		s.hub.mu.Unlock()
		s.done <- err
		close(s.done)
	})
}
func (s *remoteSession) event(sequence uint64, e worker.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sequence <= s.sequence {
		return
	}
	s.sequence = sequence
	e.Host = s.request.Plan.Host
	s.receive(e)
}
func (s *remoteSession) snapshot(snapshot wire.Snapshot) {
	s.mu.Lock()
	if snapshot.Sequence < s.sequence {
		s.mu.Unlock()
		return
	}
	s.sequence = snapshot.Sequence
	for i := range snapshot.Events {
		snapshot.Events[i].Host = s.request.Plan.Host
	}
	s.receive(worker.Event{Type: "snapshot", Host: s.request.Plan.Host, At: time.Now().UTC(), Snapshot: snapshot.Events})
	s.mu.Unlock()
	if snapshot.Finished {
		var err error
		if snapshot.Error != "" {
			err = errors.New(snapshot.Error)
		}
		s.finish(err)
	}
}

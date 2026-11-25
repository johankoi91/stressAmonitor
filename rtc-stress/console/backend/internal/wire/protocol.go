package wire

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"rtcstress/internal/monitor"
	"rtcstress/internal/worker"
)

const Version = "0.12.3"

type Frame struct {
	Telemetry *monitor.Sample `json:"telemetry,omitempty"`
	Type      string          `json:"type"`
	RequestID string          `json:"requestId,omitempty"`
	TaskID    string          `json:"taskId,omitempty"`
	Node      string          `json:"node,omitempty"`
	BootID    string          `json:"bootId,omitempty"`
	Version   string          `json:"version,omitempty"`
	WorkDir   string          `json:"workDir,omitempty"`
	Sequence  uint64          `json:"sequence,omitempty"`
	Request   *worker.Request `json:"request,omitempty"`
	Command   *worker.Command `json:"command,omitempty"`
	Event     *worker.Event   `json:"event,omitempty"`
	Snapshot  *Snapshot       `json:"snapshot,omitempty"`
	Error     string          `json:"error,omitempty"`
	At        time.Time       `json:"at"`
}

type Snapshot struct {
	TaskID   string         `json:"taskId"`
	Sequence uint64         `json:"sequence"`
	Events   []worker.Event `json:"events"`
	Finished bool           `json:"finished"`
	Error    string         `json:"error,omitempty"`
}

// Peer serializes all data writes and keeps a bounded deadline on network I/O.
type Peer struct {
	Conn *websocket.Conn
	mu   sync.Mutex
}

func (p *Peer) Send(frame Frame) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if frame.At.IsZero() {
		frame.At = time.Now().UTC()
	}
	_ = p.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return p.Conn.WriteJSON(frame)
}
func (p *Peer) Ping() error {
	return p.Conn.WriteControl(websocket.PingMessage, []byte("rtc"), time.Now().Add(5*time.Second))
}

// State stores a compact, authoritative task snapshot, not a replay of log lines.
type State struct {
	TaskID      string
	Sequence    uint64
	Connections map[string]worker.Event
	Processes   map[string]worker.Event
	SDK         map[string]worker.Event
	Audio       map[string]worker.Event
	Armed       *worker.Event
	TX, RX      uint64
	Finished    bool
	Error       string
}

func NewState(id string) *State {
	return &State{TaskID: id, Connections: map[string]worker.Event{}, Processes: map[string]worker.Event{}, SDK: map[string]worker.Event{}, Audio: map[string]worker.Event{}}
}
func (s *State) Apply(e worker.Event) {
	if e.Type == "log" {
		return
	}
	s.Sequence++
	switch e.Type {
	case "connected", "disconnected":
		s.Connections[e.Channel+"/"+formatUID(e.UID)] = e
	case "sdk_stats":
		s.SDK[e.Process] = e
	case "process_started", "process_exited":
		s.Processes[e.Process] = e
	case "audio":
		key := e.Channel + "/" + formatUID(e.UID)
		if old, ok := s.Audio[key]; ok {
			e.At = old.At
		}
		s.Audio[key] = e
	case "armed":
		copy := e
		s.Armed = &copy
	case "traffic":
		s.TX += e.TX
		s.RX += e.RX
	case "error":
		s.Error = e.Message
	}
}
func (s *State) Snapshot(node string) Snapshot {
	out := Snapshot{TaskID: s.TaskID, Sequence: s.Sequence, Finished: s.Finished, Error: s.Error, Events: []worker.Event{}}
	for _, e := range s.Connections {
		out.Events = append(out.Events, e)
	}
	for _, e := range s.Processes {
		out.Events = append(out.Events, e)
	}
	for _, e := range s.SDK {
		out.Events = append(out.Events, e)
	}
	// Sort audio observations by first frame time, so replay preserves each source's first frame.
	audio := make([]worker.Event, 0, len(s.Audio))
	for _, e := range s.Audio {
		audio = append(audio, e)
	}
	sortEvents(audio)
	out.Events = append(out.Events, audio...)
	if s.Armed != nil {
		out.Events = append(out.Events, *s.Armed)
	}
	out.Events = append(out.Events, worker.Event{Type: "traffic", Host: node, TX: s.TX, RX: s.RX, At: time.Now().UTC()})
	return out
}

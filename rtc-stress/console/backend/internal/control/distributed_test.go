package control

import (
	"context"
	"rtcstress/internal/scenario"
	"rtcstress/internal/wire"
	"rtcstress/internal/worker"
	"sync"
	"testing"
	"time"
)

type distributedTransport struct {
	expected  int
	mu        sync.Mutex
	started   int
	epochs    map[string]time.Time
	premature bool
}
type distributedSession struct {
	owner   *distributedTransport
	host    string
	receive func(worker.Event)
	done    chan error
	once    sync.Once
}

func (d *distributedTransport) Check(context.Context, scenario.Host, worker.Request) error {
	return nil
}
func (d *distributedTransport) Start(_ context.Context, _ scenario.Host, r worker.Request, receive func(worker.Event)) (wire.TaskSession, error) {
	d.mu.Lock()
	d.started++
	d.mu.Unlock()
	s := &distributedSession{owner: d, host: r.Plan.Host, receive: receive, done: make(chan error, 1)}
	for _, p := range r.Plan.Processes {
		for _, u := range p.Connections {
			receive(worker.Event{Type: "connected", Host: r.Plan.Host, Channel: u.Channel, UID: u.UID})
		}
	}
	return s, nil
}
func (s *distributedSession) Send(c worker.Command) error {
	if c.Type == "begin" {
		s.owner.mu.Lock()
		expected := s.owner.expected
		if expected == 0 {
			expected = 3
		}
		if s.owner.started != expected {
			s.owner.premature = true
		}
		s.owner.epochs[s.host] = c.At
		s.owner.mu.Unlock()
		s.receive(worker.Event{Type: "armed", Host: s.host, At: c.At})
	}
	return nil
}
func (s *distributedSession) Stop() error {
	s.once.Do(func() { s.done <- nil; close(s.done) })
	return nil
}
func (s *distributedSession) Done() <-chan error { return s.done }
func TestDistributedStartWaitsForAllAndUsesOneEpoch(t *testing.T) {
	transport := &distributedTransport{epochs: map[string]time.Time{}}
	m, err := NewManager(t.TempDir(), transport)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		m.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := m.Wait(ctx); err != nil {
			t.Error(err)
		}
	}()
	c := scenario.Defaults()
	c.Distribution = "distributed"
	c.Channels = 3
	c.PublisherChannels = [3]int{1, 1, 1}
	c.Token = "test-app"
	c.AP = "127.0.0.1"
	c.APCert = "ap.test.local"
	c.Allocations = []scenario.RoleAllocation{{Role: "A", NodeID: "A", ProcessCount: 1, MaxUIDsPerProcess: 5}, {Role: "B", NodeID: "B", ProcessCount: 1, MaxUIDsPerProcess: 5}, {Role: "C", NodeID: "C", ProcessCount: 1, MaxUIDsPerProcess: 5}}
	settings := Settings{Config: c, Hosts: scenario.DefaultHosts()}
	for i := range settings.Hosts {
		settings.Hosts[i].Address = []string{"127.0.0.1", "127.0.0.2", "127.0.0.3"}[i]
	}
	if err = m.Update(settings); err != nil {
		t.Fatal(err)
	}
	task, err := m.Create(c)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, _ = m.Get(task.ID)
		if task.State == "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if task.State != "running" {
		t.Fatalf("not running: %s %s", task.State, task.Error)
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.premature || len(transport.epochs) != 3 {
		t.Fatal("did not arm all publisher nodes after joining")
	}
	first := transport.epochs["A"]
	for _, node := range []string{"B", "C"} {
		if !transport.epochs[node].Equal(first) {
			t.Fatal("different material epochs")
		}
	}
	if first.IsZero() || task.StartedAt == nil || !task.StartedAt.Equal(first) {
		t.Fatal("incorrect task epoch")
	}
}

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

type membershipOrderTransport struct {
	base        distributedTransport
	mu          sync.Mutex
	order       []string
	release     func()
	ownersReady bool
	premature   bool
}

func (d *membershipOrderTransport) Check(context.Context, scenario.Host, worker.Request) error {
	return nil
}
func (d *membershipOrderTransport) Start(_ context.Context, _ scenario.Host, r worker.Request, receive func(worker.Event)) (wire.TaskSession, error) {
	emit := func() {
		for _, p := range r.Plan.Processes {
			for _, u := range p.Connections {
				receive(worker.Event{Type: "connected", Host: r.Plan.Host, Channel: u.Channel, UID: u.UID})
			}
		}
	}
	d.mu.Lock()
	if r.Plan.PublishingChannels == 0 && !d.ownersReady {
		d.premature = true
	}
	if r.Plan.Host == "B" {
		d.release = func() { d.mu.Lock(); d.ownersReady = true; d.mu.Unlock(); emit() }
	}
	d.order = append(d.order, r.Plan.Host)
	d.mu.Unlock()
	d.base.mu.Lock()
	d.base.started++
	d.base.mu.Unlock()
	if r.Plan.Host != "B" {
		emit()
	}
	return &distributedSession{owner: &d.base, host: r.Plan.Host, receive: receive, done: make(chan error, 1)}, nil
}
func TestMembershipStartsJoinersOnlyAfterCreatorsReady(t *testing.T) {
	transport := &membershipOrderTransport{base: distributedTransport{expected: 4, epochs: map[string]time.Time{}}}
	manager, err := NewManager(t.TempDir(), transport)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		manager.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := manager.Wait(ctx); err != nil {
			t.Error(err)
		}
	}()
	ptr := func(n int) *int { return &n }
	config := scenario.Defaults()
	config.AllocationPolicy = "membership"
	config.Channels = 2
	config.UsersPerChannel = 5
	config.TotalUIDs = 10
	config.Token = "test-app"
	config.AP = "127.0.0.1"
	config.APCert = "ap.test.local"
	hosts := []scenario.Host{}
	for index, id := range []string{"A", "B", "C", "D"} {
		owned, joined, pub, recv := 1, 0, 2, 1
		if index >= 2 {
			owned, joined, pub, recv = 0, 2, 0, 1
		}
		config.Allocations = append(config.Allocations, scenario.RoleAllocation{Role: id, NodeID: id, PublishingChannels: ptr(owned), JoiningChannels: ptr(joined), PublishersPerChannel: ptr(pub), ReceiversPerChannel: ptr(recv), ProcessCount: 1, MaxUIDsPerProcess: (owned + joined) * (pub + recv)})
		hosts = append(hosts, scenario.Host{ID: id, Address: []string{"127.0.0.1", "127.0.0.2", "127.0.0.3", "127.0.0.4"}[index], WorkDir: "/tmp"})
	}
	if err := manager.Update(Settings{Config: config, Hosts: hosts}); err != nil {
		t.Fatal(err)
	}
	task, err := manager.Create(config)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		transport.mu.Lock()
		defer transport.mu.Unlock()
		return len(transport.order) == 2 && transport.release != nil
	})
	transport.mu.Lock()
	if transport.order[0] != "A" || transport.order[1] != "B" {
		t.Fatal("joiners started before creators")
	}
	release := transport.release
	transport.mu.Unlock()
	// No C/D session exists while B's real connection callback is withheld.
	current, _ := manager.Get(task.ID)
	if current.State != "joining_creators" {
		t.Fatal("creator readiness barrier was skipped", current.State)
	}
	release()
	eventually(t, func() bool { current, _ := manager.Get(task.ID); return current.State == "running" })
	transport.mu.Lock()
	premature := transport.premature
	count := len(transport.order)
	transport.mu.Unlock()
	if premature || count != 4 {
		t.Fatal("existing-channel join order failed")
	}
	transport.base.mu.Lock()
	early := transport.base.premature
	transport.base.mu.Unlock()
	if early {
		t.Fatal("audio began before all machines joined")
	}
}

package control

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"rtcstress/internal/scenario"
	"rtcstress/internal/wire"
	"rtcstress/internal/worker"
)

func quota(n int) *int { return &n }
func TestFlexibleSchedulerIncludesEverySelectedGroup(t *testing.T) {
	transport := &distributedTransport{epochs: map[string]time.Time{}, expected: 4}
	release := make(chan struct{})
	hold := &heldConnectionTransport{distributedTransport: transport, release: release}
	manager, err := NewManager(t.TempDir(), hold)
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
	c := scenario.Defaults()
	c.Channels = 4
	c.Token = "test-app"
	c.AP = "127.0.0.1"
	c.APCert = "ap.test.local"
	settings := Settings{Config: c}
	for i := 0; i < 4; i++ {
		role := fmt.Sprintf("group-%d", i)
		node := fmt.Sprintf("node-%d", i)
		c.Allocations = append(c.Allocations, scenario.RoleAllocation{Role: role, NodeID: node, ProcessCount: 1, MaxUIDsPerProcess: 10, PublishingChannels: quota(1), ReceiversPerChannel: quota(1)})
		settings.Hosts = append(settings.Hosts, scenario.Host{ID: node, Address: fmt.Sprintf("127.0.0.%d", i+1), WorkDir: "/tmp"})
	}
	settings.Config = c
	if err := manager.Update(settings); err != nil {
		t.Fatal(err)
	}
	task, err := manager.Create(c)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { transport.mu.Lock(); defer transport.mu.Unlock(); return transport.started == 4 })
	time.Sleep(30 * time.Millisecond)
	transport.mu.Lock()
	early := len(transport.epochs) > 0
	transport.mu.Unlock()
	if early {
		close(release)
		t.Fatal("audio began before the final selected group joined")
	}
	close(release)
	eventually(t, func() bool { task, _ = manager.Get(task.ID); return task.State == "running" || task.State == "failed" })
	if task.State != "running" {
		t.Fatalf("task failed: %s", task.Error)
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.premature || len(transport.epochs) != 4 {
		t.Fatal("did not synchronize all selected groups")
	}
	for _, epoch := range transport.epochs {
		if task.StartedAt == nil || !epoch.Equal(*task.StartedAt) {
			t.Fatal("publisher epochs differ")
		}
	}
}
func TestCustomNodeDiscoveryWithoutCredentialsAndTelemetry(t *testing.T) {
	hub := NewHub(nil, time.Second)
	defer hub.Close()
	server := httptest.NewServer(hub)
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/?node=node-east-4"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(wire.Frame{Type: "hello", Node: "node-east-4", BootID: "test", Version: wire.Version, WorkDir: "/tmp"}); err != nil {
		t.Fatal(err)
	}
	var welcome wire.Frame
	if err := conn.ReadJSON(&welcome); err != nil || welcome.Type != "welcome" {
		t.Fatal("node registration failed", err)
	}
	if !hub.Ready("node-east-4") {
		t.Fatal("custom node not discovered")
	}
	manager, err := NewManager(t.TempDir(), &ClusterTransport{Hub: hub})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	settings, _ := manager.Settings()
	found := false
	for _, host := range settings.Hosts {
		if host.ID == "node-east-4" {
			found = true
		}
	}
	if !found {
		t.Fatal("discovered node missing from machine choices")
	}
}

type heldConnectionTransport struct {
	*distributedTransport
	release <-chan struct{}
}

func (h *heldConnectionTransport) Start(ctx context.Context, host scenario.Host, request worker.Request, emit func(worker.Event)) (wire.TaskSession, error) {
	if request.Plan.Host != "group-3" {
		return h.distributedTransport.Start(ctx, host, request, emit)
	}
	var events []worker.Event
	session, err := h.distributedTransport.Start(ctx, host, request, func(event worker.Event) {
		if event.Type == "connected" {
			events = append(events, event)
		} else {
			emit(event)
		}
	})
	go func() {
		select {
		case <-h.release:
			for _, event := range events {
				emit(event)
			}
		case <-ctx.Done():
		}
	}()
	return session, err
}

type checkNodeTransport struct{ target string }

func (c *checkNodeTransport) Check(_ context.Context, host scenario.Host, request worker.Request) error {
	if host.ID != request.Plan.TargetNode() {
		return fmt.Errorf("wrong check node %s/%s", host.ID, request.Plan.TargetNode())
	}
	c.target = host.ID
	return nil
}
func (c *checkNodeTransport) Start(context.Context, scenario.Host, worker.Request, func(worker.Event)) (wire.TaskSession, error) {
	return nil, fmt.Errorf("unexpected start")
}
func TestEnvironmentCheckCanTargetAnUnassignedMachine(t *testing.T) {
	transport := &checkNodeTransport{}
	manager, err := NewManager(t.TempDir(), transport)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	config := scenario.Defaults()
	config.Token = "test"
	config.AP = "127.0.0.1"
	config.APCert = "ap.test.local"
	config.Channels = 2
	config.Allocations = []scenario.RoleAllocation{{Role: "publish", NodeID: "node-one", ProcessCount: 1, MaxUIDsPerProcess: 8, PublishingChannels: quota(2), ReceiversPerChannel: quota(1)}}
	settings := Settings{Config: config, Hosts: []scenario.Host{{ID: "node-one", Address: "127.0.0.1", WorkDir: "/tmp"}, {ID: "node-extra", Address: "127.0.0.2", WorkDir: "/tmp"}}}
	if err := manager.Update(settings); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.HostAction(context.Background(), "node-extra"); err != nil {
		t.Fatal(err)
	}
	if transport.target != "node-extra" {
		t.Fatal("wrong environment-check target")
	}
}

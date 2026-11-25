package control

import (
	"context"
	"errors"
	"rtcstress/internal/scenario"
	"rtcstress/internal/wire"
	"rtcstress/internal/worker"
)

type Transport interface {
	Check(context.Context, scenario.Host, worker.Request) error
	Start(context.Context, scenario.Host, worker.Request, func(worker.Event)) (wire.TaskSession, error)
}

type ClusterTransport struct{ Hub *Hub }

func (t *ClusterTransport) Check(ctx context.Context, h scenario.Host, r worker.Request) error {
	if h.ID == "A" {
		return worker.Check(r)
	}
	return t.Hub.Check(ctx, h.ID, r)
}
func (t *ClusterTransport) Start(ctx context.Context, h scenario.Host, r worker.Request, f func(worker.Event)) (wire.TaskSession, error) {
	if h.ID == "A" {
		return wire.StartExecution(ctx, r, f)
	}
	return t.Hub.Start(h.ID, r, f)
}
func (t *ClusterTransport) Ready(id string) bool { return t.Hub.Ready(id) }
func (t *ClusterTransport) Nodes() []NodeState   { return t.Hub.Nodes() }
func (t *ClusterTransport) Disconnect(id string) error {
	if id == "A" {
		return errors.New("A 是本机中心")
	}
	t.Hub.mu.Lock()
	link := t.Hub.nodes[id]
	if link == nil || link.peer == nil {
		t.Hub.mu.Unlock()
		return errors.New("节点不在线")
	}
	peer := link.peer
	t.Hub.mu.Unlock()
	return peer.Conn.Close()
}

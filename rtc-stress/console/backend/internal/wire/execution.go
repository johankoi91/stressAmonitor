package wire

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"rtcstress/internal/worker"
)

type TaskSession interface {
	Send(worker.Command) error
	Stop() error
	Done() <-chan error
}

type Execution struct {
	mu       sync.Mutex
	input    *io.PipeWriter
	cancel   context.CancelFunc
	done     chan error
	result   error
	stopOnce sync.Once
}

func StartExecution(parent context.Context, request worker.Request, receive func(worker.Event)) (*Execution, error) {
	if err := worker.Check(request); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	input, writer := io.Pipe()
	output, events := io.Pipe()
	exec := &Execution{input: writer, cancel: cancel, done: make(chan error, 1)}
	decoded := make(chan struct{})
	go func() {
		defer close(decoded)
		defer output.Close()
		decoder := json.NewDecoder(output)
		for {
			var event worker.Event
			if decoder.Decode(&event) != nil {
				return
			}
			receive(event)
		}
	}()
	go func() {
		err := worker.Serve(ctx, input, events)
		_ = events.Close()
		_ = input.Close()
		<-decoded
		exec.mu.Lock()
		exec.result = err
		exec.mu.Unlock()
		exec.done <- err
		close(exec.done)
	}()
	if err := json.NewEncoder(writer).Encode(request); err != nil {
		cancel()
		_ = writer.Close()
		return nil, err
	}
	return exec, nil
}

func (e *Execution) Send(command worker.Command) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return json.NewEncoder(e.input).Encode(command)
}
func (e *Execution) Done() <-chan error { return e.done }
func (e *Execution) Stop() error {
	e.stopOnce.Do(func() { _ = e.Send(worker.Command{Type: "stop"}) })
	select {
	case <-e.done:
	case <-time.After(25 * time.Second):
		e.cancel()
		_ = e.input.Close()
		return errors.New("等待本机压测进程退出超时")
	}
	e.cancel()
	_ = e.input.Close()
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.result
}

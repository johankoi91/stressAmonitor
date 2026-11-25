//go:build linux || darwin

package worker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func prepareProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	setParentDeath(cmd.SysProcAttr)
}
func stopGroup(cmd *exec.Cmd, done <-chan struct{}) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
	select {
	case <-done:
		return
	case <-time.After(4 * time.Second):
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	select {
	case <-done:
	case <-time.After(time.Second):
	}
}
func makeFIFO(path string) error { return syscall.Mkfifo(path, 0600) }

func openFIFO(ctx context.Context, path string) (*os.File, error) {
	for {
		fd, e := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0600)
		if e == nil {
			return os.NewFile(uintptr(fd), path), nil
		}
		if !errors.Is(e, syscall.ENXIO) && !errors.Is(e, syscall.EINTR) {
			return nil, e
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func writePCM(ctx context.Context, f *os.File, b []byte) error {
	return writePCMTracked(ctx, f, b, nil, nil)
}

func writePCMTracked(ctx context.Context, f *os.File, b []byte, counters *fifoCounters, heartbeat func()) error {
	if counters != nil {
		defer counters.finishWait()
	}
	raw, err := f.SyscallConn()
	if err != nil {
		return err
	}
	for len(b) > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var n int
		var e error
		if err := raw.Control(func(fd uintptr) { n, e = syscall.Write(int(fd), b) }); err != nil {
			return err
		}
		if n > 0 {
			b = b[n:]
			if counters != nil {
				counters.written += uint64(n)
			}
		}
		if counters != nil && errors.Is(e, syscall.EAGAIN) {
			counters.backpressure++
			if counters.waitStarted.IsZero() {
				counters.waitStarted = time.Now()
			}
			if heartbeat != nil && counters.backpressure%128 == 0 {
				heartbeat()
			}
		}
		if e != nil && !errors.Is(e, syscall.EAGAIN) && !errors.Is(e, syscall.EINTR) {
			return e
		}
		if len(b) > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Millisecond):
			}
		}
	}
	return nil
}

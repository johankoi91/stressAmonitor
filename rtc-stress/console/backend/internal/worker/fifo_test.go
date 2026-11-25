package worker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"rtcstress/internal/scenario"
)

func TestFIFOExecHelper(t *testing.T) {
	path := os.Getenv("RTC_FIFO_EXEC_PATH")
	if path == "" {
		return
	}
	fd, err := strconv.Atoi(os.Getenv("RTC_FIFO_EXEC_FD"))
	if err != nil {
		os.Exit(3)
	}
	var expected, inherited syscall.Stat_t
	if syscall.Stat(path, &expected) != nil {
		os.Exit(4)
	}
	if syscall.Fstat(fd, &inherited) == nil && inherited.Dev == expected.Dev && inherited.Ino == expected.Ino {
		os.Exit(2)
	}
	os.Exit(0)
}

// Exercise exec inheritance, rather than only inspecting the open flags.
func TestFIFOWriterDoesNotSurviveExec(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.fifo")
	if err := makeFIFO(path); err != nil {
		t.Fatal(err)
	}
	reader, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(reader)
	writer, err := openFIFO(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestFIFOExecHelper$")
	cmd.Env = append(os.Environ(), "RTC_FIFO_EXEC_PATH="+path, "RTC_FIFO_EXEC_FD="+strconv.Itoa(int(writer.Fd())))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("FIFO writer inherited across exec: %v %s", err, output)
	}
}

func TestActivePCMTimeExcludesWaitingAndGaps(t *testing.T) {
	staggered := scenario.AudioSchedule{Mode: "staggered", OffsetMS: 5000}
	alternating := scenario.AudioSchedule{Mode: "alternating", WindowMS: 5000, GapMS: 500}
	for _, test := range []struct {
		schedule      scenario.AudioSchedule
		source        int
		elapsed, want time.Duration
	}{
		{staggered, 0, -time.Second, 0},
		{staggered, 0, 1500 * time.Millisecond, 1500 * time.Millisecond},
		{staggered, 1, 4 * time.Second, 0},
		{staggered, 1, 6500 * time.Millisecond, 1500 * time.Millisecond},
		{alternating, 0, 5250 * time.Millisecond, 5 * time.Second},
		{alternating, 1, 5250 * time.Millisecond, 0},
		{alternating, 1, 7 * time.Second, 1500 * time.Millisecond},
		{alternating, 0, 12 * time.Second, 6 * time.Second},
		{alternating, 1, 12 * time.Second, 5 * time.Second},
	} {
		if got := activePCMTime(test.schedule, test.source, test.elapsed); got != test.want {
			t.Errorf("%s source %d at %s: got %s want %s", test.schedule.Mode, test.source, test.elapsed, got, test.want)
		}
	}
}

func TestFIFOPrerollIsSilentAndMaterialWaitsForEpoch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clock.fifo")
	if err := makeFIFO(path); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c := scenario.Defaults()
	c.Engine = "native"
	frameBytes := c.SampleRate / 100 * c.AudioChannels * 2
	content := bytes.Repeat([]byte{0x31}, frameBytes*2)
	var epoch atomic.Int64
	notified := make(chan uint64, 10)
	var last fifoProgress
	done := make(chan error, 1)
	go func() {
		done <- feedPCM(ctx, path, content, c, 0, &epoch, func(n uint64) { notified <- n }, func(p fifoProgress) { last = p })
	}()
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	frame := make([]byte, frameBytes)
	for i := 0; i < 3; i++ {
		if _, err := io.ReadFull(reader, frame); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(frame, make([]byte, frameBytes)) {
			t.Fatal("material emitted during join wait")
		}
	}
	select {
	case <-notified:
		t.Fatal("silence counted as material")
	default:
	}
	start := time.Now().Add(60 * time.Millisecond)
	epoch.Store(start.UnixNano())
	for {
		if _, err := io.ReadFull(reader, frame); err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(frame, content[:frameBytes]) {
			if time.Now().Before(start) {
				t.Fatal("material before scheduled epoch")
			}
			break
		}
		if !bytes.Equal(frame, make([]byte, frameBytes)) {
			t.Fatal("unexpected PCM payload")
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
	}
	select {
	case <-notified:
	case <-ctx.Done():
		t.Fatal("missing material notification")
	}
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if last.WrittenBytes == 0 || last.SilentBytes == 0 || last.StreamBytes != last.WrittenBytes+last.SilentBytes {
		t.Fatalf("incorrect PCM accounting: %+v", last)
	}
}

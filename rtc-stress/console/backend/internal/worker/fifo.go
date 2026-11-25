package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"sync/atomic"
	"time"

	"rtcstress/internal/scenario"
)

const pcmPeriod = 10 * time.Millisecond

// These counters describe FIFO input, not encoded network traffic.
type fifoCounters struct {
	written      uint64
	backpressure uint64
	missedTicks  uint64
	writeWait    time.Duration
	waitStarted  time.Time
}

func (c *fifoCounters) finishWait() {
	if !c.waitStarted.IsZero() {
		c.writeWait += time.Since(c.waitStarted)
		c.waitStarted = time.Time{}
	}
}

type fifoProgress struct {
	At              time.Time `json:"at"`
	Process         string    `json:"process"`
	Channel         string    `json:"channel"`
	UID             uint64    `json:"uid"`
	Source          int       `json:"source"`
	Phase           string    `json:"phase"`
	WrittenBytes    uint64    `json:"writtenBytes"`
	StreamBytes     uint64    `json:"streamBytes"`
	SilentBytes     uint64    `json:"silentBytes"`
	ExpectedBytes   uint64    `json:"expectedBytes"`
	ActiveMS        int64     `json:"activeMs"`
	OpenWaitMS      int64     `json:"openWaitMs"`
	WriteWaitMS     int64     `json:"writeWaitMs"`
	Backpressure    uint64    `json:"backpressureCount"`
	MissedTicks     uint64    `json:"missedTickEstimate"`
	BufferedBytes   *int      `json:"bufferedBytes,omitempty"`
	CapacityBytes   *int      `json:"capacityBytes,omitempty"`
	ReadBytesApprox *uint64   `json:"readBytesApprox,omitempty"`
	PipeError       string    `json:"pipeError,omitempty"`
	Error           string    `json:"error,omitempty"`
}

type fifoTrace struct {
	queue   chan fifoProgress
	done    chan struct{}
	dropped atomic.Uint64
}

func openFIFOTrace(path string) (*fifoTrace, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	trace := &fifoTrace{queue: make(chan fifoProgress, 512), done: make(chan struct{})}
	go func() {
		defer close(trace.done)
		defer file.Close()
		buffer := bufio.NewWriterSize(file, 64<<10)
		defer buffer.Flush()
		size := info.Size()
		disabled := false
		write := func(value any) {
			if disabled {
				return
			}
			data, err := json.Marshal(value)
			if err != nil {
				return
			}
			data = append(data, '\n')
			if size+int64(len(data)) > 64<<20 {
				_, _ = buffer.WriteString("{\"type\":\"truncated\",\"reason\":\"64 MiB diagnostic limit\"}\n")
				disabled = true
				return
			}
			n, err := buffer.Write(data)
			size += int64(n)
			if err != nil {
				disabled = true
			}
		}
		flush := func() {
			if dropped := trace.dropped.Swap(0); dropped > 0 {
				write(struct {
					Type  string `json:"type"`
					Count uint64 `json:"count"`
				}{"dropped", dropped})
			}
			if buffer.Flush() != nil {
				disabled = true
			}
		}
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case value, ok := <-trace.queue:
				if !ok {
					flush()
					return
				}
				write(value)
			case <-ticker.C:
				flush()
			}
		}
	}()
	return trace, nil
}

// Recording never blocks PCM producers; the bounded queue drops diagnostics
// under pressure and records the number lost in the next flush.
func (t *fifoTrace) record(progress fifoProgress) {
	select {
	case t.queue <- progress:
	default:
		t.dropped.Add(1)
	}
}
func (t *fifoTrace) close() { close(t.queue); <-t.done }

// activePCMTime integrates only scheduled audio windows, excluding join wait and
// the source offset. It provides an expected byte count even when ticks are lost.
func activePCMTime(schedule scenario.AudioSchedule, source int, elapsed time.Duration) time.Duration {
	if elapsed <= 0 {
		return 0
	}
	if schedule.Mode == "staggered" {
		if source == 1 {
			elapsed -= time.Duration(schedule.OffsetMS) * time.Millisecond
		}
		if elapsed < 0 {
			return 0
		}
		return elapsed
	}
	if schedule.Mode != "alternating" || schedule.WindowMS <= 0 {
		return 0
	}
	window := time.Duration(schedule.WindowMS) * time.Millisecond
	slot := window + time.Duration(schedule.GapMS)*time.Millisecond
	period := 2 * slot
	active := elapsed / period * window
	tail := elapsed % period
	if source == 1 {
		tail -= slot
	}
	if tail < 0 {
		tail = 0
	}
	if tail > window {
		tail = window
	}
	return active + tail
}

func feed(ctx context.Context, path string, data []byte, c scenario.Config, source int, epoch *atomic.Int64, notify func(uint64)) error {
	return feedPCM(ctx, path, data, c, source, epoch, notify, nil)
}

func feedPCM(ctx context.Context, path string, data []byte, c scenario.Config, source int, epoch *atomic.Int64, notify func(uint64), observe func(fifoProgress)) (result error) {
	opening := time.Now()
	if observe != nil {
		observe(fifoProgress{At: opening.UTC(), Phase: "opening"})
	}
	f, err := openFIFO(ctx, path)
	openWait := time.Since(opening).Milliseconds()
	if err != nil {
		if observe != nil {
			observe(fifoProgress{At: time.Now().UTC(), Phase: "closed", OpenWaitMS: openWait, Error: err.Error()})
		}
		return err
	}
	defer f.Close()
	frame := make([]byte, c.SampleRate/100*c.AudioChannels*2)
	offset := 0
	var bytes uint64
	var counters fifoCounters
	var previousTick, lastReport time.Time
	previousActive := false
	reported := false
	report := func(force bool) {
		if observe == nil {
			return
		}
		now := time.Now()
		if !force && !lastReport.IsZero() && now.Sub(lastReport) < 5*time.Second {
			return
		}
		lastReport = now
		start := epoch.Load()
		progress := fifoProgress{At: now.UTC(), Phase: "waiting", OpenWaitMS: openWait, WrittenBytes: bytes, StreamBytes: counters.written, SilentBytes: counters.written - bytes, Backpressure: counters.backpressure, MissedTicks: counters.missedTicks}
		wait := counters.writeWait
		if !counters.waitStarted.IsZero() {
			wait += now.Sub(counters.waitStarted)
		}
		progress.WriteWaitMS = wait.Milliseconds()
		if start > 0 {
			elapsed := now.Sub(time.Unix(0, start))
			active := activePCMTime(c.Schedule, source, elapsed)
			progress.ActiveMS = active.Milliseconds()
			progress.ExpectedBytes = uint64(active/pcmPeriod) * uint64(len(frame))
			if elapsed >= 0 && c.Schedule.Active(source, elapsed.Milliseconds()) {
				progress.Phase = "active"
			} else {
				progress.Phase = "scheduled_pause"
			}
		}
		progress.BufferedBytes, progress.CapacityBytes, progress.PipeError = fifoBuffer(f)
		if progress.BufferedBytes != nil && counters.written >= uint64(*progress.BufferedBytes) {
			read := counters.written - uint64(*progress.BufferedBytes)
			progress.ReadBytesApprox = &read
		}
		if result != nil {
			progress.Error = result.Error()
		}
		if force {
			progress.Phase = "closed"
		}
		observe(progress)
	}
	defer func() { report(true) }()
	report(false)
	ticker := time.NewTicker(pcmPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case tick := <-ticker.C:
			start := epoch.Load()
			// Use current time after any blocked write. A buffered tick may
			// refer to an earlier, already inactive schedule window.
			elapsed := time.Now().Sub(time.Unix(0, start))
			active := start > 0 && elapsed >= 0 && c.Schedule.Active(source, elapsed.Milliseconds())
			if active && previousActive && !previousTick.IsZero() {
				if skipped := tick.Sub(previousTick)/pcmPeriod - 1; skipped > 0 {
					counters.missedTicks += uint64(skipped)
				}
			}
			previousTick = tick
			previousActive = active
			// Keep the Native's capture clock running through join wait and
			// content gaps. Pausing a live PCM source must supply silence rather
			// than block fread on a published custom track.
			if active {
				CopyLoop(frame, data, &offset)
			} else {
				clear(frame)
			}
			if err := writePCMTracked(ctx, f, frame, &counters, func() { report(false) }); err != nil {
				return err
			}
			if active {
				bytes += uint64(len(frame))
				if !reported || bytes%uint64(len(frame)*200) == 0 {
					notify(bytes)
					reported = true
				}
			}
			report(false)
		}
	}
}

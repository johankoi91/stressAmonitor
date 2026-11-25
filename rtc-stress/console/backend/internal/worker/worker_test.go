package worker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"rtcstress/internal/scenario"
)

func TestWAVPayloadAndFormatValidation(t *testing.T) {
	var b bytes.Buffer
	data := make([]byte, 1920)
	for i := range data {
		data[i] = byte(i)
	}
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+len(data)))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16))
	for _, v := range []uint16{1, 2} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	_ = binary.Write(&b, binary.LittleEndian, uint32(48000))
	_ = binary.Write(&b, binary.LittleEndian, uint32(192000))
	_ = binary.Write(&b, binary.LittleEndian, uint16(4))
	_ = binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(data)))
	b.Write(data)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "audio.wav"), b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadAudio("audio.wav", dir, 48000, 2)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("WAV header was not stripped: %v", err)
	}
	if _, err = ReadAudio("audio.wav", dir, 16000, 1); err == nil {
		t.Fatal("incompatible WAV accepted")
	}
	a, _ := ReadAudio("builtin:440", dir, 48000, 2)
	other, _ := ReadAudio("builtin:660", dir, 48000, 2)
	if !DifferentAudio(a, other) || DifferentAudio(a, a) {
		t.Fatal("audio distinction check failed")
	}
	dst := make([]byte, 7)
	offset := 0
	CopyLoop(dst, []byte{1, 2, 3}, &offset)
	if !bytes.Equal(dst, []byte{1, 2, 3, 1, 2, 3, 1}) || offset != 1 {
		t.Fatal(dst, offset)
	}
}

func TestRealCallbackParsing(t *testing.T) {
	e, ok := connectionEvent("[ APP_LOG_INFO ] onConnected: id 2, channelId stress_audio_1000, localUserId 10001, reason 0")
	if !ok || e.Type != "connected" || e.UID != 10001 || e.Channel != "stress_audio_1000" {
		t.Fatal(e, ok)
	}
	if _, ok = connectionEvent("[ APP_LOG_INFO ] onConnected: rtmp connect successfully."); ok {
		t.Fatal("false readiness from unrelated message")
	}
	if !trafficRE.MatchString("[ APP_LOG_INFO ] [123] conn:2 audio(tx,rx): 1920,3840, video(tx,rx): 0,0") {
		t.Fatal("traffic not recognized")
	}
}

func TestPCMBackpressureStillCancels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocked.fifo")
	if err := makeFIFO(path); err != nil {
		t.Fatal(err)
	}
	readerCh := make(chan *os.File, 1)
	go func() { f, _ := os.Open(path); readerCh <- f }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := scenario.Defaults()
	c.Engine = "native"
	var epoch atomic.Int64
	epoch.Store(time.Now().UnixNano())
	data, _ := ReadAudio("builtin:440", "", 48000, 2)
	done := make(chan error, 1)
	go func() { done <- feed(ctx, path, data, c, 0, &epoch, func(uint64) {}) }()
	f := <-readerCh
	defer f.Close()
	// Deliberately do not read: the FIFO fills, and cancellation must still unblock the feeder.
	time.Sleep(500 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("blocked PCM writer ignored cancellation")
	}
}

// This subprocess acts like the documented Native CLI and opens real FIFOs.
func TestNativeHelper(t *testing.T) {
	if os.Getenv("RTC_TEST_NATIVE_HELPER") != "1" {
		return
	}
	var args []string
	for i, s := range os.Args {
		if s == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	sections := []map[string]string{{}}
	for _, s := range args {
		if s == "-O" {
			sections = append(sections, map[string]string{})
			continue
		}
		if k, v, ok := strings.Cut(s, "="); ok {
			sections[len(sections)-1][k] = v
		}
	}
	joinStep, _ := strconv.Atoi(os.Getenv("RTC_TEST_NATIVE_JOIN_STEP_MS"))
	for i, s := range sections {
		if joinStep > 0 {
			time.Sleep(time.Duration(joinStep) * time.Millisecond)
		}
		fmt.Fprintf(os.Stderr, "[ APP_LOG_INFO ] onConnected: id %d, channelId %s, localUserId %s, reason 0\n", i, s["--cname"], s["--uid"])
	}
	var wg sync.WaitGroup
	for _, s := range sections {
		path := s["--audio-input"]
		if path == "" {
			continue
		}
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			f, err := os.Open(path)
			if err != nil {
				os.Exit(2)
			}
			defer f.Close()
			_, _ = io.Copy(io.Discard, f)
		}(path)
	}
	wg.Wait()
	select {}
}

func TestWorkerGatesMaterialUntilReadyAndStopsOnControl(t *testing.T) {
	t.Setenv("RTC_TEST_NATIVE_HELPER", "1")
	dir := t.TempDir()
	script := filepath.Join(dir, "native")
	text := "#!/bin/sh\nexec " + strconv.Quote(os.Args[0]) + " -test.run=TestNativeHelper -- \"$@\"\n"
	if err := os.WriteFile(script, []byte(text), 0700); err != nil {
		t.Fatal(err)
	}
	c := scenario.Defaults()
	c.Engine = "native"
	c.Channels = 1
	c.Token = "test-secret"
	c.AP = "127.0.0.1"
	c.APCert = "audit.invalid"
	c.Schedule.OffsetMS = 150
	c.LaunchIntervalMS = 0
	plan, _ := scenario.Build(c)
	req := Request{TaskID: "fifo-test", Config: c, Plan: plan.Hosts[0], WorkDir: dir, Binary: script}
	inRead, inWrite := io.Pipe()
	outRead, outWrite := io.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { err := Serve(ctx, inRead, outWrite); _ = outWrite.Close(); result <- err }()
	events := make(chan Event, 100)
	go func() {
		defer close(events)
		decoder := json.NewDecoder(outRead)
		for {
			var e Event
			if decoder.Decode(&e) != nil {
				return
			}
			events <- e
		}
	}()
	encoder := json.NewEncoder(inWrite)
	if err := encoder.Encode(req); err != nil {
		t.Fatal(err)
	}
	joined := 0
	for joined < 3 {
		select {
		case e := <-events:
			if e.Type == "audio" {
				t.Fatal("audio before begin")
			}
			if e.Type == "connected" {
				joined++
			}
		case <-ctx.Done():
			t.Fatal("join timeout")
		}
	}
	select {
	case e := <-events:
		if e.Type == "audio" {
			t.Fatal("audio before begin")
		}
	case <-time.After(30 * time.Millisecond):
	}
	if err := encoder.Encode(Command{Type: "begin", DelayMS: 20}); err != nil {
		t.Fatal(err)
	}
	var first [2]time.Time
	for first[0].IsZero() || first[1].IsZero() {
		select {
		case e := <-events:
			if e.Type == "audio" && e.Source != nil {
				first[*e.Source] = e.At
			}
			if e.Type == "error" {
				t.Fatal(e.Message)
			}
		case <-ctx.Done():
			t.Fatal("PCM deadline")
		}
	}
	if delta := first[1].Sub(first[0]); delta < 120*time.Millisecond || delta > 600*time.Millisecond {
		t.Fatalf("offset not applied: %s", delta)
	}
	if err := encoder.Encode(Command{Type: "stop"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("worker did not stop")
	}
	entries, err := os.ReadDir(filepath.Join(dir, "console-runs", req.TaskID, req.Plan.Host))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".log") || strings.HasSuffix(entry.Name(), ".jsonl") {
			t.Fatalf("runtime log file was collected: %s", entry.Name())
		}
	}
	_ = inWrite.Close()
	_ = outRead.Close()
}

func TestWorkerWaitsForProcessReadyAndStopsDuringNextStartup(t *testing.T) {
	t.Setenv("RTC_TEST_NATIVE_HELPER", "1")
	t.Setenv("RTC_TEST_NATIVE_JOIN_STEP_MS", "40")
	dir := t.TempDir()
	binary := filepath.Join(dir, "native")
	script := "#!/bin/sh\nexec " + strconv.Quote(os.Args[0]) + " -test.run=TestNativeHelper -- \"$@\"\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	c := scenario.Defaults()
	c.Engine = "native"
	c.Channels = 2
	c.Allocations = []scenario.RoleAllocation{{Role: "A", NodeID: "A", ProcessCount: 2, MaxUIDsPerProcess: 3}, {Role: "B", NodeID: "B", ProcessCount: 1, MaxUIDsPerProcess: 2}, {Role: "C", NodeID: "C", ProcessCount: 1, MaxUIDsPerProcess: 2}}
	c.Token = "test-only"
	c.AP = "127.0.0.1"
	c.APCert = "audit.invalid"
	c.LaunchIntervalMS = 0
	plan, err := scenario.Build(c)
	if err != nil {
		t.Fatal(err)
	}
	input, commands := io.Pipe()
	output, events := io.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer input.Close()
	defer commands.Close()
	defer output.Close()
	result := make(chan error, 1)
	go func() { err := Serve(ctx, input, events); events.Close(); result <- err }()
	request := Request{TaskID: "startup-barrier", Config: c, Plan: plan.Hosts[0], WorkDir: dir, Binary: binary}
	encoder := json.NewEncoder(commands)
	if err := encoder.Encode(request); err != nil {
		t.Fatal(err)
	}
	observed := make(chan Event, 100)
	go func() {
		decoder := json.NewDecoder(output)
		for {
			var event Event
			if decoder.Decode(&event) != nil {
				return
			}
			observed <- event
		}
	}()
	firstConnections := 0
	for {
		select {
		case event := <-observed:
			if event.Type == "connected" && event.Process == "A-001" {
				firstConnections++
			}
			if event.Type == "process_started" && event.Process == "A-002" {
				if firstConnections != 3 {
					t.Fatalf("second process started after only %d first-process connections", firstConnections)
				}
				if err := encoder.Encode(Command{Type: "stop"}); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-result:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("stop blocked during process initialization")
				}
				return
			}
		case err := <-result:
			t.Fatalf("worker exited before second process: %v", err)
		case <-ctx.Done():
			t.Fatal("process startup timed out")
		}
	}
}

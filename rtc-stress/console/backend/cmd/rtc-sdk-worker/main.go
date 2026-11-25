//go:build linux && cgo && agora_sdk

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	sdk "github.com/AgoraIO-Extensions/Agora-Golang-Server-SDK/v2/go_sdk/rtc"
	"rtcstress/internal/scenario"
	"rtcstress/internal/worker"
)

type emitter struct {
	mu            sync.Mutex
	encoder       *json.Encoder
	host, process string
}

func (e *emitter) send(event worker.Event) {
	if event.Type == "log" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	event.Host = e.host
	event.Process = e.process
	if event.At.IsZero() {
		event.At = time.Now().UTC()
	}
	_ = e.encoder.Encode(event)
}

type remoteStream struct {
	source     int
	sampled    atomic.Uint32
	received   atomic.Bool
	lastPacket atomic.Int64
	bitrate    atomic.Int64
}
type connection struct {
	encodedSent     atomic.Uint64
	framesSent      atomic.Uint64
	mediaMicros     atomic.Uint64
	matched         atomic.Uint64
	mismatched      atomic.Uint64
	plan            scenario.Connection
	opusPacketUS    atomic.Int64
	codecType       atomic.Int64
	encodedChannels atomic.Int64
	rtc             *sdk.RtcConnection
	peers           map[string]*remoteStream
	material        atomic.Uint64
	sent            atomic.Uint64
	rx              atomic.Uint64
	packets         atomic.Uint64
	errors          atomic.Uint64
	bitrate         atomic.Int64
	published       atomic.Bool
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	output := os.NewFile(3, "sdk-events")
	if output == nil {
		fmt.Fprintln(os.Stderr, "event descriptor missing")
		os.Exit(1)
	}
	defer output.Close()
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 8<<20))
	var request worker.Request
	if err := decoder.Decode(&request); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(request.Plan.Processes) != 1 {
		fmt.Fprintln(os.Stderr, "expected one process plan")
		os.Exit(1)
	}
	process := request.Plan.Processes[0]
	events := &emitter{encoder: json.NewEncoder(output), host: request.Plan.Host, process: process.ID}
	if err := run(ctx, decoder, request, events); err != nil {
		events.send(worker.Event{Type: "error", Message: err.Error()})
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(parent context.Context, decoder *json.Decoder, request worker.Request, events *emitter) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	process := request.Plan.Processes[0]
	cfg := request.Config
	peerIndex, err := cfg.AudioPeerIndex(process.ChannelNames)
	if err != nil {
		return err
	}
	audioScenario := sdk.AudioScenarioChorus
	if cfg.AudioScenario == "standard" {
		audioScenario = sdk.AudioScenarioDefault
	}
	var epoch atomic.Int64
	failures := make(chan error, 1)
	fail := func(err error) {
		select {
		case failures <- err:
		default:
		}
		cancel()
	}
	go func() {
		for {
			var command worker.Command
			if err := decoder.Decode(&command); err != nil {
				cancel()
				return
			}
			switch command.Type {
			case "stop":
				cancel()
				return
			case "begin":
				if command.At.IsZero() {
					command.At = time.Now().UTC().Add(time.Duration(command.DelayMS) * time.Millisecond)
				}
				if command.At.IsZero() || !epoch.CompareAndSwap(0, command.At.UnixNano()) {
					fail(errors.New("invalid or repeated audio start"))
					return
				}
				events.send(worker.Event{Type: "armed", At: command.At, Message: "Go SDK素材起始时间已设置"})
			}
		}
	}()
	dir := filepath.Join(request.WorkDir, "console-runs", request.TaskID, request.Plan.Host, process.ID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	var sources [2][]byte
	var encoded [2]*worker.OpusAudio
	var fingerprints [2]map[[32]byte]struct{}
	if cfg.AudioInputMode == "opus" {
		for i := 0; i < 2; i++ {
			audio, err := worker.ReadOpus(cfg.AudioFiles[i], request.WorkDir)
			if err != nil {
				return err
			}
			encoded[i] = audio
			fingerprints[i] = map[[32]byte]struct{}{}
			for _, packet := range audio.Packets {
				fingerprints[i][sha256.Sum256(packet)] = struct{}{}
			}
		}
	}
	if request.Plan.HasPublishers() && cfg.AudioInputMode != "opus" {
		for source := 0; source < 2; source++ {
			data, err := worker.ReadAudio(cfg.AudioFiles[source], request.WorkDir, cfg.SampleRate, cfg.AudioChannels)
			if err != nil {
				return err
			}
			sources[source] = data
		}
	}
	services := []*sdk.AgoraService{}
	connections := []*connection{}
	var pumps sync.WaitGroup
	defer func() {
		cancel()
		pumps.Wait()
		for _, state := range connections {
			if state.published.Load() {
				_ = state.rtc.UnpublishAudio()
			}
			_ = state.rtc.Disconnect()
			state.rtc.Release()
		}
		for index := len(services) - 1; index >= 0; index-- {
			_ = services[index].Release()
		}
		events.send(worker.Event{Type: "stopped", Message: "SDK connections and services released"})
	}()
	groups := scenario.ServiceGroups(cfg, process)
	nativeIDs := map[string]bool{}
	for groupIndex, group := range groups {
		if ctx.Err() != nil {
			break
		}
		serviceDir := filepath.Join(dir, fmt.Sprintf("service-%03d", groupIndex+1))
		if err := os.MkdirAll(serviceDir, 0700); err != nil {
			return err
		}
		serviceConfig := sdk.NewAgoraServiceConfig()
		serviceConfig.AppId = cfg.Token
		serviceConfig.EnableAudioProcessor = true
		serviceConfig.EnableAudioDevice = false
		serviceConfig.EnableVideo = false
		serviceConfig.UseDirectCustomAudioTrack = true
		serviceConfig.AudioScenario = audioScenario
		serviceConfig.EnableSteroEncodeMode = 1
		serviceConfig.IdleMode = false
		serviceConfig.APMModel = 0
		serviceConfig.LogPath = filepath.Join(serviceDir, "sdk.log")
		serviceConfig.LogLevel = 0 // LOG_LEVEL_NONE: disable native SDK log files.
		serviceConfig.LogSize = 512
		serviceConfig.ConfigDir = serviceDir
		serviceConfig.DataDir = serviceDir
		service, err := sdk.NewService(serviceConfig)
		if err != nil {
			return fmt.Errorf("service %d: %w", groupIndex+1, err)
		}
		services = append(services, service)
		nativeID := service.NativeID()
		if nativeIDs[nativeID] {
			return errors.New("multiple Service objects reused the same native identity")
		}
		nativeIDs[nativeID] = true
		for _, user := range group {
			if ctx.Err() != nil {
				break
			}
			state := &connection{plan: user}
			publisher := user.AudioSource >= 0
			role := sdk.ClientRoleAudience
			if publisher {
				role = sdk.ClientRoleBroadcaster
			}
			connectionConfig := &sdk.RtcConnectionConfig{AutoSubscribeAudio: true, AutoSubscribeVideo: false, EnableAudioRecordingOrPlayout: false, ClientRole: role, ChannelProfile: sdk.ChannelProfileLiveBroadcasting, ChannelType: sdk.ChannelTypeStandard, AudioRecvEncodedFrame: true}
			publishConfig := sdk.NewRtcConPublishConfig()
			publishConfig.IsPublishAudio = publisher
			publishConfig.IsPublishVideo = false
			publishConfig.AudioScenario = audioScenario
			publishConfig.AudioProfile = sdk.AudioProfileDefault
			if publisher && cfg.AudioInputMode == "opus" {
				publishConfig.AudioPublishType = sdk.AudioPublishTypeEncodedPcm
			}
			if !publisher {
				publishConfig.AudioPublishType = sdk.AudioPublishTypeNoPublish
			}
			rtc := service.NewRtcConnection(connectionConfig, publishConfig)
			if rtc == nil {
				return fmt.Errorf("create connection %s/%d", user.Channel, user.UID)
			}
			state.rtc = rtc
			connections = append(connections, state)
			peers := peerIndex[user.Channel]
			state.peers = make(map[string]*remoteStream, len(peers))
			for _, peer := range peers {
				if peer.UID == user.UID {
					continue
				}
				state.peers[strconv.FormatUint(peer.UID, 10)] = &remoteStream{source: peer.AudioSource}
			}
			if publisher && cfg.AudioInputMode != "opus" && rtc.ConfigurePcmBuffer(20, 10) < 0 {
				return fmt.Errorf("configure PCM buffer %s/%d", user.Channel, user.UID)
			}
			if ret := rtc.RegisterObserver(&sdk.RtcConnectionObserver{
				OnConnected: func(_ *sdk.RtcConnection, _ *sdk.RtcConnectionInfo, _ int) {
					events.send(worker.Event{Type: "connected", Channel: user.Channel, UID: user.UID})
				},
				OnReconnected: func(_ *sdk.RtcConnection, _ *sdk.RtcConnectionInfo, _ int) {
					events.send(worker.Event{Type: "connected", Channel: user.Channel, UID: user.UID})
				},
				OnDisconnected: func(_ *sdk.RtcConnection, _ *sdk.RtcConnectionInfo, _ int) {
					events.send(worker.Event{Type: "disconnected", Channel: user.Channel, UID: user.UID})
				},
				OnConnectionFailure: func(_ *sdk.RtcConnection, _ *sdk.RtcConnectionInfo, code int) {
					if ctx.Err() == nil {
						fail(fmt.Errorf("connect %s/%d failed: %d", user.Channel, user.UID, code))
					}
				},
			}); ret < 0 {
				return fmt.Errorf("register connection observer: %d", ret)
			}
			if ret := rtc.RegisterLocalUserObserver(&sdk.LocalUserObserver{
				OnLocalAudioTrackStatistics: func(_ *sdk.LocalUser, stats *sdk.LocalAudioTrackStats) { state.bitrate.Store(int64(stats.SentBitrate)) },
				OnRemoteAudioTrackStatistics: func(_ *sdk.LocalUser, uid string, stats *sdk.RemoteAudioTrackStats) {
					if peer := state.peers[uid]; peer != nil {
						peer.bitrate.Store(int64(stats.ReceivedBitrate))
					}
				},
			}); ret < 0 {
				return fmt.Errorf("register local observer: %d", ret)
			}
			if ret := rtc.RegisterAudioEncodedFrameObserver(&sdk.AudioEncodedFrameObserver{OnEncodedAudioFrameReceived: func(remote string, packet []byte, _ int64, codec int) {
				state.codecType.CompareAndSwap(0, int64(codec)+1)
				if codec == int(sdk.AudioCodecOpus) && state.opusPacketUS.Load() == 0 {
					us, channels := worker.OpusPacketInfo(packet)
					if us > 0 {
						state.encodedChannels.Store(int64(channels))
						state.opusPacketUS.Store(int64(us))
					}
				}
				state.rx.Add(uint64(len(packet)))
				state.packets.Add(1)
				if peer := state.peers[remote]; peer != nil {
					if cfg.AudioInputMode == "opus" {
						if n := peer.sampled.Add(1); n <= 32 || n%50 == 0 {
							if _, ok := fingerprints[peer.source][sha256.Sum256(packet)]; ok {
								state.matched.Add(1)
							} else {
								state.mismatched.Add(1)
							}
						}
					}
					peer.lastPacket.Store(time.Now().UnixNano())
					peer.received.Store(true)
				}
			}}); ret < 0 {
				return fmt.Errorf("register encoded observer: %d", ret)
			}
			// The private AP parameters must be applied after creating a
			// Connection but before Connect, as required by SDK 2.6.6.
			for _, value := range []map[string]any{{"rtc.enable_nasa2": false}, {"rtc.force_local": true}, {"rtc.local_domain": cfg.APCert}, {"rtc.local_ap_list": []string{cfg.AP}}} {
				parameter, _ := json.Marshal(value)
				if result := service.GetAgoraParameter().SetParameters(string(parameter)); result < 0 {
					return fmt.Errorf("private AP parameter: %d", result)
				}
			}
			if cfg.VOSIP != "" {
				parameter, _ := json.Marshal(map[string]any{"rtc.vos_list": []string{fmt.Sprintf("%s:%d", cfg.VOSIP, cfg.VOSPort)}})
				if result := rtc.GetAgoraParameter().SetParameters(string(parameter)); result < 0 {
					return fmt.Errorf("RTN parameter: %d", result)
				}
			}
			// The supplied 32-character value is the private deployment AppId;
			// no expiring RTC token is configured for this environment.
			if result := rtc.Connect("", user.Channel, strconv.FormatUint(user.UID, 10), ""); result < 0 {
				return fmt.Errorf("Connect %s/%d: %d", user.Channel, user.UID, result)
			}
			if publisher {
				pumps.Add(1)
				go func() {
					defer pumps.Done()
					if cfg.AudioInputMode == "opus" {
						pumpEncoded(ctx, state, encoded[user.AudioSource], cfg, &epoch, events)
					} else {
						pump(ctx, state, sources[user.AudioSource], cfg, &epoch, events)
					}
				}()
			}
		}
	}
	ids := make([]string, 0, len(services))
	for _, service := range services {
		ids = append(ids, service.NativeID())
	}
	var lastTX, lastRX uint64
	report := func() {
		stats := &worker.SDKStats{Version: "Go SDK 2.6.6 / native 4.6.31.135", Layout: cfg.ServiceLayout, Services: len(services), Connections: len(connections), ServiceIDs: ids, DecodeEnabled: false}
		stats.AudioInputMode = cfg.Normalized().AudioInputMode
		stats.EncodingEnabled = cfg.AudioInputMode != "opus"
		stats.AudioScenario = cfg.Normalized().AudioScenario
		stats.PID = os.Getpid()
		if data, err := os.ReadFile("/proc/self/status"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				fields := strings.Fields(line)
				if len(fields) > 1 {
					if fields[0] == "VmRSS:" {
						n, _ := strconv.ParseUint(fields[1], 10, 64)
						stats.RSSBytes = n * 1024
					}
					if fields[0] == "Threads:" {
						stats.Threads, _ = strconv.Atoi(fields[1])
					}
				}
			}
		}
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		stats.GoHeapBytes = memory.HeapAlloc
		codecs := map[int]bool{}
		for _, state := range connections {
			if code := state.codecType.Load(); code > 0 && !codecs[int(code-1)] {
				codecs[int(code-1)] = true
				stats.EncodedCodecTypes = append(stats.EncodedCodecTypes, int(code-1))
			}
			us := int(state.opusPacketUS.Load())
			if us > 0 {
				if stats.OpusPacketUSMin == 0 || us < stats.OpusPacketUSMin {
					stats.OpusPacketUSMin = us
				}
				if us > stats.OpusPacketUSMax {
					stats.OpusPacketUSMax = us
				}
			}
			switch state.encodedChannels.Load() {
			case 1:
				stats.OpusMonoConnections++
			case 2:
				stats.OpusStereoConnections++
			}
			stats.EncodedSent += state.encodedSent.Load()
			stats.AudioFramesSent += state.framesSent.Load()
			stats.AudioSentMicros += state.mediaMicros.Load()
			stats.PayloadMatched += state.matched.Load()
			stats.PayloadMismatched += state.mismatched.Load()
			stats.PCMSent += state.sent.Load()
			stats.EncodedReceived += state.rx.Load()
			stats.Packets += state.packets.Load()
			stats.SendErrors += state.errors.Load()
			stats.SendBitrateKbps += int(state.bitrate.Load())
			for _, peer := range state.peers {
				stats.ReceiveBitrateKbps += int(peer.bitrate.Load())
				stats.ExpectedStreams++
				if at := peer.lastPacket.Load(); at > 0 && time.Since(time.Unix(0, at)) < 5*time.Second {
					stats.ActiveStreams++
				}
				if peer.received.Load() {
					stats.ReceivedStreams++
				}
			}
		}
		events.send(worker.Event{Type: "traffic", TX: stats.PCMSent + stats.EncodedSent - lastTX, RX: stats.EncodedReceived - lastRX})
		lastTX = stats.PCMSent + stats.EncodedSent
		lastRX = stats.EncodedReceived
		events.send(worker.Event{Type: "sdk_stats", SDK: stats})

	}
	report()
	defer report()
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			select {
			case err := <-failures:
				return err
			default:
				return nil
			}
		case err := <-failures:
			return err
		case <-timer.C:
			report()
		}
	}
}
func pump(ctx context.Context, state *connection, data []byte, cfg scenario.Config, epoch *atomic.Int64, events *emitter) {
	frame := make([]byte, cfg.SampleRate/100*cfg.AudioChannels*2)
	offset := 0
	timer := time.NewTicker(10 * time.Millisecond)
	defer timer.Stop()
	lastNotify := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			start := epoch.Load()
			if start == 0 {
				continue
			}
			elapsed := time.Now().Sub(time.Unix(0, start))
			active := elapsed >= 0 && cfg.Schedule.Active(state.plan.AudioSource, elapsed.Milliseconds())
			if !active && !state.published.Load() {
				continue
			}
			if !state.published.Load() {
				if result := state.rtc.PublishAudio(); result < 0 {
					state.errors.Add(1)
					continue
				}
				state.published.Store(true)
			}
			if active {
				worker.CopyLoop(frame, data, &offset)
			} else {
				clear(frame)
			}
			result := state.rtc.PushAudioPcmData(frame, cfg.SampleRate, cfg.AudioChannels, 0)
			if result != 0 {
				state.errors.Add(1)
				continue
			}
			state.framesSent.Add(1)
			state.mediaMicros.Add(10000)
			state.sent.Add(uint64(len(frame)))
			if active {
				value := state.material.Add(uint64(len(frame)))
				if lastNotify.IsZero() || time.Since(lastNotify) >= 2*time.Second {
					source := state.plan.AudioSource
					events.send(worker.Event{Type: "audio", Channel: state.plan.Channel, UID: state.plan.UID, Source: &source, Bytes: value})
					lastNotify = time.Now()
				}
			}
		}
	}
}

func pumpEncoded(ctx context.Context, state *connection, audio *worker.OpusAudio, cfg scenario.Config, epoch *atomic.Int64, events *emitter) {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	index := 0
	lastNotify := time.Time{}
	info := &sdk.EncodedAudioFrameInfo{Speech: true, Codec: sdk.AudioCodecOpus, SampleRateHz: 48000, SamplesPerChannel: 960, NumberOfChannels: audio.Channels, CaptureTimeMs: -1}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			start := epoch.Load()
			if start == 0 {
				continue
			}
			elapsed := time.Since(time.Unix(0, start))
			if elapsed < 0 || !cfg.Schedule.Active(state.plan.AudioSource, elapsed.Milliseconds()) {
				continue
			}
			if !state.published.Load() {
				if ret := state.rtc.PublishAudio(); ret < 0 {
					state.errors.Add(1)
					continue
				}
				state.published.Store(true)
			}
			packet := audio.Packets[index]
			if ret := state.rtc.PushAudioEncodedData(packet, info); ret != 0 {
				state.errors.Add(1)
				continue
			}
			index = (index + 1) % len(audio.Packets)
			state.encodedSent.Add(uint64(len(packet)))
			state.framesSent.Add(1)
			state.mediaMicros.Add(20000)
			material := state.material.Add(uint64(len(packet)))
			if lastNotify.IsZero() || time.Since(lastNotify) >= 2*time.Second {
				source := state.plan.AudioSource
				events.send(worker.Event{Type: "audio", Channel: state.plan.Channel, UID: state.plan.UID, Source: &source, Bytes: material})
				lastNotify = time.Now()
			}
		}
	}
}

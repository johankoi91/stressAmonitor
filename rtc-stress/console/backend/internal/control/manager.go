package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"rtcstress/internal/scenario"
	"rtcstress/internal/wire"
	"rtcstress/internal/worker"
)

type Settings struct {
	Config scenario.Config `json:"config"`
	Hosts  []scenario.Host `json:"hosts"`
}
type ProcessState struct {
	SDK       *worker.SDKStats `json:"sdk,omitempty"`
	ID        string           `json:"id"`
	PID       int              `json:"pid"`
	State     string           `json:"state"`
	Expected  int              `json:"expected"`
	Connected int              `json:"connected"`
}
type MachineState struct {
	NodeID      string         `json:"nodeId"`
	NodeAddress string         `json:"nodeAddress"`
	ID          string         `json:"id"`
	Expected    int            `json:"expected"`
	Connected   int            `json:"connected"`
	Joined      int            `json:"joined"`
	Processes   []ProcessState `json:"processes"`
	FirstAudio  [2]*time.Time  `json:"firstAudio"`
	AudioBytes  [2]uint64      `json:"audioBytes"`
	AudioTX     uint64         `json:"audioTx"`
	AudioRX     uint64         `json:"audioRx"`
}
type Task struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	State      string          `json:"state"`
	Error      string          `json:"error,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
	StartedAt  *time.Time      `json:"startedAt,omitempty"`
	FinishedAt *time.Time      `json:"finishedAt,omitempty"`
	Config     scenario.Config `json:"config"`
	Plan       scenario.Plan   `json:"plan"`
	Machines   []MachineState  `json:"machines"`
}
type runState struct {
	armed      map[string]time.Time
	task       Task
	ctx        context.Context
	cancel     context.CancelFunc
	notify     chan struct{}
	failure    chan error
	connected  map[string]bool
	joined     map[string]bool
	expected   map[string]bool
	processes  map[string]ProcessState
	audio      map[string]uint64
	tx         map[string]uint64
	rx         map[string]uint64
	firstAudio map[string]time.Time
	sessions   map[string]wire.TaskSession
}
type Manager struct {
	mu          sync.Mutex
	settings    Settings
	tasks       map[string]*runState
	active      string
	maintenance bool
	dataDir     string
	transport   Transport
	audio       *AudioStore
}

func NewManager(dataDir string, t Transport, defaultsDir ...string) (*Manager, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	m := &Manager{dataDir: dataDir, transport: t, tasks: map[string]*runState{}, settings: Settings{Config: scenario.Defaults(), Hosts: scenario.DefaultHosts()}}
	store, err := NewAudioStore(dataDir)
	if err != nil {
		return nil, err
	}
	m.audio = store
	var defaults *Settings
	if len(defaultsDir) > 0 && defaultsDir[0] != "" {
		defaults, err = m.loadDefaults(defaultsDir[0])
		if err != nil {
			return nil, err
		}
	}
	if data, err := os.ReadFile(filepath.Join(dataDir, "settings.json")); err == nil {
		if err = json.Unmarshal(data, &m.settings); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	} else if defaults != nil {
		if err := m.Update(*defaults); err != nil {
			return nil, fmt.Errorf("初始化默认配置: %w", err)
		}
	}
	return m, nil
}

func terminal(s string) bool {
	return s == "completed" || s == "stopped" || s == "failed" || s == "interrupted"
}
func (m *Manager) Settings() (Settings, bool) {
	discovered := []NodeState{}
	if cluster, ok := m.transport.(interface{ Nodes() []NodeState }); ok {
		discovered = cluster.Nodes()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.settings
	s.Config = s.Config.Normalized()
	s.Config, _ = m.audio.Normalize(s.Config)
	s.Hosts = mergeDiscoveredHosts(s.Hosts, discovered)
	has := s.Config.Token != ""
	return s, has
}

func (m *Manager) Nodes() []NodeState {
	nodes := []NodeState{}
	if cluster, ok := m.transport.(interface{ Nodes() []NodeState }); ok {
		nodes = cluster.Nodes()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range nodes {
		for _, host := range m.settings.Hosts {
			if host.ID == nodes[i].ID {
				if nodes[i].Address == "" {
					nodes[i].Address = host.Address
				}
				if nodes[i].WorkDir == "" {
					nodes[i].WorkDir = host.WorkDir
				}
			}
		}
		if nodes[i].ID == "A" {
			nodes[i].ActiveTaskID = m.active
		}
	}
	return nodes
}

func (m *Manager) Update(s Settings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != "" || m.maintenance {
		return errors.New("任务或环境检查正在进行，暂不能修改机器及场景配置")
	}
	if s.Config.Token == "" {
		s.Config.Token = m.settings.Config.Token
	}
	s.Config = s.Config.Normalized()
	normalized, err := m.audio.Normalize(s.Config)
	if err != nil {
		return err
	}
	s.Config = normalized
	if err := s.Config.Validate(false); err != nil {
		return err
	}
	if len(s.Hosts) < 1 {
		return errors.New("请至少配置一台机器")
	}
	seen := map[string]bool{}
	addresses := map[string]bool{}
	for _, h := range s.Hosts {
		if err := h.Validate(); err != nil {
			return err
		}
		if seen[h.ID] || addresses[h.Address] {
			return errors.New("机器标识和地址须分别唯一")
		}
		seen[h.ID] = true
		addresses[h.Address] = true
	}
	for _, row := range s.Config.Allocations {
		if !seen[row.NodeID] {
			return fmt.Errorf("执行分组 %s 选择的节点 %s 未发现或未配置", row.Role, row.NodeID)
		}
	}
	if err := writeJSON(filepath.Join(m.dataDir, "settings.json"), s); err != nil {
		return err
	}
	m.settings = s
	if current := m.currentLocked(); current != nil && terminal(current.task.State) {
		m.tasks = map[string]*runState{}
	}
	return nil
}

func (m *Manager) resolved(c scenario.Config) scenario.Config {
	if c.Token == "" {
		c.Token = m.settings.Config.Token
	}
	return c
}

func (m *Manager) Create(c scenario.Config) (Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != "" || m.maintenance {
		return Task{}, errors.New("已有任务或环境检查正在进行")
	}
	c = m.resolved(c).Normalized()
	normalized, normalizeErr := m.audio.Normalize(c)
	if normalizeErr != nil {
		return Task{}, normalizeErr
	}
	c = normalized
	if err := c.Validate(true); err != nil {
		return Task{}, err
	}
	plan, err := scenario.Build(c)
	if err != nil {
		return Task{}, err
	}
	discovered := []NodeState{}
	if cluster, ok := m.transport.(interface{ Nodes() []NodeState }); ok {
		discovered = cluster.Nodes()
	}
	resolvedHosts := mergeDiscoveredHosts(m.settings.Hosts, discovered)
	hostIDs := map[string]bool{}
	for _, host := range resolvedHosts {
		if err := host.Validate(); err != nil {
			return Task{}, err
		}
		hostIDs[host.ID] = true
	}
	for i := range plan.Hosts {
		if !hostIDs[plan.Hosts[i].TargetNode()] {
			return Task{}, fmt.Errorf("执行节点 %s 未发现或未配置", plan.Hosts[i].TargetNode())
		}
		for _, host := range resolvedHosts {
			if host.ID == plan.Hosts[i].TargetNode() {
				plan.Hosts[i].NodeAddress = host.Address
			}
		}
		for _, node := range discovered {
			if c.Coverage() && node.ID == plan.Hosts[i].TargetNode() && node.ID != "A" && node.Version != wire.Version {
				return Task{}, fmt.Errorf("节点 %s 版本 %s 不支持本次频道分配，请先升级到 %s", node.ID, node.Version, wire.Version)
			}
			if node.ID == plan.Hosts[i].TargetNode() && node.Address != "" {
				plan.Hosts[i].NodeAddress = node.Address
			}
		}
	}
	var random [6]byte
	if _, err = rand.Read(random[:]); err != nil {
		return Task{}, err
	}
	id := "run-" + time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(random[:])
	ctx, cancel := context.WithCancel(context.Background())
	r := &runState{task: Task{ID: id, Name: c.Name, State: "preflight", CreatedAt: time.Now().UTC(), Config: c, Plan: plan}, ctx: ctx, cancel: cancel, notify: make(chan struct{}, 1), failure: make(chan error, 1), connected: map[string]bool{}, joined: map[string]bool{}, expected: map[string]bool{}, processes: map[string]ProcessState{}, audio: map[string]uint64{}, tx: map[string]uint64{}, rx: map[string]uint64{}, firstAudio: map[string]time.Time{}, sessions: map[string]wire.TaskSession{}}
	for _, hp := range plan.Hosts {
		for _, proc := range hp.Processes {
			r.processes[proc.ID] = ProcessState{ID: proc.ID, State: "pending", Expected: len(proc.Connections)}
			for _, u := range proc.Connections {
				r.expected[hp.Host+"/"+scenario.Key(u.Channel, u.UID)] = true
			}
		}
	}
	m.tasks = map[string]*runState{id: r}
	m.active = id
	hosts := append([]scenario.Host(nil), resolvedHosts...)
	go m.execute(r, hosts)
	return m.snapshotLocked(r), nil
}

func (m *Manager) execute(r *runState, hosts []scenario.Host) {
	finalState := "completed"
	var finalErr error
	defer func() {
		m.phase(r, "stopping")
		var wg sync.WaitGroup
		stopErrors := make(chan error, len(r.sessions))
		for _, s := range r.sessions {
			wg.Add(1)
			go func(s wire.TaskSession) {
				defer wg.Done()
				if err := s.Stop(); err != nil {
					stopErrors <- err
				}
			}(s)
		}
		wg.Wait()
		close(stopErrors)
		for err := range stopErrors {
			if finalErr == nil {
				finalErr = err
			}
			if finalState == "completed" {
				finalState = "failed"
			}
		}
		r.cancel()
		m.mu.Lock()
		defer m.mu.Unlock()
		now := time.Now().UTC()
		r.task.FinishedAt = &now
		r.task.State = finalState
		if finalErr != nil {
			r.task.Error = finalErr.Error()
		}
		m.active = ""
	}()
	fail := func(err error) {
		if r.ctx.Err() != nil {
			finalState = "stopped"
		} else {
			finalState = "failed"
			finalErr = err
		}
	}
	hostMap := map[string]scenario.Host{}
	for _, h := range hosts {
		hostMap[h.ID] = h
	}
	m.phase(r, "preflight")
	for _, hp := range r.task.Plan.Hosts {
		h := hostMap[hp.TargetNode()]
		req, prepareErr := m.audio.Request(r.task.ID, r.task.Config, hp, h)
		if prepareErr != nil {
			fail(prepareErr)
			return
		}
		if err := m.transport.Check(r.ctx, h, req); err != nil {
			fail(err)
			return
		}
	}
	start := func(hp scenario.HostPlan) error {
		h := hostMap[hp.TargetNode()]
		req, err := m.audio.Request(r.task.ID, r.task.Config, hp, h)
		if err != nil {
			return err
		}
		s, err := m.transport.Start(context.WithoutCancel(r.ctx), h, req, func(e worker.Event) { m.event(r, e) })
		if err != nil {
			return err
		}
		r.sessions[hp.Host] = s
		go func() {
			err := <-s.Done()
			if !terminalStateContext(r.ctx) {
				if err == nil {
					err = fmt.Errorf("%s Worker 提前结束", hp.Host)
				}
				select {
				case r.failure <- err:
				default:
				}
			}
		}()
		return nil
	}

	if r.task.Config.Membership() {
		m.phase(r, "joining_creators")
		creators := []string{}
		for _, hp := range r.task.Plan.Hosts {
			if hp.PublishingChannels > 0 {
				if err := start(hp); err != nil {
					fail(err)
					return
				}
				creators = append(creators, hp.Host)
			}
		}
		if err := m.waitReady(r, creators); err != nil {
			fail(err)
			return
		}
		m.phase(r, "joining_existing")
		for _, hp := range r.task.Plan.Hosts {
			if hp.PublishingChannels == 0 {
				if err := start(hp); err != nil {
					fail(err)
					return
				}
			}
		}
	} else if r.task.Config.Distribution == "distributed" || r.task.Config.Flexible() {
		m.phase(r, "joining_all")
		for _, hp := range r.task.Plan.Hosts {
			if err := start(hp); err != nil {
				fail(err)
				return
			}
		}
	} else {
		m.phase(r, "joining_a")
		if err := start(r.task.Plan.Hosts[0]); err != nil {
			fail(err)
			return
		}
		if err := m.waitReady(r, []string{"A"}); err != nil {
			fail(err)
			return
		}
		m.phase(r, "joining_bc")
		for _, hp := range r.task.Plan.Hosts[1:] {
			if err := start(hp); err != nil {
				fail(err)
				return
			}
		}
	}
	roles := make([]string, 0, len(r.task.Plan.Hosts))
	for _, hp := range r.task.Plan.Hosts {
		roles = append(roles, hp.Host)
	}
	if err := m.waitReady(r, roles); err != nil {
		fail(err)
		return
	}
	m.phase(r, "arming")
	epoch := time.Now().UTC().Add(3 * time.Second)
	for _, hp := range r.task.Plan.Hosts {
		if hp.HasPublishers() {
			if err := r.sessions[hp.Host].Send(worker.Command{Type: "begin", At: epoch}); err != nil {
				fail(err)
				return
			}
		}
	}
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for {
		m.mu.Lock()
		armed := true
		for _, hp := range r.task.Plan.Hosts {
			if hp.HasPublishers() && !r.armed[hp.Host].Equal(epoch) {
				armed = false
			}
		}
		m.mu.Unlock()
		if armed {
			break
		}
		select {
		case <-r.notify:
		case err := <-r.failure:
			fail(err)
			return
		case <-r.ctx.Done():
			fail(r.ctx.Err())
			return
		case <-deadline.C:
			fail(errors.New("发流节点未全部确认音频启动时间"))
			return
		}
	}
	m.phase(r, "running")
	timer := time.NewTimer(time.Until(epoch.Add(time.Duration(r.task.Config.DurationSec) * time.Second)))
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return
		case <-r.ctx.Done():
			fail(r.ctx.Err())
			return
		case err := <-r.failure:
			fail(err)
			return
		}
	}
}

func terminalStateContext(ctx context.Context) bool { return ctx.Err() != nil }

func (m *Manager) waitReady(r *runState, roles []string) error {
	timer := time.NewTimer(time.Duration(r.task.Config.ReadyTimeoutSec) * time.Second)
	defer timer.Stop()
	for {
		m.mu.Lock()
		ready := true
		if cluster, ok := m.transport.(interface{ Ready(string) bool }); ok {
			for _, role := range roles {
				for _, host := range r.task.Plan.Hosts {
					if host.Host == role && !cluster.Ready(host.TargetNode()) {
						ready = false
					}
				}
			}
		}
		for key := range r.expected {
			for _, role := range roles {
				if strings.HasPrefix(key, role+"/") && !r.connected[key] {
					ready = false
				}
			}
		}
		m.mu.Unlock()
		if ready {
			return nil
		}
		select {
		case <-r.notify:
		case err := <-r.failure:
			return err
		case <-r.ctx.Done():
			return r.ctx.Err()
		case <-timer.C:
			return fmt.Errorf("等待 %v 入频道超时，任务已停止；请检查执行机器与 RTC 连接参数", roles)
		}
	}
}

func (m *Manager) event(r *runState, e worker.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.eventLocked(r, e)
}

func (m *Manager) eventLocked(r *runState, e worker.Event) {
	if terminal(r.task.State) || e.Type == "log" {
		return
	}
	if e.Type == "snapshot" {
		prefix := e.Host + "/"
		for key := range r.connected {
			if strings.HasPrefix(key, prefix) {
				delete(r.connected, key)
			}
		}
		for key := range r.audio {
			if strings.HasPrefix(key, prefix) {
				delete(r.audio, key)
			}
		}
		r.tx[e.Host] = 0
		r.rx[e.Host] = 0
		for _, item := range e.Snapshot {
			m.eventLocked(r, item)
		}
		select {
		case r.notify <- struct{}{}:
		default:
		}
		return
	}
	key := e.Host + "/" + scenario.Key(e.Channel, e.UID)
	switch e.Type {
	case "connected", "disconnected":
		if r.expected[key] {
			r.connected[key] = e.Type == "connected"
			if e.Type == "connected" {
				r.joined[key] = true
			}
		}
	case "sdk_stats":
		state := r.processes[e.Process]
		state.SDK = e.SDK
		r.processes[e.Process] = state
		return
	case "process_started":
		p := r.processes[e.Process]
		p.PID = e.PID
		p.State = "running"
		r.processes[e.Process] = p
	case "process_exited":
		p := r.processes[e.Process]
		p.State = "exited"
		r.processes[e.Process] = p
		for _, h := range r.task.Plan.Hosts {
			for _, proc := range h.Processes {
				if proc.ID == e.Process {
					for _, u := range proc.Connections {
						r.connected[h.Host+"/"+scenario.Key(u.Channel, u.UID)] = false
					}
				}
			}
		}
	case "armed":
		if r.armed == nil {
			r.armed = map[string]time.Time{}
		}
		r.armed[e.Host] = e.At
		at := e.At
		if r.task.StartedAt == nil {
			r.task.StartedAt = &at
		}
	case "audio":
		if r.expected[key] && e.Source != nil && *e.Source >= 0 && *e.Source < 2 {
			r.audio[key] = e.Bytes
			sourceKey := fmt.Sprintf("%s/%d", e.Host, *e.Source)
			if _, ok := r.firstAudio[sourceKey]; !ok {
				r.firstAudio[sourceKey] = e.At
			}
		}
		select {
		case r.notify <- struct{}{}:
		default:
		}
		return
	case "traffic":
		r.tx[e.Host] += e.TX
		r.rx[e.Host] += e.RX
		return
	case "error":
		select {
		case r.failure <- errors.New(e.Host + ": " + e.Message):
		default:
		}
	}
	select {
	case r.notify <- struct{}{}:
	default:
	}
}

func (m *Manager) phase(r *runState, state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.task.State = state
}

func (m *Manager) snapshotLocked(r *runState) Task {
	t := r.task
	t.Config.Token = ""
	if r.connected == nil {
		return t
	}
	t.Machines = nil
	for _, hp := range t.Plan.Hosts {
		h := MachineState{ID: hp.Host, NodeID: hp.TargetNode(), NodeAddress: hp.NodeAddress, Expected: hp.Connections, Processes: []ProcessState{}, AudioTX: r.tx[hp.Host], AudioRX: r.rx[hp.Host]}
		for _, proc := range hp.Processes {
			p := r.processes[proc.ID]
			p.Connected = 0
			for _, u := range proc.Connections {
				key := hp.Host + "/" + scenario.Key(u.Channel, u.UID)
				if r.connected[key] {
					p.Connected++
					h.Connected++
				}
				if r.joined[key] {
					h.Joined++
				}
				if u.AudioSource >= 0 {
					h.AudioBytes[u.AudioSource] += r.audio[key]
				}
			}
			h.Processes = append(h.Processes, p)
		}
		for source := 0; source < 2; source++ {
			if at, ok := r.firstAudio[fmt.Sprintf("%s/%d", hp.Host, source)]; ok {
				at := at
				h.FirstAudio[source] = &at
			}
		}
		t.Machines = append(t.Machines, h)
	}
	return t
}

func (m *Manager) currentLocked() *runState {
	if m.active != "" {
		return m.tasks[m.active]
	}
	for _, run := range m.tasks {
		return run
	}
	return nil
}
func (m *Manager) CurrentID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if run := m.currentLocked(); run != nil {
		return run.task.ID
	}
	return ""
}
func (m *Manager) Current() (Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if run := m.currentLocked(); run != nil {
		return m.snapshotLocked(run), true
	}
	return Task{}, false
}

// Tasks is retained for API compatibility; it contains only the current in-memory run.
func (m *Manager) Tasks() []Task {
	if task, ok := m.Current(); ok {
		return []Task{task}
	}
	return []Task{}
}
func (m *Manager) Get(id string) (Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.tasks[id]
	if !ok {
		return Task{}, false
	}
	return m.snapshotLocked(r), true
}
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.tasks[id]
	if !ok {
		return errors.New("任务不存在")
	}
	if terminal(r.task.State) {
		return nil
	}
	r.cancel()
	return nil
}
func (m *Manager) Close() {
	m.mu.Lock()
	for _, r := range m.tasks {
		if r.cancel != nil {
			r.cancel()
		}
	}
	m.mu.Unlock()
}

func (m *Manager) Wait(ctx context.Context) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		idle := m.active == ""
		m.mu.Unlock()
		if idle {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func (m *Manager) HostAction(ctx context.Context, id string) (string, error) {
	m.mu.Lock()
	if m.active != "" || m.maintenance {
		m.mu.Unlock()
		return "", errors.New("任务或环境检查正在进行")
	}
	m.maintenance = true
	var host scenario.Host
	for _, h := range m.settings.Hosts {
		if h.ID == id {
			host = h
		}
	}
	c := m.settings.Config
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.maintenance = false; m.mu.Unlock() }()
	if err := host.Validate(); err != nil {
		return "", err
	}
	plan, err := scenario.Build(c)
	if err != nil {
		return "", err
	}
	var hp scenario.HostPlan
	for _, h := range plan.Hosts {
		if h.TargetNode() == id {
			hp = h
		}
	}
	if hp.Host == "" {
		for _, candidate := range plan.Hosts {
			if candidate.HasPublishers() {
				hp = candidate
				break
			}
		}
		hp.NodeID = id
	}
	request, err := m.audio.Request("environment-check", c, hp, host)
	if err != nil {
		return "", err
	}
	err = m.transport.Check(ctx, host, request)
	if err != nil {
		return "", err
	}
	return "节点连接、Native 和本机音频检查通过", nil
}

func mergeDiscoveredHosts(configured []scenario.Host, nodes []NodeState) []scenario.Host {
	out := append([]scenario.Host(nil), configured...)
	seen := map[string]bool{}
	for _, host := range out {
		seen[host.ID] = true
	}
	for _, node := range nodes {
		if !seen[node.ID] && scenario.ValidID(node.ID) && node.Address != "" {
			workDir := node.WorkDir
			if workDir == "" {
				workDir = "/home/bbt"
			}
			out = append(out, scenario.Host{ID: node.ID, Address: node.Address, WorkDir: workDir})
			seen[node.ID] = true
		}
	}
	return out
}

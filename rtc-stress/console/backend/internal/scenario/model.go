package scenario

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Host struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	WorkDir string `json:"workDir"`
}

func DefaultHosts() []Host {
	return []Host{{ID: "A", WorkDir: "/home/bbt"}, {ID: "B", WorkDir: "/home/bbt"}, {ID: "C", WorkDir: "/home/bbt"}}
}
func (h Host) Validate() error {
	if !ValidID(h.ID) {
		return errors.New("节点标识须为 1～32 位字母、数字、下划线或连字符")
	}
	if net.ParseIP(h.Address) == nil && !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*$`).MatchString(h.Address) {
		return fmt.Errorf("节点 %s 地址无效", h.ID)
	}
	if !filepath.IsAbs(h.WorkDir) || strings.ContainsAny(h.WorkDir, "\x00\n\r") {
		return errors.New("工作目录须为绝对路径")
	}
	return nil
}

type AudioSchedule struct {
	Mode     string `json:"mode"`
	OffsetMS int64  `json:"offsetMs"`
	WindowMS int64  `json:"windowMs"`
	GapMS    int64  `json:"gapMs"`
}

// Active controls material content. Inactive windows supply silent PCM to keep the Native clock running.
func (s AudioSchedule) Active(source int, elapsedMS int64) bool {
	if elapsedMS < 0 {
		return false
	}
	if s.Mode == "staggered" {
		return source == 0 || elapsedMS >= s.OffsetMS
	}
	if s.Mode != "alternating" || s.WindowMS <= 0 || s.GapMS < 0 {
		return false
	}
	slot := s.WindowMS + s.GapMS
	phase := elapsedMS % (2 * slot)
	if source == 0 {
		return phase < s.WindowMS
	}
	return phase >= slot && phase < slot+s.WindowMS
}

type RoleAllocation struct {
	JoiningChannels           *int   `json:"joiningChannels,omitempty"`
	ReceiversPerJoinedChannel *int   `json:"receiversPerJoinedChannel,omitempty"`
	PublishingChannels        *int   `json:"publishingChannels,omitempty"`
	PublishersPerChannel      *int   `json:"publishersPerChannel,omitempty"`
	ReceiversPerChannel       *int   `json:"receiversPerChannel,omitempty"`
	Role                      string `json:"role"`
	NodeID                    string `json:"nodeId"`
	ProcessCount              int    `json:"processCount"`
	MaxUIDsPerProcess         int    `json:"maxUidsPerProcess"`
}

type Config struct {
	AllocationPolicy      string           `json:"allocationPolicy,omitempty"`
	TotalUIDs             int              `json:"totalUids,omitempty"`
	UsersPerChannel       int              `json:"usersPerChannel,omitempty"`
	AudioInputMode        string           `json:"audioInputMode,omitempty"`
	AudioScenario         string           `json:"audioScenario,omitempty"`
	Distribution          string           `json:"distribution,omitempty"`
	PublisherChannels     [3]int           `json:"publisherChannels,omitempty"`
	Engine                string           `json:"engine"`
	ServiceLayout         string           `json:"serviceLayout"`
	ConnectionsPerService int              `json:"connectionsPerService"`
	Allocations           []RoleAllocation `json:"allocations"`
	AudioLabels           [2]string        `json:"audioLabels"`
	Name                  string           `json:"name"`
	Channels              int              `json:"channels"`
	ChannelPrefix         string           `json:"channelPrefix"`
	ChannelStart          int              `json:"channelStart"`
	ChannelsPerProcessA   int              `json:"channelsPerProcessA"`
	UsersPerProcessBC     int              `json:"usersPerProcessBC"`
	UIDs                  [5]uint64        `json:"uids"`
	Token                 string           `json:"token"`
	AP                    string           `json:"ap"`
	APCert                string           `json:"apCert"`
	VOSIP                 string           `json:"vosIp"`
	VOSPort               int              `json:"vosPort"`
	UseAUT                bool             `json:"useAut"`
	AudioFiles            [2]string        `json:"audioFiles"`
	SampleRate            int              `json:"sampleRate"`
	AudioChannels         int              `json:"audioChannels"`
	DurationSec           int              `json:"durationSec"`
	ReadyTimeoutSec       int              `json:"readyTimeoutSec"`
	LaunchIntervalMS      int              `json:"launchIntervalMs"`
	Schedule              AudioSchedule    `json:"schedule"`
}

func Defaults() Config {
	return Config{Engine: "go-sdk", ServiceLayout: "single", ConnectionsPerService: 3, Name: "多机音频压测", Channels: 200, ChannelPrefix: "stress_audio", ChannelStart: 1000, ChannelsPerProcessA: 15, UsersPerProcessBC: 15, UIDs: [5]uint64{10001, 20001, 30001, 40001, 50001}, AudioFiles: [2]string{"builtin:440", "builtin:660"}, SampleRate: 48000, AudioChannels: 2, DurationSec: 600, ReadyTimeoutSec: 600, LaunchIntervalMS: 100, Schedule: AudioSchedule{Mode: "staggered", OffsetMS: 5000, WindowMS: 5000, GapMS: 500}}
}

func (c Config) Normalized() Config {
	if c.Coverage() && !c.Membership() {
		c.AllocationPolicy = "coverage"
	}
	if c.AudioInputMode == "" {
		c.AudioInputMode = "pcm"
	}
	if c.AudioScenario == "" {
		c.AudioScenario = "chorus"
	}
	if c.Engine == "" {
		c.Engine = "native"
	}
	if c.ServiceLayout == "" {
		c.ServiceLayout = "single"
	}
	if c.ConnectionsPerService == 0 {
		c.ConnectionsPerService = 3
	}
	if len(c.Allocations) == 0 && !c.Coverage() {
		a := c.ChannelsPerProcessA
		b := c.UsersPerProcessBC
		if a < 1 {
			a = 15
		}
		if b < 1 {
			b = 15
		}
		c.Allocations = []RoleAllocation{{Role: "A", NodeID: "A", ProcessCount: (c.Channels + a - 1) / a, MaxUIDsPerProcess: a * 3}, {Role: "B", NodeID: "B", ProcessCount: (c.Channels + b - 1) / b, MaxUIDsPerProcess: b}, {Role: "C", NodeID: "C", ProcessCount: (c.Channels + b - 1) / b, MaxUIDsPerProcess: b}}
	}
	return c
}
func (c Config) Allocation(role string) RoleAllocation {
	for _, a := range c.Normalized().Allocations {
		if a.Role == role {
			return a
		}
	}
	return RoleAllocation{}
}

func (c Config) Validate(requireCredential bool) error {
	if c.AllocationPolicy != "" && c.AllocationPolicy != "coverage" && c.AllocationPolicy != "membership" {
		return errors.New("频道分配规则无效")
	}
	if c.AudioInputMode != "" && c.AudioInputMode != "pcm" && c.AudioInputMode != "opus" {
		return errors.New("音频输入模式无效")
	}
	if c.AudioInputMode == "opus" && c.Engine != "go-sdk" {
		return errors.New("Opus 直发需要 Go SDK 引擎")
	}
	if c.AudioScenario != "" && c.AudioScenario != "chorus" && c.AudioScenario != "standard" {
		return errors.New("SDK 音频模式无效")
	}
	if c.Distribution != "" && c.Distribution != "centralized" && c.Distribution != "distributed" {
		return errors.New("发流分配方式无效")
	}
	if c.Distribution == "distributed" && !c.Flexible() {
		if c.Engine != "go-sdk" {
			return errors.New("三机分摊发流需要 Go SDK 引擎")
		}
		counts := c.PublisherCounts()
		if counts[0] < 0 || counts[1] < 0 || counts[2] < 0 || counts[0]+counts[1]+counts[2] != c.Channels {
			return errors.New("三台机器的发流频道数之和须等于总频道数")
		}
	}

	if c.Engine == "go-sdk" && c.ServiceLayout == "multi" {
		return errors.New("当前 Go SDK 2.6.6 的底层 Service 为进程单例，请选择一个 Service、多个 Connection")
	}
	if c.Engine != "" && c.Engine != "native" && c.Engine != "go-sdk" {
		return errors.New("压测引擎无效")
	}
	if c.ServiceLayout != "" && c.ServiceLayout != "single" && c.ServiceLayout != "multi" {
		return errors.New("Service 布局无效")
	}
	if c.ConnectionsPerService != 0 && (c.ConnectionsPerService < 3 || c.ConnectionsPerService > 3000) {
		return errors.New("每 Service 的连接上限须为3～3000")
	}

	if c.Channels < 1 || c.Channels > 10000 {
		return errors.New("频道数须为 1～10000")
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,40}$`).MatchString(c.ChannelPrefix) {
		return errors.New("频道前缀限 1～40 位字母、数字、下划线和连字符")
	}
	if c.ChannelStart < 0 || c.ChannelStart > 1000000000-c.Channels {
		return errors.New("频道起始编号超出范围")
	}
	if len(c.Allocations) == 0 && (c.ChannelsPerProcessA < 1 || c.ChannelsPerProcessA > 100 || c.UsersPerProcessBC < 1 || c.UsersPerProcessBC > 100) {
		return errors.New("每进程承载量须为 1～100")
	}
	c = c.Normalized()
	if c.Membership() {
		if err := c.validateMembership(); err != nil {
			return err
		}
	} else if c.Coverage() {
		if err := c.validateCoverage(); err != nil {
			return err
		}
	} else if c.Flexible() {
		if err := c.validateFlexible(); err != nil {
			return err
		}
	} else {
		if len(c.Allocations) != 3 {
			return errors.New("请配置 A、B、C 三个任务角色")
		}
		roles := map[string]bool{}
		nodes := map[string]bool{}
		for _, a := range c.Allocations {
			if (a.Role != "A" && a.Role != "B" && a.Role != "C") || roles[a.Role] {
				return errors.New("任务角色 A/B/C 必须各配置一次")
			}
			roles[a.Role] = true
			if (a.NodeID != "A" && a.NodeID != "B" && a.NodeID != "C") || nodes[a.NodeID] {
				return errors.New("三个角色须选择三台不同的已发现机器")
			}
			nodes[a.NodeID] = true
			if a.ProcessCount < 1 || a.ProcessCount > c.Channels {
				return fmt.Errorf("角色 %s 的进程数须为 1～%d，保证每个进程至少承载一个频道", a.Role, c.Channels)
			}
			if a.MaxUIDsPerProcess < 1 || a.MaxUIDsPerProcess > 3000 {
				return fmt.Errorf("角色 %s 每进程 UID 上限须为 1～3000", a.Role)
			}
			if c.Distribution == "distributed" {
				count := c.PublisherCounts()[int(a.Role[0]-'A')]
				required := c.Channels + 2*count
				if a.ProcessCount*a.MaxUIDsPerProcess < required || a.ProcessCount*(a.MaxUIDsPerProcess/3) < count {
					return fmt.Errorf("分组 %s 容量不足，需要 %d UID，且 %d 个发流频道的三人组不能拆分", a.Role, required, count)
				}
				continue
			}
			perChannel := 1
			if a.Role == "A" {
				perChannel = 3
			}
			capacity := a.MaxUIDsPerProcess / perChannel
			if a.ProcessCount*capacity < c.Channels {
				return fmt.Errorf("角色 %s 容量不足：%d 个进程、每进程最多 %d UID，只能承载 %d 个频道，需要 %d 个", a.Role, a.ProcessCount, a.MaxUIDsPerProcess, a.ProcessCount*capacity, c.Channels)
			}
		}
	}
	seen := map[uint64]bool{}
	uidValues := c.UIDs[:]
	if c.Flexible() {
		uidValues = c.UIDs[:3]
	}
	for _, u := range uidValues {
		if u == 0 || u > 4294967295-uint64(c.Channels-1) || seen[u] {
			return errors.New("UID 基值须互不相同，范围为 1～4294967295")
		}
		seen[u] = true
	}
	if requireCredential && (strings.TrimSpace(c.Token) == "" || strings.TrimSpace(c.AP) == "" || strings.TrimSpace(c.APCert) == "") {
		return errors.New("请填写本次 RTC 环境的 Token/AppId、AP 和证书域名")
	}
	if strings.ContainsAny(c.Token+c.AP+c.APCert+c.VOSIP, "\x00\n\r") {
		return errors.New("连接配置不能含换行或空字符")
	}
	if (c.VOSIP == "") != (c.VOSPort == 0) || c.VOSPort < 0 || c.VOSPort > 65535 {
		return errors.New("VOS 地址和有效端口须同时填写或同时留空")
	}
	if c.SampleRate != 8000 && c.SampleRate != 16000 && c.SampleRate != 32000 && c.SampleRate != 44100 && c.SampleRate != 48000 {
		return errors.New("不支持的采样率")
	}
	if c.AudioChannels != 1 && c.AudioChannels != 2 {
		return errors.New("音频须为单声道或双声道")
	}
	if c.DurationSec < 1 || c.DurationSec > 86400 || c.ReadyTimeoutSec < 10 || c.ReadyTimeoutSec > 3600 || c.LaunchIntervalMS < 0 || c.LaunchIntervalMS > 10000 {
		return errors.New("运行时长、入频道超时或启动间隔超出范围")
	}
	if c.Schedule.Mode != "alternating" && c.Schedule.Mode != "staggered" {
		return errors.New("请选择交替发送或延迟后持续发送")
	}
	if c.Schedule.OffsetMS < 0 || c.Schedule.OffsetMS > 3600000 || c.Schedule.WindowMS < 100 || c.Schedule.WindowMS > 3600000 || c.Schedule.GapMS < 0 || c.Schedule.GapMS > 60000 {
		return errors.New("音频时间配置超出范围")
	}
	if c.Schedule.Mode == "staggered" && c.Schedule.OffsetMS >= int64(c.DurationSec)*1000 {
		return errors.New("第二路延迟必须小于任务运行时长")
	}
	if c.AudioFiles[0] == "" || c.AudioFiles[1] == "" || c.AudioFiles[0] == c.AudioFiles[1] {
		return errors.New("请选择两份不同音频")
	}
	for _, f := range c.AudioFiles {
		if strings.ContainsAny(f, "\x00\n\r") || len(f) > 4096 {
			return errors.New("音频路径无效")
		}
		if strings.HasPrefix(f, "builtin:") {
			hz, e := strconv.Atoi(strings.TrimPrefix(f, "builtin:"))
			if e != nil || hz < 100 || hz > 4000 {
				return errors.New("内置音频频率须为 100～4000 Hz")
			}
		}
	}
	return nil
}

type Connection struct {
	Channel     string `json:"channel"`
	UID         uint64 `json:"uid"`
	Role        int    `json:"role"`
	AudioSource int    `json:"audioSource"` // 0/1 are publishers; -1 is a receive-only connection.
}

type Process struct {
	Services     int          `json:"services,omitempty"`
	ID           string       `json:"id"`
	Host         string       `json:"host"`
	ChannelNames []string     `json:"channelNames"`
	Connections  []Connection `json:"connections"`
}

type HostPlan struct {
	JoiningChannels           int       `json:"joiningChannels,omitempty"`
	ReceiversPerJoinedChannel int       `json:"receiversPerJoinedChannel,omitempty"`
	PublishersPerChannel      int       `json:"publishersPerChannel,omitempty"`
	ReceiversPerChannel       int       `json:"receiversPerChannel,omitempty"`
	AudioPublishers           int       `json:"audioPublishers,omitempty"`
	PublishingChannels        int       `json:"publishingChannels,omitempty"`
	NodeID                    string    `json:"nodeId"`
	NodeAddress               string    `json:"nodeAddress"`
	MaxUIDsPerProcess         int       `json:"maxUidsPerProcess"`
	Host                      string    `json:"host"`
	Channels                  int       `json:"channels"`
	Connections               int       `json:"connections"`
	Processes                 []Process `json:"processes"`
}

type Plan struct {
	ChannelAssignments []ChannelAssignment `json:"channelAssignments,omitempty"`
	Coverage           *CoverageSummary    `json:"coverage,omitempty"`
	Hosts              []HostPlan          `json:"hosts"`
	TotalProcesses     int                 `json:"totalProcesses"`
	TotalConnections   int                 `json:"totalConnections"`
	AudioPublishers    int                 `json:"audioPublishers"`
}

func (p HostPlan) TargetNode() string {
	if p.NodeID != "" {
		return p.NodeID
	}
	return p.Host
}

func Build(c Config) (Plan, error) {
	if err := c.Validate(false); err != nil {
		return Plan{}, err
	}
	c = c.Normalized()
	if c.Membership() {
		return buildMembership(c)
	}
	if c.Coverage() {
		return buildCoverage(c)
	}
	if c.Flexible() {
		return buildFlexible(c)
	}
	if c.Distribution == "distributed" {
		return buildDistributed(c)
	}
	plan := Plan{AudioPublishers: c.Channels * 2}
	for _, role := range []string{"A", "B", "C"} {
		a := c.Allocation(role)
		host := HostPlan{Host: role, NodeID: a.NodeID, Channels: c.Channels, MaxUIDsPerProcess: a.MaxUIDsPerProcess}
		first := 0
		for index := 0; index < a.ProcessCount; index++ {
			left := a.ProcessCount - index
			take := (c.Channels - first + left - 1) / left
			proc := Process{ID: fmt.Sprintf("%s-%03d", role, index+1), Host: role}
			for n := first; n < first+take; n++ {
				channel := fmt.Sprintf("%s_%d", c.ChannelPrefix, c.ChannelStart+n)
				proc.ChannelNames = append(proc.ChannelNames, channel)
				if role == "A" {
					for u := 0; u < 3; u++ {
						source := -1
						// Match stress_jktuchuan.py for mixed publisher/receiver
						// processes. Receive-only A users still mute both media;
						// role=1 skips the Native's publisher initialization.
						nativeRole := 0
						if u < 2 {
							source = u
							nativeRole = 0
						}
						proc.Connections = append(proc.Connections, Connection{channel, c.UIDs[u] + uint64(n), nativeRole, source})
					}
				} else {
					uidIndex := 3
					if role == "C" {
						uidIndex = 4
					}
					proc.Connections = append(proc.Connections, Connection{channel, c.UIDs[uidIndex] + uint64(n), 1, -1})
				}
			}
			first += take
			host.Connections += len(proc.Connections)
			if c.Engine == "go-sdk" {
				proc.Services = len(ServiceGroups(c, proc))
			}
			host.Processes = append(host.Processes, proc)
		}
		if role == "A" {
			host.AudioPublishers = c.Channels * 2
			host.PublishingChannels = c.Channels
		}
		plan.TotalProcesses += len(host.Processes)
		plan.TotalConnections += host.Connections
		plan.Hosts = append(plan.Hosts, host)
	}
	return plan, nil
}

func NativeArgs(c Config, p Process, pipes map[string]string) []string {
	args := []string{"--mode=0", "--hold=999999999", fmt.Sprintf("--audio-rate=%d", c.SampleRate), fmt.Sprintf("--audio-channels=%d", c.AudioChannels)}
	for i, u := range p.Connections {
		if i > 0 {
			args = append(args, "-O")
		}
		mute := "1"
		if u.AudioSource >= 0 {
			mute = "0"
		}
		args = append(args, "--token="+c.Token, "--cname="+u.Channel, fmt.Sprintf("--uid=%d", u.UID), fmt.Sprintf("--role=%d", u.Role), "--ap="+c.AP, "--ap_cert="+c.APCert, "--a-sub-flag=1", "--v-sub-flag=0", "--mute-audio="+mute, "--mute-video=1", "--dual=0", fmt.Sprintf("--use-aut=%d", boolInt(c.UseAUT)))
		if c.VOSIP != "" {
			args = append(args, "--ip="+c.VOSIP, fmt.Sprintf("--port=%d", c.VOSPort))
		}
		if u.AudioSource >= 0 {
			args = append(args, "--audio-input="+pipes[Key(u.Channel, u.UID)])
		}
	}
	return args
}

func Key(channel string, uid uint64) string { return channel + "/" + strconv.FormatUint(uid, 10) }
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ServiceGroups preserves complete A channels when grouping multiple Services.
func ServiceGroups(config Config, process Process) [][]Connection {
	config = config.Normalized()
	if len(process.Connections) == 0 {
		return nil
	}
	if config.ServiceLayout != "multi" {
		return [][]Connection{process.Connections}
	}
	size := config.ConnectionsPerService
	if process.Host == "A" {
		size = size / 3 * 3
	}
	if size < 1 {
		size = 3
	}
	groups := make([][]Connection, 0, (len(process.Connections)+size-1)/size)
	for first := 0; first < len(process.Connections); first += size {
		last := first + size
		if last > len(process.Connections) {
			last = len(process.Connections)
		}
		groups = append(groups, process.Connections[first:last])
	}
	return groups
}

// PublisherCounts assigns a small extra share to the Agent nodes by default.
func (c Config) PublisherCounts() [3]int {
	if c.PublisherChannels != [3]int{} {
		return c.PublisherChannels
	}
	a := c.Channels * 3 / 10
	b := (c.Channels - a + 1) / 2
	return [3]int{a, b, c.Channels - a - b}
}
func (h HostPlan) HasPublishers() bool {
	for _, p := range h.Processes {
		for _, u := range p.Connections {
			if u.AudioSource >= 0 {
				return true
			}
		}
	}
	return false
}

func buildDistributed(c Config) (Plan, error) {
	plan := Plan{AudioPublishers: c.Channels * 2}
	counts := c.PublisherCounts()
	for node, role := range []string{"A", "B", "C"} {
		allocation := c.Allocation(role)
		host := HostPlan{Host: role, NodeID: allocation.NodeID, Channels: c.Channels, MaxUIDsPerProcess: allocation.MaxUIDsPerProcess, PublishingChannels: counts[node], AudioPublishers: 2 * counts[node]}
		for i := 0; i < allocation.ProcessCount; i++ {
			host.Processes = append(host.Processes, Process{ID: fmt.Sprintf("%s-%03d", role, i+1), Host: role, Services: 1})
		}
		var publishers, receivers [][]Connection
		for channelIndex := 0; channelIndex < c.Channels; channelIndex++ {
			owner := 0
			if channelIndex >= counts[0] {
				owner = 1
			}
			if channelIndex >= counts[0]+counts[1] {
				owner = 2
			}
			channel := fmt.Sprintf("%s_%d", c.ChannelPrefix, c.ChannelStart+channelIndex)
			if node == owner {
				group := []Connection{}
				for slot := 0; slot < 3; slot++ {
					source := -1
					if slot < 2 {
						source = slot
					}
					group = append(group, Connection{channel, c.UIDs[slot] + uint64(channelIndex), 0, source})
				}
				publishers = append(publishers, group)
			} else {
				slot := 3
				// The lower-index non-publishing node receives slot 3, the other slot 4.
				for other := 0; other < node; other++ {
					if other != owner {
						slot = 4
					}
				}
				receivers = append(receivers, []Connection{{channel, c.UIDs[slot] + uint64(channelIndex), 1, -1}})
			}
		}
		for _, groups := range [][][]Connection{publishers, receivers} {
			for _, group := range groups {
				target := -1
				for index, p := range host.Processes {
					if len(p.Connections)+len(group) <= allocation.MaxUIDsPerProcess && (target < 0 || len(p.Connections) < len(host.Processes[target].Connections)) {
						target = index
					}
				}
				if target < 0 {
					return Plan{}, fmt.Errorf("分组 %s 无法在 UID 上限内放置完整频道用户组", role)
				}
				p := &host.Processes[target]
				p.Connections = append(p.Connections, group...)
				p.ChannelNames = append(p.ChannelNames, group[0].Channel)
			}
		}
		for _, p := range host.Processes {
			host.Connections += len(p.Connections)
		}
		plan.TotalConnections += host.Connections
		plan.TotalProcesses += len(host.Processes)
		plan.Hosts = append(plan.Hosts, host)
	}
	return plan, nil
}

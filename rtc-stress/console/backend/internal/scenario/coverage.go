package scenario

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type ChannelAssignment struct {
	Members         []ChannelMember `json:"members,omitempty"`
	Channel         string          `json:"channel"`
	CreatorRole     string          `json:"creatorRole"`
	CreatorNode     string          `json:"creatorNode"`
	ReceiverRole    string          `json:"receiverRole,omitempty"`
	ReceiverNode    string          `json:"receiverNode,omitempty"`
	Publishers      int             `json:"publishers"`
	LocalReceivers  int             `json:"localReceivers"`
	JoinedReceivers int             `json:"joinedReceivers"`
	TotalUIDs       int             `json:"totalUids"`
}
type CoverageSummary struct {
	JoinMemberships int  `json:"joinMemberships,omitempty"`
	Channels        int  `json:"channels"`
	ExtraChannels   int  `json:"extraChannels"`
	TotalUIDs       int  `json:"totalUids"`
	Verified        bool `json:"verified"`
}

func (c Config) Coverage() bool {
	if c.AllocationPolicy == "coverage" || c.Membership() || c.TotalUIDs != 0 || c.UsersPerChannel != 0 {
		return true
	}
	for _, row := range c.Allocations {
		if row.JoiningChannels != nil || row.ReceiversPerJoinedChannel != nil {
			return true
		}
	}
	return false
}
func (r RoleAllocation) CoverageUIDs() int {
	return count(r.PublishingChannels)*(r.PublisherCount()+count(r.ReceiversPerChannel)) + count(r.JoiningChannels)*count(r.ReceiversPerJoinedChannel)
}

type flowEdge struct{ to, reverse, capacity int }
type quotaFlow struct {
	edges       [][]flowEdge
	level, next []int
}

func newQuotaFlow(n int) *quotaFlow {
	return &quotaFlow{edges: make([][]flowEdge, n), level: make([]int, n), next: make([]int, n)}
}
func (f *quotaFlow) add(from, to, capacity int) int {
	index := len(f.edges[from])
	reverse := len(f.edges[to])
	f.edges[from] = append(f.edges[from], flowEdge{to, reverse, capacity})
	f.edges[to] = append(f.edges[to], flowEdge{from, index, 0})
	return index
}
func (f *quotaFlow) push(node, sink, capacity int) int {
	if node == sink {
		return capacity
	}
	for f.next[node] < len(f.edges[node]) {
		index := f.next[node]
		edge := f.edges[node][index]
		if edge.capacity > 0 && f.level[edge.to] == f.level[node]+1 {
			sent := f.push(edge.to, sink, min(capacity, edge.capacity))
			if sent > 0 {
				f.edges[node][index].capacity -= sent
				f.edges[edge.to][edge.reverse].capacity += sent
				return sent
			}
		}
		f.next[node]++
	}
	return 0
}
func (f *quotaFlow) solve(source, sink int) int {
	total := 0
	for {
		for i := range f.level {
			f.level[i] = -1
			f.next[i] = 0
		}
		f.level[source] = 0
		queue := []int{source}
		for head := 0; head < len(queue); head++ {
			node := queue[head]
			for _, edge := range f.edges[node] {
				if edge.capacity > 0 && f.level[edge.to] < 0 {
					f.level[edge.to] = f.level[node] + 1
					queue = append(queue, edge.to)
				}
			}
		}
		if f.level[sink] < 0 {
			return total
		}
		for {
			sent := f.push(source, sink, 1<<30)
			if sent == 0 {
				break
			}
			total += sent
		}
	}
}

type assignmentEdge struct{ row, index, capacity int }

func coverageLayout(c Config) ([]ChannelAssignment, error) {
	if c.UsersPerChannel < 1 || c.UsersPerChannel > 300 || c.TotalUIDs < 1 || c.TotalUIDs != c.Channels*c.UsersPerChannel {
		return nil, errors.New("先决条件不一致：总 PCU 必须等于总频道数 × 每频道 UID 数（每频道 1～300 UID）")
	}
	if len(c.Allocations) < 1 || len(c.Allocations) > 128 {
		return nil, errors.New("请配置 1～128 台执行机器")
	}
	roles, nodes := map[string]bool{}, map[string]bool{}
	created, joined, needed, uidTotal := 0, 0, 0, 0
	for _, row := range c.Allocations {
		if !ValidID(row.Role) || roles[row.Role] || !ValidID(row.NodeID) || nodes[row.NodeID] {
			return nil, errors.New("每个分组须选择不同机器，分组和机器标识不能重复")
		}
		roles[row.Role] = true
		nodes[row.NodeID] = true
		if row.PublishingChannels == nil || row.PublishersPerChannel == nil || row.ReceiversPerChannel == nil || row.JoiningChannels == nil || row.ReceiversPerJoinedChannel == nil {
			return nil, fmt.Errorf("分组 %s 须完整填写创建、额外加入频道及各自 UID 配额", row.Role)
		}
		owned, extra := count(row.PublishingChannels), count(row.JoiningChannels)
		send, local, remote := row.PublisherCount(), count(row.ReceiversPerChannel), count(row.ReceiversPerJoinedChannel)
		if owned < 0 || owned > c.Channels || extra < 0 || extra > c.Channels-owned || send < 0 || send > 100 || local < 0 || local > 100 || remote < 0 || remote > 100 {
			return nil, fmt.Errorf("分组 %s 配额超出范围：创建/额外加入不能超过频道数，UID 人数须为 0～100", row.Role)
		}
		if owned > 0 && (send+local < 1 || send+local > c.UsersPerChannel) {
			return nil, fmt.Errorf("分组 %s 创建频道的发流＋收流 UID 须为 1～%d", row.Role, c.UsersPerChannel)
		}
		if extra > 0 && remote == 0 {
			return nil, fmt.Errorf("分组 %s 额外加入频道时，每频道至少配置 1 个收流 UID", row.Role)
		}
		required := row.CoverageUIDs()
		if required == 0 {
			return nil, fmt.Errorf("分组 %s 未分配任务，请配置配额或移除该分组", row.Role)
		}
		groups := owned + extra*remote
		if row.ProcessCount < 1 || row.ProcessCount > groups || row.MaxUIDsPerProcess < 1 || row.MaxUIDsPerProcess > 3000 {
			return nil, fmt.Errorf("分组 %s 进程须为 1～%d，每进程 UID 上限须为 1～3000", row.Role, groups)
		}
		if row.ProcessCount*row.MaxUIDsPerProcess < required || (owned > 0 && row.ProcessCount*(row.MaxUIDsPerProcess/(send+local)) < owned) {
			return nil, fmt.Errorf("分组 %s 容量不足，需要 %d UID，创建频道的本机用户须放在同一进程", row.Role, required)
		}
		created += owned
		joined += extra
		uidTotal += required
		if send+local < c.UsersPerChannel {
			needed += owned
		}
	}
	if created != c.Channels {
		return nil, fmt.Errorf("创建频道合计须为 %d，当前为 %d", c.Channels, created)
	}
	if joined != needed {
		return nil, fmt.Errorf("额外加入频道合计须为 %d，当前为 %d；每个需补足的频道只能分给一台额外接收机器", needed, joined)
	}
	if uidTotal != c.TotalUIDs {
		return nil, fmt.Errorf("UID 配额合计须为 %d，当前为 %d，请调整各机器人数和频道配额", c.TotalUIDs, uidTotal)
	}
	// Match channel-owner groups to receiver quotas. Capacity one per channel is
	// represented by the owner's group count, so construction is linear in channels.
	n := len(c.Allocations)
	source, sink := 2*n, 2*n+1
	flow := newQuotaFlow(2*n + 2)
	edges := make([][]assignmentEdge, n)
	for i, owner := range c.Allocations {
		owned := count(owner.PublishingChannels)
		missing := c.UsersPerChannel - owner.PublisherCount() - count(owner.ReceiversPerChannel)
		if owned == 0 || missing == 0 {
			continue
		}
		flow.add(source, i, owned)
		for j, receiver := range c.Allocations {
			if i == j || count(receiver.JoiningChannels) == 0 || count(receiver.ReceiversPerJoinedChannel) != missing {
				continue
			}
			capacity := min(owned, count(receiver.JoiningChannels))
			index := flow.add(i, n+j, capacity)
			edges[i] = append(edges[i], assignmentEdge{j, index, capacity})
		}
		if len(edges[i]) == 0 {
			return nil, fmt.Errorf("分组 %s 创建的频道每个还缺 %d UID，没有人数匹配的额外接收机器", owner.Role, missing)
		}
	}
	for j, row := range c.Allocations {
		flow.add(n+j, sink, count(row.JoiningChannels))
	}
	if matched := flow.solve(source, sink); matched != needed {
		return nil, fmt.Errorf("只能无重复覆盖 %d/%d 个需补足的频道；请调整额外加入频道数、收流人数及创建机器", matched, needed)
	}
	layout := make([]ChannelAssignment, 0, c.Channels)
	offset := 0
	for i, owner := range c.Allocations {
		owned := count(owner.PublishingChannels)
		for index := 0; index < owned; index++ {
			layout = append(layout, ChannelAssignment{Channel: fmt.Sprintf("%s_%d", c.ChannelPrefix, c.ChannelStart+offset+index), CreatorRole: owner.Role, CreatorNode: owner.NodeID, Publishers: owner.PublisherCount(), LocalReceivers: count(owner.ReceiversPerChannel), TotalUIDs: owner.PublisherCount() + count(owner.ReceiversPerChannel)})
		}
		next := offset
		for _, link := range edges[i] {
			used := link.capacity - flow.edges[i][link.index].capacity
			for assigned := 0; assigned < used; assigned++ {
				entry := &layout[next]
				receiver := c.Allocations[link.row]
				entry.ReceiverRole = receiver.Role
				entry.ReceiverNode = receiver.NodeID
				entry.JoinedReceivers = count(receiver.ReceiversPerJoinedChannel)
				entry.TotalUIDs += entry.JoinedReceivers
				next++
			}
		}
		offset += owned
	}
	return layout, nil
}

func (c Config) validateCoverage() error {
	layout, err := coverageLayout(c)
	if err != nil {
		return err
	}
	for index, entry := range layout {
		receiverCount := entry.LocalReceivers + entry.JoinedReceivers
		first := c.UIDs[2] + uint64(index)*uint64(c.UsersPerChannel)
		if receiverCount > 0 && (first == 0 || first+uint64(receiverCount)-1 > 4294967295) {
			return errors.New("收流 UID 范围超出 1～4294967295")
		}
		seen := map[uint64]bool{}
		for slot := 0; slot < entry.Publishers; slot++ {
			uid := publisherUID(c, index, slot)
			if uid == 0 || uid > 4294967295 || seen[uid] || (receiverCount > 0 && uid >= first && uid < first+uint64(receiverCount)) {
				return errors.New("同频道 UID 冲突或超出 uint32 范围，请调整 UID 基值")
			}
			seen[uid] = true
		}
	}
	return nil
}

func buildCoverage(c Config) (Plan, error) {
	layout, err := coverageLayout(c)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{ChannelAssignments: layout}
	hostIndex := map[string]int{}
	for _, row := range c.Allocations {
		host := HostPlan{Host: row.Role, NodeID: row.NodeID, PublishingChannels: count(row.PublishingChannels), JoiningChannels: count(row.JoiningChannels), PublishersPerChannel: row.PublisherCount(), ReceiversPerChannel: count(row.ReceiversPerChannel), ReceiversPerJoinedChannel: count(row.ReceiversPerJoinedChannel), Channels: count(row.PublishingChannels) + count(row.JoiningChannels), MaxUIDsPerProcess: row.MaxUIDsPerProcess, AudioPublishers: count(row.PublishingChannels) * row.PublisherCount()}
		for index := 0; index < row.ProcessCount; index++ {
			services := 0
			if c.Engine == "go-sdk" {
				services = 1
			}
			host.Processes = append(host.Processes, Process{ID: fmt.Sprintf("%s-%03d", row.Role, index+1), Host: row.Role, Services: services})
		}
		hostIndex[row.Role] = len(plan.Hosts)
		plan.Hosts = append(plan.Hosts, host)
	}
	place := func(role string, group []Connection) error {
		host := &plan.Hosts[hostIndex[role]]
		target := -1
		for index, process := range host.Processes {
			if len(process.Connections)+len(group) <= host.MaxUIDsPerProcess && (target < 0 || len(process.Connections) < len(host.Processes[target].Connections)) {
				target = index
			}
		}
		if target < 0 {
			return fmt.Errorf("分组 %s 无法在进程容量内放置 UID", role)
		}
		process := &host.Processes[target]
		process.Connections = append(process.Connections, group...)
		found := false
		for _, name := range process.ChannelNames {
			if name == group[0].Channel {
				found = true
				break
			}
		}
		if !found {
			process.ChannelNames = append(process.ChannelNames, group[0].Channel)
		}
		host.Connections += len(group)
		return nil
	}
	// Place complete creator groups first. Extra receive-only UIDs can fill every
	// remaining slot, without imposing an unnecessary channel-group packing limit.
	for index, entry := range layout {
		group := make([]Connection, 0, entry.Publishers+entry.LocalReceivers)
		for slot := 0; slot < entry.Publishers; slot++ {
			group = append(group, Connection{entry.Channel, publisherUID(c, index, slot), 0, slot % 2})
		}
		for slot := 0; slot < entry.LocalReceivers; slot++ {
			group = append(group, Connection{entry.Channel, c.UIDs[2] + uint64(index)*uint64(c.UsersPerChannel) + uint64(slot), 1, -1})
		}
		if err := place(entry.CreatorRole, group); err != nil {
			return Plan{}, err
		}
	}
	for index, entry := range layout {
		for slot := 0; slot < entry.JoinedReceivers; slot++ {
			if err := place(entry.ReceiverRole, []Connection{{entry.Channel, c.UIDs[2] + uint64(index)*uint64(c.UsersPerChannel) + uint64(entry.LocalReceivers+slot), 1, -1}}); err != nil {
				return Plan{}, err
			}
		}
	}
	for _, host := range plan.Hosts {
		for _, process := range host.Processes {
			if len(process.Connections) == 0 {
				return Plan{}, fmt.Errorf("分组 %s 存在空进程，请减少进程数", host.Host)
			}
		}
		plan.TotalConnections += host.Connections
		plan.TotalProcesses += len(host.Processes)
		plan.AudioPublishers += host.AudioPublishers
	}
	// Audit the materialized result as a separate gate before it can be dispatched.
	if err := auditCoverage(c, plan); err != nil {
		return Plan{}, err
	}
	extra := 0
	for _, entry := range layout {
		if entry.ReceiverRole != "" {
			extra++
		}
	}
	plan.Coverage = &CoverageSummary{Channels: len(layout), ExtraChannels: extra, TotalUIDs: plan.TotalConnections, Verified: true}
	return plan, nil
}
func auditCoverage(c Config, p Plan) error {
	users := map[string]map[uint64]bool{}
	machines := map[string]map[string]bool{}
	for _, host := range p.Hosts {
		for _, process := range host.Processes {
			for _, connection := range process.Connections {
				if users[connection.Channel] == nil {
					users[connection.Channel] = map[uint64]bool{}
					machines[connection.Channel] = map[string]bool{}
				}
				if users[connection.Channel][connection.UID] {
					return errors.New("规划出现重复 UID，已阻止任务下发")
				}
				users[connection.Channel][connection.UID] = true
				machines[connection.Channel][host.Host] = true
			}
		}
	}
	if len(users) != c.Channels || len(p.ChannelAssignments) != c.Channels || p.TotalConnections != c.TotalUIDs {
		return errors.New("规划频道或 PCU 总数不一致，已阻止任务下发")
	}
	assigned := map[string]bool{}
	for _, entry := range p.ChannelAssignments {
		if assigned[entry.Channel] || len(users[entry.Channel]) != c.UsersPerChannel || entry.TotalUIDs != c.UsersPerChannel || entry.CreatorRole == entry.ReceiverRole {
			return errors.New("频道分配存在重复或人数缺口，已阻止任务下发")
		}
		assigned[entry.Channel] = true
		want := 1
		if entry.ReceiverRole != "" {
			want = 2
		}
		if len(machines[entry.Channel]) != want || !machines[entry.Channel][entry.CreatorRole] || (want == 2 && !machines[entry.Channel][entry.ReceiverRole]) {
			return errors.New("频道机器归属不一致，已阻止任务下发")
		}
	}
	return nil
}

// AudioPeers returns the actual publisher UIDs, including customized counts and
// multiple publishers sharing one of the two configured audio sources.
func (c Config) AudioPeers(channel string, localUID uint64) ([]Connection, error) {
	if c.Membership() {
		index, err := c.AudioPeerIndex([]string{channel})
		if err != nil {
			return nil, err
		}
		peers := []Connection{}
		for _, peer := range index[channel] {
			if peer.UID != localUID {
				peers = append(peers, peer)
			}
		}
		return peers, nil
	}
	if !strings.HasPrefix(channel, c.ChannelPrefix+"_") {
		return nil, errors.New("频道前缀不属于当前任务")
	}
	number, err := strconv.Atoi(strings.TrimPrefix(channel, c.ChannelPrefix+"_"))
	if err != nil {
		return nil, err
	}
	index := number - c.ChannelStart
	if index < 0 || index >= c.Channels {
		return nil, errors.New("频道不属于当前任务")
	}
	publishers := 2
	if c.Coverage() || c.Flexible() {
		offset := 0
		found := false
		for _, row := range c.Allocations {
			end := offset + count(row.PublishingChannels)
			if index >= offset && index < end {
				publishers = row.PublisherCount()
				found = true
				break
			}
			offset = end
		}
		if !found {
			return nil, errors.New("频道未分配创建机器")
		}
	}
	peers := []Connection{}
	for slot := 0; slot < publishers; slot++ {
		uid := publisherUID(c, index, slot)
		if uid != localUID {
			peers = append(peers, Connection{channel, uid, 0, slot % 2})
		}
	}
	return peers, nil
}

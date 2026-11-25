package scenario

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ChannelMember struct {
	Role        string `json:"role"`
	Node        string `json:"node"`
	Publishers  int    `json:"publishers"`
	Receivers   int    `json:"receivers"`
	OwnsChannel bool   `json:"ownsChannel"`
}

func (c Config) Membership() bool { return c.AllocationPolicy == "membership" }
func (r RoleAllocation) MembershipUIDs() int {
	return (count(r.PublishingChannels) + count(r.JoiningChannels)) * (r.PublisherCount() + count(r.ReceiversPerChannel))
}

type membershipState struct {
	caps, remaining []int
	chosen          [][]bool
}

func (s *membershipState) clone() *membershipState {
	next := &membershipState{caps: append([]int(nil), s.caps...), remaining: append([]int(nil), s.remaining...), chosen: make([][]bool, len(s.chosen))}
	for i := range s.chosen {
		next.chosen[i] = append([]bool(nil), s.chosen[i]...)
	}
	return next
}

type membershipGroup struct {
	owner, capacity int
	channels        []int
}

func membershipGroups(owners, caps []int) []membershipGroup {
	groups := []membershipGroup{}
	index := map[[2]int]int{}
	for channel, capacity := range caps {
		if capacity == 0 {
			continue
		}
		key := [2]int{owners[channel], capacity}
		target, found := index[key]
		if !found {
			target = len(groups)
			index[key] = target
			groups = append(groups, membershipGroup{owner: owners[channel], capacity: capacity})
		}
		groups[target].channels = append(groups[target].channels, channel)
	}
	return groups
}

type membershipSolver struct {
	owners, weights []int
	dead            map[string]bool
	steps           int
	deadline        time.Time
	err             error
}

func (q *membershipSolver) budget() bool {
	q.steps++
	if q.steps > 100000 || (q.steps%128 == 0 && time.Now().After(q.deadline)) {
		q.err = errors.New("该人数与频道组合的计算量超过限制，请缩小规模或调整配额后重试")
		return false
	}
	return true
}
func (q *membershipSolver) apply(s *membershipState, row, channel int) {
	s.chosen[row][channel] = true
	s.caps[channel] -= q.weights[row]
	s.remaining[row]--
}
func (q *membershipSolver) candidates(s *membershipState, row int) []int {
	result := []int{}
	for channel, capacity := range s.caps {
		if !s.chosen[row][channel] && capacity >= q.weights[row] {
			result = append(result, channel)
		}
	}
	return result
}
func (q *membershipSolver) key(s *membershipState) string {
	var b strings.Builder
	for _, n := range s.remaining {
		b.WriteString(strconv.Itoa(n))
		b.WriteByte(',')
	}
	b.WriteByte('|')
	groups := membershipGroups(q.owners, s.caps)
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].owner != groups[j].owner {
			return groups[i].owner < groups[j].owner
		}
		return groups[i].capacity < groups[j].capacity
	})
	for _, group := range groups {
		fmt.Fprintf(&b, "%d:%d:%d;", group.owner, group.capacity, len(group.channels))
	}
	return b.String()
}
func (q *membershipSolver) uniform(s *membershipState, weight int) *membershipState {
	groups := membershipGroups(q.owners, s.caps)
	n := len(s.remaining)
	source, sink := len(groups)+n, len(groups)+n+1
	flow := newQuotaFlow(sink + 1)
	links := make([][]assignmentEdge, len(groups))
	needed := 0
	for i, group := range groups {
		if group.capacity%weight != 0 {
			return nil
		}
		demand := len(group.channels) * (group.capacity / weight)
		needed += demand
		flow.add(source, i, demand)
		for row, remaining := range s.remaining {
			if remaining == 0 || group.owner == row {
				continue
			}
			cap := min(len(group.channels), remaining)
			edge := flow.add(i, len(groups)+row, cap)
			links[i] = append(links[i], assignmentEdge{row, edge, cap})
		}
	}
	total := 0
	for row, remaining := range s.remaining {
		flow.add(len(groups)+row, sink, remaining)
		total += remaining
	}
	if total != needed || flow.solve(source, sink) != needed {
		return nil
	}
	// A group has equal residual degree for all its channels. A simple bipartite
	// degree realization ensures a row never selects the same channel twice.
	for i, group := range groups {
		assignments := []struct{ row, count int }{}
		for _, link := range links[i] {
			used := link.capacity - flow.edges[i][link.index].capacity
			if used > 0 {
				assignments = append(assignments, struct{ row, count int }{link.row, used})
			}
		}
		sort.Slice(assignments, func(i, j int) bool {
			if assignments[i].count != assignments[j].count {
				return assignments[i].count > assignments[j].count
			}
			return assignments[i].row < assignments[j].row
		})
		for _, assignment := range assignments {
			channels := append([]int(nil), group.channels...)
			sort.Slice(channels, func(i, j int) bool {
				if s.caps[channels[i]] != s.caps[channels[j]] {
					return s.caps[channels[i]] > s.caps[channels[j]]
				}
				return channels[i] < channels[j]
			})
			for _, channel := range channels[:assignment.count] {
				if s.caps[channel] < weight || s.chosen[assignment.row][channel] {
					return nil
				}
				q.apply(s, assignment.row, channel)
			}
		}
	}
	for _, cap := range s.caps {
		if cap != 0 {
			return nil
		}
	}
	for _, count := range s.remaining {
		if count != 0 {
			return nil
		}
	}
	return s
}
func (q *membershipSolver) solve(s *membershipState) *membershipState {
	if !q.budget() {
		return nil
	}
	// Filling every available channel is forced, including the common case of
	// C and D each joining the entire existing channel array.
	for {
		changed := false
		for row, remaining := range s.remaining {
			if remaining == 0 {
				continue
			}
			candidates := q.candidates(s, row)
			if len(candidates) < remaining {
				return nil
			}
			if len(candidates) == remaining {
				for _, channel := range candidates {
					q.apply(s, row, channel)
				}
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	active := []int{}
	for row, remaining := range s.remaining {
		if remaining > 0 {
			active = append(active, row)
		}
	}
	if len(active) == 0 {
		for _, cap := range s.caps {
			if cap != 0 {
				return nil
			}
		}
		return s
	}
	weight := q.weights[active[0]]
	uniform := true
	for _, row := range active {
		if q.weights[row] != weight {
			uniform = false
			break
		}
	}
	if uniform {
		return q.uniform(s, weight)
	}
	key := q.key(s)
	if q.dead[key] {
		return nil
	}
	sort.Slice(active, func(i, j int) bool {
		a, b := active[i], active[j]
		if q.weights[a] != q.weights[b] {
			return q.weights[a] > q.weights[b]
		}
		if s.remaining[a] != s.remaining[b] {
			return s.remaining[a] > s.remaining[b]
		}
		return a < b
	})
	row := active[0]
	groups := membershipGroups(q.owners, s.caps)
	eligible := []membershipGroup{}
	for _, group := range groups {
		if group.owner != row && group.capacity >= q.weights[row] {
			eligible = append(eligible, group)
		}
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		if eligible[i].capacity != eligible[j].capacity {
			return eligible[i].capacity < eligible[j].capacity
		}
		return eligible[i].owner < eligible[j].owner
	})
	suffix := make([]int, len(eligible)+1)
	for i := len(eligible) - 1; i >= 0; i-- {
		suffix[i] = suffix[i+1] + len(eligible[i].channels)
	}
	take := make([]int, len(eligible))
	var distribute func(int, int) *membershipState
	distribute = func(groupIndex, remaining int) *membershipState {
		if !q.budget() {
			return nil
		}
		if groupIndex == len(eligible) {
			if remaining != 0 {
				return nil
			}
			next := s.clone()
			for i, n := range take {
				for _, channel := range eligible[i].channels[:n] {
					q.apply(next, row, channel)
				}
			}
			return q.solve(next)
		}
		low, high := max(0, remaining-suffix[groupIndex+1]), min(remaining, len(eligible[groupIndex].channels))
		for n := high; n >= low; n-- {
			take[groupIndex] = n
			if result := distribute(groupIndex+1, remaining-n); result != nil {
				return result
			}
			if q.err != nil {
				return nil
			}
		}
		return nil
	}
	result := distribute(0, s.remaining[row])
	if result == nil && q.err == nil {
		q.dead[key] = true
	}
	return result
}

func membershipLayout(c Config) ([]ChannelAssignment, error) {
	if c.UsersPerChannel < 1 || c.UsersPerChannel > 300 || c.TotalUIDs < 1 || c.TotalUIDs != c.Channels*c.UsersPerChannel {
		return nil, errors.New("总 PCU 必须等于总频道数 × 每频道 UID 数（每频道 1～300 UID）")
	}
	if len(c.Allocations) < 1 || len(c.Allocations) > 128 {
		return nil, errors.New("请配置 1～128 台执行机器")
	}
	roles, nodes := map[string]bool{}, map[string]bool{}
	created, pcu := 0, 0
	for _, row := range c.Allocations {
		if !ValidID(row.Role) || roles[row.Role] || !ValidID(row.NodeID) || nodes[row.NodeID] {
			return nil, errors.New("分组和机器标识不能重复")
		}
		roles[row.Role] = true
		nodes[row.NodeID] = true
		if row.PublishingChannels == nil || row.JoiningChannels == nil || row.PublishersPerChannel == nil || row.ReceiversPerChannel == nil {
			return nil, fmt.Errorf("分组 %s 须填写创建、加入已有频道和频道内 UID 行为/个数", row.Role)
		}
		owned, joined, send, receive := count(row.PublishingChannels), count(row.JoiningChannels), row.PublisherCount(), count(row.ReceiversPerChannel)
		weight := send + receive
		if owned < 0 || owned > c.Channels {
			return nil, fmt.Errorf("分组 %s 创建频道数须为 0～%d", row.Role, c.Channels)
		}
		if joined < 0 || joined > c.Channels-owned {
			return nil, fmt.Errorf("分组 %s 加入已有频道最多为其它机器创建的 %d 个频道", row.Role, c.Channels-owned)
		}
		if send < 0 || send > 100 || receive < 0 || receive > 100 || weight < 1 || weight > c.UsersPerChannel {
			return nil, fmt.Errorf("分组 %s 每频道发流＋收流 UID 须为 1～%d，单项 0～100", row.Role, c.UsersPerChannel)
		}
		memberships := owned + joined
		if memberships == 0 {
			return nil, fmt.Errorf("分组 %s 未分配任务，请配置频道个数或移除该分组", row.Role)
		}
		if row.ProcessCount < 1 || row.ProcessCount > memberships || row.MaxUIDsPerProcess < 1 || row.MaxUIDsPerProcess > 3000 {
			return nil, fmt.Errorf("分组 %s 进程数须为 1～%d，单进程分配 UID 数须为 1～3000", row.Role, memberships)
		}
		required := row.MembershipUIDs()
		assigned := row.ProcessCount * row.MaxUIDsPerProcess
		if assigned != required {
			return nil, fmt.Errorf("分组 %s UID 分配不相等：%d 个进程 × 每进程 %d UID = %d，频道任务需要 %d UID", row.Role, row.ProcessCount, row.MaxUIDsPerProcess, assigned, required)
		}
		if row.MaxUIDsPerProcess%weight != 0 {
			return nil, fmt.Errorf("分组 %s 单进程分配 UID 数 %d 必须是每频道本机 UID 数 %d 的整数倍，频道内本机用户须在同一进程", row.Role, row.MaxUIDsPerProcess, weight)
		}
		created += owned
		pcu += row.MembershipUIDs()
	}
	if created != c.Channels {
		return nil, fmt.Errorf("创建频道合计须为 %d，当前为 %d", c.Channels, created)
	}
	if pcu != c.TotalUIDs {
		return nil, fmt.Errorf("UID 配额合计须为 %d，当前为 %d，请调整加入频道数或频道内 UID 个数", c.TotalUIDs, pcu)
	}
	state := &membershipState{caps: make([]int, c.Channels), remaining: make([]int, len(c.Allocations)), chosen: make([][]bool, len(c.Allocations))}
	owners := make([]int, c.Channels)
	weights := make([]int, len(c.Allocations))
	for channel := range state.caps {
		state.caps[channel] = c.UsersPerChannel
	}
	offset := 0
	for row, allocation := range c.Allocations {
		state.chosen[row] = make([]bool, c.Channels)
		state.remaining[row] = count(allocation.JoiningChannels)
		weights[row] = allocation.PublisherCount() + count(allocation.ReceiversPerChannel)
		for channel := offset; channel < offset+count(allocation.PublishingChannels); channel++ {
			owners[channel] = row
			state.chosen[row][channel] = true
			state.caps[channel] -= weights[row]
		}
		offset += count(allocation.PublishingChannels)
	}
	solver := &membershipSolver{owners: owners, weights: weights, dead: map[string]bool{}, deadline: time.Now().Add(2 * time.Second)}
	solved := solver.solve(state)
	if solved == nil {
		if solver.err != nil {
			return nil, solver.err
		}
		return nil, errors.New("无法按这些频道与 UID 配额填满每个频道，请调整加入已有频道数或频道内 UID 个数")
	}
	layout := make([]ChannelAssignment, c.Channels)
	for channel, owner := range owners {
		allocation := c.Allocations[owner]
		entry := ChannelAssignment{Channel: fmt.Sprintf("%s_%d", c.ChannelPrefix, c.ChannelStart+channel), CreatorRole: allocation.Role, CreatorNode: allocation.NodeID, LocalReceivers: count(allocation.ReceiversPerChannel)}
		for row, member := range c.Allocations {
			if !solved.chosen[row][channel] {
				continue
			}
			entry.Members = append(entry.Members, ChannelMember{Role: member.Role, Node: member.NodeID, Publishers: member.PublisherCount(), Receivers: count(member.ReceiversPerChannel), OwnsChannel: row == owner})
			entry.Publishers += member.PublisherCount()
			entry.TotalUIDs += weights[row]
			if row != owner {
				entry.JoinedReceivers += count(member.ReceiversPerChannel)
			}
		}
		layout[channel] = entry
	}
	return layout, nil
}

func memberConnections(c Config, index int, entry ChannelAssignment) map[string][]Connection {
	result := map[string][]Connection{}
	sourceSlots := [2]int{}
	receiveSlot := 0
	for _, member := range entry.Members {
		for slot := 0; slot < member.Publishers; slot++ {
			source := slot % 2
			uid := c.UIDs[source] + uint64(index) + uint64(sourceSlots[source])*uint64(c.Channels)
			sourceSlots[source]++
			result[member.Role] = append(result[member.Role], Connection{entry.Channel, uid, 0, source})
		}
		for slot := 0; slot < member.Receivers; slot++ {
			uid := c.UIDs[2] + uint64(index)*uint64(c.UsersPerChannel) + uint64(receiveSlot)
			receiveSlot++
			result[member.Role] = append(result[member.Role], Connection{entry.Channel, uid, 1, -1})
		}
	}
	return result
}
func (c Config) validateMembership() error {
	layout, err := membershipLayout(c)
	if err != nil {
		return err
	}
	for index, entry := range layout {
		seen := map[uint64]bool{}
		for _, group := range memberConnections(c, index, entry) {
			for _, u := range group {
				if u.UID == 0 || u.UID > 4294967295 || seen[u.UID] {
					return errors.New("同频道 UID 冲突或超出 uint32 范围，请调整 UID 基值")
				}
				seen[u.UID] = true
			}
		}
	}
	return nil
}
func buildMembership(c Config) (Plan, error) {
	layout, err := membershipLayout(c)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{ChannelAssignments: layout}
	hostIndex := map[string]int{}
	for _, row := range c.Allocations {
		host := HostPlan{Host: row.Role, NodeID: row.NodeID, PublishingChannels: count(row.PublishingChannels), JoiningChannels: count(row.JoiningChannels), PublishersPerChannel: row.PublisherCount(), ReceiversPerChannel: count(row.ReceiversPerChannel), Channels: count(row.PublishingChannels) + count(row.JoiningChannels), MaxUIDsPerProcess: row.MaxUIDsPerProcess, AudioPublishers: (count(row.PublishingChannels) + count(row.JoiningChannels)) * row.PublisherCount()}
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
	for channel, entry := range layout {
		connections := memberConnections(c, channel, entry)
		for _, member := range entry.Members {
			host := &plan.Hosts[hostIndex[member.Role]]
			group := connections[member.Role]
			target := -1
			for index, p := range host.Processes {
				if len(p.Connections)+len(group) <= host.MaxUIDsPerProcess && (target < 0 || len(p.Connections) < len(host.Processes[target].Connections)) {
					target = index
				}
			}
			if target < 0 {
				return Plan{}, fmt.Errorf("分组 %s 进程容量不足", host.Host)
			}
			p := &host.Processes[target]
			p.Connections = append(p.Connections, group...)
			p.ChannelNames = append(p.ChannelNames, entry.Channel)
			host.Connections += len(group)
		}
	}
	for _, host := range plan.Hosts {
		for _, p := range host.Processes {
			if len(p.Connections) == 0 {
				return Plan{}, fmt.Errorf("分组 %s 存在空进程，请减少进程数", host.Host)
			}
		}
		plan.TotalProcesses += len(host.Processes)
		plan.TotalConnections += host.Connections
		plan.AudioPublishers += host.AudioPublishers
	}
	if err := auditMembership(c, plan); err != nil {
		return Plan{}, err
	}
	unique, joins := 0, 0
	for _, entry := range layout {
		if len(entry.Members) > 1 {
			unique++
		}
		joins += len(entry.Members) - 1
	}
	plan.Coverage = &CoverageSummary{Channels: len(layout), ExtraChannels: unique, JoinMemberships: joins, TotalUIDs: plan.TotalConnections, Verified: true}
	return plan, nil
}
func auditMembership(c Config, plan Plan) error {
	users := map[string]map[uint64]bool{}
	counts := map[string]map[string]int{}
	pubs := map[string]map[string]int{}
	nodeChannels := map[string]map[string]bool{}
	for _, host := range plan.Hosts {
		if nodeChannels[host.Host] != nil {
			return errors.New("机器重复，已阻止下发")
		}
		nodeChannels[host.Host] = map[string]bool{}
		actual := 0
		for _, process := range host.Processes {
			localChannels := map[string]bool{}
			for _, u := range process.Connections {
				if users[u.Channel] == nil {
					users[u.Channel] = map[uint64]bool{}
					counts[u.Channel] = map[string]int{}
					pubs[u.Channel] = map[string]int{}
				}
				if users[u.Channel][u.UID] {
					return errors.New("同频道 UID 重复，已阻止下发")
				}
				users[u.Channel][u.UID] = true
				counts[u.Channel][host.Host]++
				if u.AudioSource >= 0 {
					pubs[u.Channel][host.Host]++
				}
				localChannels[u.Channel] = true
				actual++
			}
			if len(process.Connections) != host.MaxUIDsPerProcess {
				return errors.New("进程实际 UID 数与固定分配数不一致，已阻止下发")
			}
			for channel := range localChannels {
				if nodeChannels[host.Host][channel] {
					return errors.New("机器的同一频道被重复分到多个进程，已阻止下发")
				}
				nodeChannels[host.Host][channel] = true
			}
		}
		row := c.Allocation(host.Host)
		if actual != host.Connections || actual != row.MembershipUIDs() || len(nodeChannels[host.Host]) != count(row.PublishingChannels)+count(row.JoiningChannels) {
			return errors.New("机器 UID 或频道数量不一致，已阻止下发")
		}
	}
	if len(users) != c.Channels || len(plan.ChannelAssignments) != c.Channels || plan.TotalConnections != c.TotalUIDs {
		return errors.New("总频道或 PCU 不一致，已阻止下发")
	}
	seen := map[string]bool{}
	owned, joined := map[string]int{}, map[string]int{}
	for _, entry := range plan.ChannelAssignments {
		if seen[entry.Channel] || entry.TotalUIDs != c.UsersPerChannel || len(users[entry.Channel]) != c.UsersPerChannel {
			return errors.New("频道重复或人数不一致，已阻止下发")
		}
		seen[entry.Channel] = true
		members := map[string]bool{}
		creators := 0
		for _, member := range entry.Members {
			row := c.Allocation(member.Role)
			if members[member.Role] || row.NodeID != member.Node || counts[entry.Channel][member.Role] != member.Publishers+member.Receivers || pubs[entry.Channel][member.Role] != member.Publishers || row.PublisherCount() != member.Publishers || count(row.ReceiversPerChannel) != member.Receivers {
				return errors.New("频道内机器行为或 UID 不一致，已阻止下发")
			}
			members[member.Role] = true
			if member.OwnsChannel {
				creators++
				owned[member.Role]++
				if member.Role != entry.CreatorRole || member.Node != entry.CreatorNode {
					return errors.New("创建机器不一致，已阻止下发")
				}
			} else {
				joined[member.Role]++
			}
		}
		if creators != 1 || len(members) != len(counts[entry.Channel]) {
			return errors.New("频道机器名单不一致，已阻止下发")
		}
	}
	for _, row := range c.Allocations {
		if owned[row.Role] != count(row.PublishingChannels) || joined[row.Role] != count(row.JoiningChannels) {
			return errors.New("创建或加入已有频道配额不一致，已阻止下发")
		}
	}
	return nil
}

// AudioPeerIndex computes membership once per worker and materializes only the
// channels carried by its process. It includes publishers on joined channels.
func (c Config) AudioPeerIndex(channels []string) (map[string][]Connection, error) {
	result := map[string][]Connection{}
	if !c.Membership() {
		for _, name := range channels {
			peers, err := c.AudioPeers(name, 0)
			if err != nil {
				return nil, err
			}
			result[name] = peers
		}
		return result, nil
	}
	layout, err := membershipLayout(c)
	if err != nil {
		return nil, err
	}
	for _, name := range channels {
		if !strings.HasPrefix(name, c.ChannelPrefix+"_") {
			return nil, errors.New("频道前缀不属于当前任务")
		}
		number, err := strconv.Atoi(strings.TrimPrefix(name, c.ChannelPrefix+"_"))
		if err != nil {
			return nil, err
		}
		index := number - c.ChannelStart
		if index < 0 || index >= len(layout) || layout[index].Channel != name {
			return nil, errors.New("频道不属于当前任务")
		}
		result[name] = []Connection{}
		groups := memberConnections(c, index, layout[index])
		for _, row := range c.Allocations {
			for _, u := range groups[row.Role] {
				if u.AudioSource >= 0 {
					result[name] = append(result[name], u)
				}
			}
		}
	}
	return result, nil
}

package scenario

import (
	"errors"
	"fmt"
	"regexp"
)

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}$`)

func ValidID(id string) bool { return id != "all" && identifier.MatchString(id) }
func (c Config) Flexible() bool {
	for _, row := range c.Allocations {
		if row.PublishingChannels != nil || row.ReceiversPerChannel != nil || row.PublishersPerChannel != nil {
			return true
		}
	}
	return false
}
func count(n *int) int {
	if n == nil {
		return 0
	}
	return *n
}

// Older saved allocations used two publishers in every owned channel.
func (r RoleAllocation) PublisherCount() int {
	if r.PublishersPerChannel != nil {
		return *r.PublishersPerChannel
	}
	return 2
}

func publisherUID(c Config, channel, slot int) uint64 {
	return c.UIDs[slot%2] + uint64(channel) + uint64(slot/2)*uint64(c.Channels)
}
func (c Config) validateFlexible() error {
	if len(c.Allocations) < 1 || len(c.Allocations) > 128 {
		return errors.New("请配置 1～128 个执行分组")
	}
	roles, nodes := map[string]bool{}, map[string]bool{}
	publishing, receiving := 0, 0
	for _, row := range c.Allocations {
		if !ValidID(row.Role) || roles[row.Role] {
			return errors.New("执行分组标识无效或重复")
		}
		roles[row.Role] = true
		if !ValidID(row.NodeID) || nodes[row.NodeID] {
			return errors.New("每个执行分组须选择不同的已发现机器")
		}
		nodes[row.NodeID] = true
		if row.PublishingChannels == nil || row.ReceiversPerChannel == nil {
			return fmt.Errorf("分组 %s 须填写创建频道个数与单频道内收流人数", row.Role)
		}
		pubs, recv, send := count(row.PublishingChannels), count(row.ReceiversPerChannel), row.PublisherCount()
		if pubs < 0 || pubs > c.Channels || recv < 0 || recv > 100 || send < 0 || send > 100 || (pubs > 0 && send == 0) {
			return fmt.Errorf("分组 %s 创建频道个数须为 0～%d，单频道内发流人数和收流人数为 0～100；创建频道时至少有 1 人发流", row.Role, c.Channels)
		}
		required := pubs*send + c.Channels*recv
		groups := pubs + (c.Channels-pubs)*recv
		if required == 0 {
			return fmt.Errorf("分组 %s 未分配任何发布或接收用户，请配置任务或移除该分组", row.Role)
		}
		if row.ProcessCount < 1 || row.ProcessCount > groups {
			return fmt.Errorf("分组 %s 的进程数须为 1～%d，保证每个进程分配到用户", row.Role, groups)
		}
		if row.MaxUIDsPerProcess < 1 || row.MaxUIDsPerProcess > 3000 {
			return fmt.Errorf("分组 %s 每进程 UID 上限须为 1～3000", row.Role)
		}
		groupSize := send + recv
		if row.ProcessCount*row.MaxUIDsPerProcess < required || (pubs > 0 && row.ProcessCount*(row.MaxUIDsPerProcess/groupSize) < pubs) {
			return fmt.Errorf("分组 %s 容量不足：需要 %d UID，创建频道内的 %d 人组须放在同一进程", row.Role, required, groupSize)
		}
		publishing += pubs
		receiving += recv
	}
	if publishing != c.Channels {
		return fmt.Errorf("所有分组创建频道个数合计须为 %d，当前为 %d", c.Channels, publishing)
	}
	if receiving > 0 {
		end := c.UIDs[2] + uint64(c.Channels)*uint64(receiving) - 1
		if c.UIDs[2] == 0 || end > 4294967295 {
			return errors.New("接收者 UID 范围超出 1～4294967295")
		}
	}
	channel := 0
	for _, row := range c.Allocations {
		for limit := channel + count(row.PublishingChannels); channel < limit; channel++ {
			seen := map[uint64]bool{}
			firstReceiver := c.UIDs[2] + uint64(channel)*uint64(receiving)
			for slot := 0; slot < row.PublisherCount(); slot++ {
				uid := publisherUID(c, channel, slot)
				if uid == 0 || uid > 4294967295 {
					return errors.New("发流 UID 范围超出 1～4294967295")
				}
				if seen[uid] || (receiving > 0 && uid >= firstReceiver && uid < firstReceiver+uint64(receiving)) {
					return errors.New("同频道 UID 冲突，请调整音源发送者或接收者的 UID 起始值")
				}
				seen[uid] = true
			}
		}
	}
	return nil
}

// Publisher counts belong to each channel owner. Receiver counts belong to allocations,
// independently of the node owning that channel's publishers.
func buildFlexible(c Config) (Plan, error) {
	plan := Plan{}
	totalReceivers := 0
	for _, row := range c.Allocations {
		totalReceivers += count(row.ReceiversPerChannel)
	}
	publisherOffset, receiverOffset := 0, 0
	for _, row := range c.Allocations {
		pubs, recv, send := count(row.PublishingChannels), count(row.ReceiversPerChannel), row.PublisherCount()
		host := HostPlan{Host: row.Role, NodeID: row.NodeID, MaxUIDsPerProcess: row.MaxUIDsPerProcess, PublishingChannels: pubs, PublishersPerChannel: send, AudioPublishers: pubs * send, ReceiversPerChannel: recv}
		if recv > 0 {
			host.Channels = c.Channels
		} else {
			host.Channels = pubs
		}
		for i := 0; i < row.ProcessCount; i++ {
			host.Processes = append(host.Processes, Process{ID: fmt.Sprintf("%s-%03d", row.Role, i+1), Host: row.Role})
		}
		place := func(group []Connection) error {
			target := -1
			for i, p := range host.Processes {
				if len(p.Connections)+len(group) <= row.MaxUIDsPerProcess && (target < 0 || len(p.Connections) < len(host.Processes[target].Connections)) {
					target = i
				}
			}
			if target < 0 {
				return fmt.Errorf("分组 %s 无法在 UID 上限内放置完整的频道用户组", row.Role)
			}
			proc := &host.Processes[target]
			proc.Connections = append(proc.Connections, group...)
			channel := group[0].Channel
			exists := false
			for _, name := range proc.ChannelNames {
				if name == channel {
					exists = true
					break
				}
			}
			if !exists {
				proc.ChannelNames = append(proc.ChannelNames, channel)
			}
			return nil
		}
		receiver := func(channel string, n, slot int) Connection {
			return Connection{Channel: channel, UID: c.UIDs[2] + uint64(n)*uint64(totalReceivers) + uint64(receiverOffset+slot), Role: 1, AudioSource: -1}
		}
		for n := publisherOffset; n < publisherOffset+pubs; n++ {
			channel := fmt.Sprintf("%s_%d", c.ChannelPrefix, c.ChannelStart+n)
			group := make([]Connection, 0, send+recv)
			for slot := 0; slot < send; slot++ {
				group = append(group, Connection{channel, publisherUID(c, n, slot), 0, slot % 2})
			}
			for slot := 0; slot < recv; slot++ {
				group = append(group, receiver(channel, n, slot))
			}
			if err := place(group); err != nil {
				return Plan{}, err
			}
		}
		for n := 0; n < c.Channels; n++ {
			if n >= publisherOffset && n < publisherOffset+pubs {
				continue
			}
			channel := fmt.Sprintf("%s_%d", c.ChannelPrefix, c.ChannelStart+n)
			for slot := 0; slot < recv; slot++ {
				if err := place([]Connection{receiver(channel, n, slot)}); err != nil {
					return Plan{}, err
				}
			}
		}
		for i := range host.Processes {
			p := &host.Processes[i]
			if len(p.Connections) == 0 {
				return Plan{}, fmt.Errorf("分组 %s 进程过多，存在空进程", row.Role)
			}
			if c.Engine == "go-sdk" {
				p.Services = 1
			}
			host.Connections += len(p.Connections)
		}
		plan.Hosts = append(plan.Hosts, host)
		plan.TotalConnections += host.Connections
		plan.AudioPublishers += host.AudioPublishers
		plan.TotalProcesses += len(host.Processes)
		publisherOffset += pubs
		receiverOffset += recv
	}
	return plan, nil
}

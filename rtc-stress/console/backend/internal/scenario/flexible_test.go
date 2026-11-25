package scenario

import (
	"fmt"
	"testing"
)

func intPtr(n int) *int { return &n }
func flexibleConfig(nodes int) Config {
	c := Defaults()
	c.Channels = 24
	for i := 0; i < nodes; i++ {
		pubs := c.Channels / nodes
		if i < c.Channels%nodes {
			pubs++
		}
		receivers := i % 3
		if nodes == 1 {
			receivers = 3
		}
		required := 2*pubs + c.Channels*receivers
		processes := 2
		if pubs+(c.Channels-pubs)*receivers < processes {
			processes = 1
		}
		if required == 0 {
			receivers = 1
			required = c.Channels
		}
		c.Allocations = append(c.Allocations, RoleAllocation{Role: fmt.Sprintf("group-%d", i), NodeID: fmt.Sprintf("node-%d", i), PublishingChannels: intPtr(pubs), ReceiversPerChannel: intPtr(receivers), ProcessCount: processes, MaxUIDsPerProcess: (required+processes-1)/processes + 4})
	}
	return c
}
func TestFlexibleMachineCountsUIDsAndCapacity(t *testing.T) {
	for _, n := range []int{1, 2, 4, 8, 24} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			c := flexibleConfig(n)
			plan, err := Build(c)
			if err != nil {
				t.Fatal(err)
			}
			expectedProcesses := 0
			for _, row := range c.Allocations {
				expectedProcesses += row.ProcessCount
			}
			if len(plan.Hosts) != n || plan.TotalProcesses != expectedProcesses || plan.AudioPublishers != 48 {
				t.Fatalf("wrong totals: %+v", plan)
			}
			receivers := 0
			for _, row := range c.Allocations {
				receivers += *row.ReceiversPerChannel
			}
			if plan.TotalConnections != c.Channels*(2+receivers) {
				t.Fatal("wrong connection total")
			}
			channels := map[string]map[uint64]bool{}
			senders := map[string]int{}
			sourceProcess := map[string]string{}
			for index, host := range plan.Hosts {
				row := c.Allocations[index]
				if host.Connections != 2**row.PublishingChannels+c.Channels**row.ReceiversPerChannel {
					t.Fatal("incorrect per-node load")
				}
				for _, process := range host.Processes {
					if len(process.Connections) == 0 || len(process.Connections) > row.MaxUIDsPerProcess || process.Services != 1 {
						t.Fatal("bad process packing")
					}
					for _, connection := range process.Connections {
						if channels[connection.Channel] == nil {
							channels[connection.Channel] = map[uint64]bool{}
						}
						if channels[connection.Channel][connection.UID] {
							t.Fatal("duplicate channel UID")
						}
						channels[connection.Channel][connection.UID] = true
						if connection.AudioSource >= 0 {
							senders[connection.Channel]++
							if previous := sourceProcess[connection.Channel]; previous != "" && previous != process.ID {
								t.Fatal("publisher pair split across processes")
							}
							sourceProcess[connection.Channel] = process.ID
						}
					}
				}
			}
			if len(channels) != c.Channels {
				t.Fatal("missing channel")
			}
			for channel, users := range channels {
				if len(users) != 2+receivers || senders[channel] != 2 {
					t.Fatal("wrong channel model")
				}
			}
		})
	}
}
func TestFlexibleInvalidAssignments(t *testing.T) {
	cases := map[string]func(*Config){
		"duplicate node":                func(c *Config) { c.Allocations[1].NodeID = c.Allocations[0].NodeID },
		"missing node":                  func(c *Config) { c.Allocations[0].NodeID = "" },
		"unsafe role":                   func(c *Config) { c.Allocations[0].Role = "../escape" },
		"wrong publisher sum":           func(c *Config) { c.Allocations[0].PublishingChannels = intPtr(0) },
		"insufficient process capacity": func(c *Config) { c.Allocations[0].MaxUIDsPerProcess = 1 },
		"negative receiver quota":       func(c *Config) { c.Allocations[0].ReceiversPerChannel = intPtr(-1) },
		"no workload": func(c *Config) {
			c.Allocations[0].PublishingChannels = intPtr(0)
			c.Allocations[0].ReceiversPerChannel = intPtr(0)
		},
		"UID collision": func(c *Config) { c.UIDs[2] = c.UIDs[0] - 1 },
		"UID overflow":  func(c *Config) { c.UIDs[2] = 4294967290 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := flexibleConfig(4)
			mutate(&c)
			if _, err := Build(c); err == nil {
				t.Fatal("invalid assignment accepted")
			}
		})
	}
}

func TestFlexiblePerMachinePublisherCountsAndSourceMapping(t *testing.T) {
	c := Defaults()
	c.Channels = 6
	c.Allocations = []RoleAllocation{
		{Role: "A", NodeID: "node-a", PublishingChannels: intPtr(3), PublishersPerChannel: intPtr(1), ReceiversPerChannel: intPtr(1), ProcessCount: 3, MaxUIDsPerProcess: 4},
		{Role: "B", NodeID: "node-b", PublishingChannels: intPtr(3), PublishersPerChannel: intPtr(4), ReceiversPerChannel: intPtr(2), ProcessCount: 3, MaxUIDsPerProcess: 8},
	}
	p, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	if p.TotalProcesses != 6 || p.TotalConnections != 33 || p.AudioPublishers != 15 || p.Hosts[0].Connections != 9 || p.Hosts[1].Connections != 24 {
		t.Fatalf("incorrect allocation totals: %+v", p)
	}
	channels := map[string][]Connection{}
	for i, host := range p.Hosts {
		for _, process := range host.Processes {
			if process.Services != 1 || len(process.Connections) > c.Allocations[i].MaxUIDsPerProcess {
				t.Fatal("incorrect Service or process capacity")
			}
			for _, connection := range process.Connections {
				channels[connection.Channel] = append(channels[connection.Channel], connection)
			}
		}
	}
	for n := 0; n < c.Channels; n++ {
		name := fmt.Sprintf("%s_%d", c.ChannelPrefix, c.ChannelStart+n)
		want := 1
		if n >= 3 {
			want = 4
		}
		seen := map[uint64]bool{}
		sources := [2]int{}
		for _, connection := range channels[name] {
			if seen[connection.UID] {
				t.Fatal("UID collision")
			}
			seen[connection.UID] = true
			if connection.AudioSource >= 0 {
				sources[connection.AudioSource]++
			}
		}
		if len(seen) != want+3 || sources[0] != (want+1)/2 || sources[1] != want/2 {
			t.Fatalf("%s: wrong users or audio assignment: %v %v", name, seen, sources)
		}
	}
	for name, mutate := range map[string]func(*Config){
		"zero publishers in owned channel": func(c *Config) { c.Allocations[0].PublishersPerChannel = intPtr(0) },
		"too many publishers":              func(c *Config) { c.Allocations[0].PublishersPerChannel = intPtr(101) },
		"whole channel does not fit":       func(c *Config) { c.Allocations[1].MaxUIDsPerProcess = 5 },
		"extra publisher UID collision":    func(c *Config) { c.UIDs[1] = c.UIDs[0] + uint64(c.Channels) },
		"extra publisher UID overflow":     func(c *Config) { c.UIDs[0] = 4294967290 },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := c
			invalid.Allocations = append([]RoleAllocation(nil), c.Allocations...)
			mutate(&invalid)
			if _, err := Build(invalid); err == nil {
				t.Fatal("invalid sender allocation accepted")
			}
		})
	}
}

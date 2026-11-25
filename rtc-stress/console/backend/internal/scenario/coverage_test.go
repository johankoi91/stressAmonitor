package scenario

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

func coverageExample() Config {
	c := Defaults()
	c.AllocationPolicy = "coverage"
	c.UsersPerChannel = 5
	c.TotalUIDs = 1000
	c.Channels = 200
	for i, node := range []string{"A", "B", "C", "D"} {
		owned, send, local, extra, remote := 0, 0, 0, 100, 2
		if i < 2 {
			owned, send, local, extra = 100, 2, 1, 0
		}
		c.Allocations = append(c.Allocations, RoleAllocation{Role: node, NodeID: node, PublishingChannels: intPtr(owned), PublishersPerChannel: intPtr(send), ReceiversPerChannel: intPtr(local), JoiningChannels: intPtr(extra), ReceiversPerJoinedChannel: intPtr(remote), ProcessCount: 10, MaxUIDsPerProcess: 100})
	}
	return c
}
func assertCoverage(t *testing.T, c Config, p Plan) {
	t.Helper()
	channelUsers := map[string]map[uint64]bool{}
	creatorSets, receiverSets := map[string]map[string]bool{}, map[string]map[string]bool{}
	for _, entry := range p.ChannelAssignments {
		if creatorSets[entry.CreatorRole] == nil {
			creatorSets[entry.CreatorRole] = map[string]bool{}
		}
		creatorSets[entry.CreatorRole][entry.Channel] = true
		if entry.ReceiverRole != "" {
			if receiverSets[entry.ReceiverRole] == nil {
				receiverSets[entry.ReceiverRole] = map[string]bool{}
			}
			receiverSets[entry.ReceiverRole][entry.Channel] = true
		}
		if entry.TotalUIDs != c.UsersPerChannel || entry.CreatorRole == entry.ReceiverRole {
			t.Fatal("invalid channel ownership or population")
		}
	}
	extraSeen := map[string]bool{}
	for _, set := range receiverSets {
		for channel := range set {
			if extraSeen[channel] {
				t.Fatal("channel assigned to multiple extra receiver machines")
			}
			extraSeen[channel] = true
		}
	}
	for _, host := range p.Hosts {
		allocation := c.Allocation(host.Host)
		if len(creatorSets[host.Host]) != count(allocation.PublishingChannels) || len(receiverSets[host.Host]) != count(allocation.JoiningChannels) || host.Connections != allocation.CoverageUIDs() {
			t.Fatalf("wrong node quotas: %+v", host)
		}
		for _, process := range host.Processes {
			if len(process.Connections) == 0 || len(process.Connections) > allocation.MaxUIDsPerProcess || process.Services != 1 {
				t.Fatal("bad process/service capacity")
			}
			for _, u := range process.Connections {
				if channelUsers[u.Channel] == nil {
					channelUsers[u.Channel] = map[uint64]bool{}
				}
				if channelUsers[u.Channel][u.UID] {
					t.Fatal("duplicate channel/UID")
				}
				channelUsers[u.Channel][u.UID] = true
			}
		}
	}
	if len(channelUsers) != c.Channels || p.TotalConnections != c.TotalUIDs || !p.Coverage.Verified {
		t.Fatal("incorrect channel/PCU coverage")
	}
	for _, users := range channelUsers {
		if len(users) != c.UsersPerChannel {
			t.Fatal("unequal channel population")
		}
	}
	again, err := Build(c)
	if err != nil || !reflect.DeepEqual(p, again) {
		t.Fatal("assignment is not deterministic", err)
	}
}
func TestCoverageThousandUIDsAndUnequalReceiverQuotas(t *testing.T) {
	for _, quota := range []int{1, 60, 100, 150, 199} {
		t.Run(fmt.Sprint(quota), func(t *testing.T) {
			c := coverageExample()
			c.Allocations[2].JoiningChannels = intPtr(quota)
			c.Allocations[3].JoiningChannels = intPtr(200 - quota)
			// Small quotas cannot create empty processes.
			c.Allocations[2].ProcessCount = min(10, quota*2)
			c.Allocations[3].ProcessCount = min(10, (200-quota)*2)
			p, err := Build(c)
			if err != nil {
				t.Fatal(err)
			}
			assertCoverage(t, c, p)
		})
	}
}
func TestCoverageCustomizedCreatorUIDsAndMixedMachineRoles(t *testing.T) {
	c := coverageExample()
	c.Allocations[0].PublishersPerChannel = intPtr(1)
	c.Allocations[0].ReceiversPerChannel = intPtr(3)
	c.Allocations[2].ReceiversPerJoinedChannel = intPtr(1)
	p, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	assertCoverage(t, c, p)
	for _, entry := range p.ChannelAssignments {
		want := "D"
		if entry.CreatorRole == "A" {
			want = "C"
		}
		if entry.ReceiverRole != want {
			t.Fatal("receiver did not match the channel's UID deficit")
		}
	}
	peers, err := c.AudioPeers(c.ChannelPrefix+"_1000", 30001)
	if err != nil || len(peers) != 1 {
		t.Fatal("custom publisher count was lost", peers, err)
	}
	c = coverageExample()
	c.Allocations = c.Allocations[:2]
	for i := range c.Allocations {
		c.Allocations[i].JoiningChannels = intPtr(100)
	}
	p, err = Build(c)
	if err != nil {
		t.Fatal(err)
	}
	assertCoverage(t, c, p)
	for _, entry := range p.ChannelAssignments {
		if entry.CreatorRole == entry.ReceiverRole {
			t.Fatal("owner received its own extra channel quota")
		}
	}
	c = coverageExample()
	c.Allocations = c.Allocations[:2]
	for i := range c.Allocations {
		c.Allocations[i].ReceiversPerChannel = intPtr(3)
	}
	p, err = Build(c)
	if err != nil {
		t.Fatal(err)
	}
	assertCoverage(t, c, p)
	if p.Coverage.ExtraChannels != 0 {
		t.Fatal("fully populated channels received extra users")
	}
}
func TestCoverageRejectsGapsDuplicatesAndUnmatchableQuotas(t *testing.T) {
	cases := map[string]func(*Config){
		"PCU equation":             func(c *Config) { c.TotalUIDs = 999 },
		"missing created channel":  func(c *Config) { c.Allocations[0].PublishingChannels = intPtr(99) },
		"missing receiver channel": func(c *Config) { c.Allocations[2].JoiningChannels = intPtr(99) },
		"extra receiver channel":   func(c *Config) { c.Allocations[2].JoiningChannels = intPtr(101) },
		"duplicate machine":        func(c *Config) { c.Allocations[3].NodeID = "C" },
		"no creator UID": func(c *Config) {
			c.Allocations[0].PublishersPerChannel = intPtr(0)
			c.Allocations[0].ReceiversPerChannel = intPtr(0)
		},
		"wrong receiver UID total": func(c *Config) { c.Allocations[2].ReceiversPerJoinedChannel = intPtr(1) },
		"capacity":                 func(c *Config) { c.Allocations[0].MaxUIDsPerProcess = 2 },
		"UID collision":            func(c *Config) { c.UIDs[2] = c.UIDs[0] },
		"UID overflow":             func(c *Config) { c.UIDs[2] = 4294967290 },
		"global total correct but deficits not matchable": func(c *Config) {
			c.Allocations = c.Allocations[:3]
			c.Allocations[0].ReceiversPerChannel = intPtr(2)
			c.Allocations[1].PublishersPerChannel = intPtr(1)
			c.Allocations[2].JoiningChannels = intPtr(200)
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			c := coverageExample()
			change(&c)
			if _, err := Build(c); err == nil {
				t.Fatal("invalid allocation accepted")
			}
		})
	}
}
func TestCoverageRandomizedExactPopulationAndNoDuplicateChannels(t *testing.T) {
	random := rand.New(rand.NewSource(90125))
	for trial := 0; trial < 100; trial++ {
		c := Defaults()
		c.AllocationPolicy = "coverage"
		c.UsersPerChannel = 5
		c.Channels = 10 + random.Intn(80)
		c.TotalUIDs = 5 * c.Channels
		creators := 2 + random.Intn(3)
		receivers := 2 + random.Intn(3)
		remaining := c.Channels
		for i := 0; i < creators; i++ {
			quota := remaining
			if i < creators-1 {
				quota = 1 + random.Intn(remaining-(creators-i-1))
			}
			remaining -= quota
			send := random.Intn(4)
			count := quota * 3
			processes := (count + 29) / 30
			c.Allocations = append(c.Allocations, RoleAllocation{Role: fmt.Sprintf("create-%d", i), NodeID: fmt.Sprintf("node-create-%d", i), PublishingChannels: intPtr(quota), PublishersPerChannel: intPtr(send), ReceiversPerChannel: intPtr(3 - send), JoiningChannels: intPtr(0), ReceiversPerJoinedChannel: intPtr(0), ProcessCount: processes, MaxUIDsPerProcess: 30})
		}
		remaining = c.Channels
		for i := 0; i < receivers; i++ {
			quota := remaining
			if i < receivers-1 {
				quota = 1 + random.Intn(remaining-(receivers-i-1))
			}
			remaining -= quota
			c.Allocations = append(c.Allocations, RoleAllocation{Role: fmt.Sprintf("receive-%d", i), NodeID: fmt.Sprintf("node-receive-%d", i), PublishingChannels: intPtr(0), PublishersPerChannel: intPtr(0), ReceiversPerChannel: intPtr(0), JoiningChannels: intPtr(quota), ReceiversPerJoinedChannel: intPtr(2), ProcessCount: (quota*2 + 29) / 30, MaxUIDsPerProcess: 30})
		}
		p, err := Build(c)
		if err != nil {
			t.Fatalf("trial %d: %v", trial, err)
		}
		assertCoverage(t, c, p)
	}
}
func TestCoverageAuditRejectsMaterializedCorruption(t *testing.T) {
	c := coverageExample()
	p, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"missing channel", "duplicate channel", "duplicate UID", "wrong receiver"} {
		data, _ := json.Marshal(p)
		var bad Plan
		_ = json.Unmarshal(data, &bad)
		switch kind {
		case "missing channel":
			bad.ChannelAssignments = bad.ChannelAssignments[:199]
		case "duplicate channel":
			bad.ChannelAssignments[1] = bad.ChannelAssignments[0]
		case "duplicate UID":
			bad.Hosts[0].Processes[0].Connections[1].UID = bad.Hosts[0].Processes[0].Connections[0].UID
		case "wrong receiver":
			bad.ChannelAssignments[0].ReceiverRole = "A"
		}
		if err := auditCoverage(c, bad); err == nil {
			t.Fatal("corrupted plan passed audit:", kind)
		}
	}
}

func TestCoverageFieldsCannotFallBackToLegacyRules(t *testing.T) {
	c := coverageExample()
	c.AllocationPolicy = ""
	p, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	assertCoverage(t, c, p)
	c.Allocations[2].JoiningChannels = intPtr(101)
	if _, err = Build(c); err == nil {
		t.Fatal("new quota fields bypassed coverage validation")
	}
}
func TestAudioPeersIncludeEveryCustomPublisherAndExcludeSelf(t *testing.T) {
	c := coverageExample()
	c.Allocations[0].PublishersPerChannel = intPtr(4)
	c.Allocations[0].ReceiversPerChannel = intPtr(0)
	c.Allocations[2].ReceiversPerJoinedChannel = intPtr(1)
	p, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	assertCoverage(t, c, p)
	peers, err := c.AudioPeers(c.ChannelPrefix+"_1000", 999999)
	if err != nil || len(peers) != 4 {
		t.Fatal(peers, err)
	}
	for index, peer := range peers {
		if peer.AudioSource != index%2 {
			t.Fatal("audio source mapping changed")
		}
	}
	local, err := c.AudioPeers(c.ChannelPrefix+"_1000", peers[0].UID)
	if err != nil || len(local) != 3 {
		t.Fatal(local, err)
	}
	if _, err := c.AudioPeers("other_1000", 999999); err == nil {
		t.Fatal("wrong channel prefix accepted")
	}
}

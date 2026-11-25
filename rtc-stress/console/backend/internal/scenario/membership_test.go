package scenario

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

func memberRow(id string, owned, joined, send, receive int) RoleAllocation {
	weight := send + receive
	memberships := owned + joined
	processes := max(1, min(10, memberships))
	for memberships > 0 && memberships%processes != 0 {
		processes--
	}
	return RoleAllocation{Role: id, NodeID: id, PublishingChannels: intPtr(owned), JoiningChannels: intPtr(joined), PublishersPerChannel: intPtr(send), ReceiversPerChannel: intPtr(receive), ProcessCount: processes, MaxUIDsPerProcess: max(1, weight*memberships/processes)}
}
func membershipExample() Config {
	c := Defaults()
	c.AllocationPolicy = "membership"
	c.Channels = 200
	c.UsersPerChannel = 5
	c.TotalUIDs = 1000
	c.Allocations = []RoleAllocation{memberRow("A", 100, 0, 2, 1), memberRow("B", 100, 0, 2, 1), memberRow("C", 0, 200, 0, 1), memberRow("D", 0, 200, 0, 1)}
	return c
}
func assertMembership(t *testing.T, c Config, p Plan) {
	t.Helper()
	actual := map[string]map[uint64]bool{}
	perRow := map[string]map[string]int{}
	for _, host := range p.Hosts {
		perRow[host.Host] = map[string]int{}
		for _, process := range host.Processes {
			if len(process.Connections) == 0 || len(process.Connections) != host.MaxUIDsPerProcess || process.Services != 1 {
				t.Fatal("invalid process/service capacity")
			}
			local := map[string]bool{}
			for _, u := range process.Connections {
				if actual[u.Channel] == nil {
					actual[u.Channel] = map[uint64]bool{}
				}
				if actual[u.Channel][u.UID] {
					t.Fatal("same channel/UID repeated")
				}
				actual[u.Channel][u.UID] = true
				local[u.Channel] = true
			}
			for channel := range local {
				perRow[host.Host][channel]++
			}
		}
		if host.Connections != c.Allocation(host.Host).MembershipUIDs() {
			t.Fatal("wrong row UID count")
		}
	}
	if len(actual) != c.Channels || p.TotalConnections != c.TotalUIDs || !p.Coverage.Verified {
		t.Fatal("wrong channel/PCU total")
	}
	for _, row := range c.Allocations {
		if len(perRow[row.Role]) != count(row.PublishingChannels)+count(row.JoiningChannels) {
			t.Fatal("row channel count wrong")
		}
		for _, count := range perRow[row.Role] {
			if count != 1 {
				t.Fatal("one machine's channel was duplicated across processes")
			}
		}
	}
	for _, entry := range p.ChannelAssignments {
		if len(actual[entry.Channel]) != c.UsersPerChannel || entry.TotalUIDs != c.UsersPerChannel {
			t.Fatal("per-channel population differs")
		}
		rows := map[string]bool{}
		for _, member := range entry.Members {
			if rows[member.Role] {
				t.Fatal("machine repeated inside channel")
			}
			rows[member.Role] = true
		}
	}
	again, err := Build(c)
	if err != nil || !reflect.DeepEqual(p, again) {
		t.Fatal("assignment is not reproducible", err)
	}
}
func TestMembershipCAndDEachJoinEntireCreatedArray(t *testing.T) {
	c := membershipExample()
	p, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	assertMembership(t, c, p)
	for _, entry := range p.ChannelAssignments {
		members := map[string]bool{}
		for _, member := range entry.Members {
			members[member.Role] = true
		}
		if !members["C"] || !members["D"] || len(members) != 3 {
			t.Fatal("C and D did not both join the existing channel")
		}
	}
	if p.Coverage.ExtraChannels != 200 || p.Coverage.JoinMemberships != 400 {
		t.Fatal("shared memberships counted as unique channels")
	}
}
func TestMembershipPartialWeightedAndMixedRoles(t *testing.T) {
	c := membershipExample()
	c.Allocations[2] = memberRow("C", 0, 100, 0, 2)
	c.Allocations[3] = memberRow("D", 0, 100, 0, 2)
	p, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	assertMembership(t, c, p)
	c = membershipExample()
	c.Allocations = []RoleAllocation{memberRow("A", 100, 100, 1, 1), memberRow("B", 100, 100, 1, 2)}
	p, err = Build(c)
	if err != nil {
		t.Fatal(err)
	}
	assertMembership(t, c, p)
	c = membershipExample()
	c.Allocations = []RoleAllocation{memberRow("A", 100, 0, 1, 1), memberRow("B", 100, 0, 1, 1), memberRow("C", 0, 100, 0, 2), memberRow("D", 0, 150, 0, 1), memberRow("E", 0, 150, 0, 1), memberRow("F", 0, 100, 0, 1)}
	p, err = Build(c)
	if err != nil {
		t.Fatal(err)
	}
	assertMembership(t, c, p)
}
func TestMembershipWeightedSearchBacktracksForOwnerExclusions(t *testing.T) {
	c := Defaults()
	c.AllocationPolicy = "membership"
	c.Channels = 4
	c.UsersPerChannel = 4
	c.TotalUIDs = 16
	c.Allocations = []RoleAllocation{memberRow("A", 2, 0, 1, 0), memberRow("B", 2, 1, 1, 0), memberRow("C", 0, 2, 0, 2), memberRow("D", 0, 4, 0, 1), memberRow("E", 0, 2, 0, 1), memberRow("F", 0, 1, 0, 1)}
	p, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	assertMembership(t, c, p)
}
func TestMembershipPublishersCanJoinExistingChannels(t *testing.T) {
	c := membershipExample()
	c.Allocations[2] = memberRow("C", 0, 200, 1, 0)
	p, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	assertMembership(t, c, p)
	if p.AudioPublishers != 600 || p.Hosts[2].AudioPublishers != 200 || !p.Hosts[2].HasPublishers() {
		t.Fatal("joined publishers were omitted")
	}
	index, err := c.AudioPeerIndex([]string{c.ChannelPrefix + "_1000", c.ChannelPrefix + "_1100"})
	if err != nil {
		t.Fatal(err)
	}
	for name, peers := range index {
		if len(peers) != 3 {
			t.Fatal("joined publisher absent from peer list", name)
		}
		seen := map[uint64]bool{}
		sources := [2]int{}
		for _, peer := range peers {
			if seen[peer.UID] {
				t.Fatal("duplicate publisher UID")
			}
			seen[peer.UID] = true
			sources[peer.AudioSource]++
		}
		if sources != [2]int{2, 1} {
			t.Fatal("per-machine source order was lost", sources)
		}
		for _, peer := range peers {
			others, err := c.AudioPeers(name, peer.UID)
			if err != nil || len(others) != 2 {
				t.Fatal("self not excluded", err)
			}
		}
	}
}
func TestMembershipRejectsInvalidCountsAndImpossibleSharedPopulation(t *testing.T) {
	cases := map[string]func(*Config){
		"PCU":                     func(c *Config) { c.TotalUIDs = 999 },
		"UID sum":                 func(c *Config) { c.Allocations[2].ReceiversPerChannel = intPtr(2) },
		"creator gap":             func(c *Config) { c.Allocations[0].PublishingChannels = intPtr(99) },
		"same machine":            func(c *Config) { c.Allocations[3].NodeID = "C" },
		"join own channels twice": func(c *Config) { c.Allocations[0].JoiningChannels = intPtr(101) },
		"same UID":                func(c *Config) { c.UIDs[2] = c.UIDs[0] },
		"UID overflow":            func(c *Config) { c.UIDs[2] = 4294967290 },
		"capacity":                func(c *Config) { c.Allocations[0].MaxUIDsPerProcess = 2 },
		"no behavior":             func(c *Config) { c.Allocations[2].ReceiversPerChannel = intPtr(0) },
		"global total correct but individual channels impossible": func(c *Config) {
			c.Allocations = []RoleAllocation{memberRow("A", 100, 0, 1, 1), memberRow("B", 100, 0, 1, 1), memberRow("C", 0, 100, 0, 2), memberRow("D", 0, 200, 0, 1), memberRow("E", 0, 200, 0, 1)}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			c := membershipExample()
			change(&c)
			if _, err := Build(c); err == nil {
				t.Fatal("invalid shared assignment accepted")
			}
		})
	}
}
func TestMembershipRandomizedSharedCoverage(t *testing.T) {
	random := rand.New(rand.NewSource(120012))
	for trial := 0; trial < 100; trial++ {
		channels := 10 + random.Intn(70)
		creators := 2 + random.Intn(3)
		joiners := 2 + random.Intn(4)
		degree := 1 + random.Intn(joiners)
		c := Defaults()
		c.AllocationPolicy = "membership"
		c.Channels = channels
		c.UsersPerChannel = 3 + degree
		c.TotalUIDs = channels * c.UsersPerChannel
		remaining := channels
		for owner := 0; owner < creators; owner++ {
			quota := remaining
			if owner < creators-1 {
				quota = 1 + random.Intn(remaining-(creators-owner-1))
			}
			remaining -= quota
			send := random.Intn(4)
			c.Allocations = append(c.Allocations, memberRow(fmt.Sprintf("owner-%d", owner), quota, 0, send, 3-send))
		}
		// Construct feasible receiver degree totals independently, with per-channel
		// distinct machines. The planner must realize these quotas without repeats.
		counts := make([]int, joiners)
		for channel := 0; channel < channels; channel++ {
			for offset := 0; offset < degree; offset++ {
				counts[(channel+offset)%joiners]++
			}
		}
		for row, count := range counts {
			c.Allocations = append(c.Allocations, memberRow(fmt.Sprintf("join-%d", row), 0, count, 0, 1))
		}
		p, err := Build(c)
		if err != nil {
			t.Fatalf("trial %d: %v", trial, err)
		}
		assertMembership(t, c, p)
	}
}
func TestMembershipAuditRejectsDuplicateOrMissingMembers(t *testing.T) {
	c := membershipExample()
	p, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"duplicate member", "missing channel", "duplicate UID", "wrong counts"} {
		data, _ := json.Marshal(p)
		var bad Plan
		_ = json.Unmarshal(data, &bad)
		switch kind {
		case "duplicate member":
			bad.ChannelAssignments[0].Members = append(bad.ChannelAssignments[0].Members, bad.ChannelAssignments[0].Members[0])
		case "missing channel":
			bad.ChannelAssignments = bad.ChannelAssignments[:199]
		case "duplicate UID":
			bad.Hosts[0].Processes[0].Connections[1].UID = bad.Hosts[0].Processes[0].Connections[0].UID
		case "wrong counts":
			bad.ChannelAssignments[0].Members[0].Receivers++
		}
		if err := auditMembership(c, bad); err == nil {
			t.Fatal("corrupted shared membership passed audit", kind)
		}
	}
}

func TestMembershipRequiresExactProcessUIDAssignment(t *testing.T) {
	for _, processes := range []int{10, 20} {
		c := membershipExample()
		c.Allocations[0].ProcessCount = processes
		c.Allocations[0].MaxUIDsPerProcess = 300 / processes
		p, err := Build(c)
		if err != nil {
			t.Fatal(err)
		}
		assertMembership(t, c, p)
		for _, process := range p.Hosts[0].Processes {
			if len(process.Connections) != 300/processes {
				t.Fatal("process allocation was not exact")
			}
		}
	}
	for name, mutate := range map[string]func(*Config){
		"unused slots":  func(c *Config) { c.Allocations[0].ProcessCount = 20; c.Allocations[0].MaxUIDsPerProcess = 30 },
		"too few slots": func(c *Config) { c.Allocations[0].ProcessCount = 5; c.Allocations[0].MaxUIDsPerProcess = 30 },
		"equation holds but channel group would split": func(c *Config) { c.Allocations[0].ProcessCount = 12; c.Allocations[0].MaxUIDsPerProcess = 25 },
	} {
		t.Run(name, func(t *testing.T) {
			c := membershipExample()
			mutate(&c)
			if _, err := Build(c); err == nil {
				t.Fatal("invalid fixed UID allocation accepted")
			}
		})
	}
	c := membershipExample()
	p, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	// The final dispatch audit also checks fixed allocation, independently of the input validator.
	p.Hosts[0].MaxUIDsPerProcess++
	if err := auditMembership(c, p); err == nil {
		t.Fatal("actual per-process UID mismatch passed dispatch audit")
	}
}

package scenario

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestThreeMachine200ChannelPlan(t *testing.T) {
	c := Defaults()
	p, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	if p.TotalProcesses != 42 || p.TotalConnections != 1000 || p.AudioPublishers != 400 {
		t.Fatalf("wrong totals: %+v", p)
	}
	channels := map[string]map[uint64]Connection{}
	for _, h := range p.Hosts {
		if len(h.Processes) != 14 {
			t.Fatalf("%s: expected 14 processes", h.Host)
		}
		for index, proc := range h.Processes {
			expected := c.Channels / len(h.Processes)
			if index < c.Channels%len(h.Processes) {
				expected++
			}
			if len(proc.ChannelNames) != expected {
				t.Fatalf("%s channels %d", proc.ID, len(proc.ChannelNames))
			}
			connections := expected
			if h.Host == "A" {
				connections *= 3
			}
			if len(proc.Connections) != connections {
				t.Fatalf("%s splits an A channel", proc.ID)
			}
			for _, u := range proc.Connections {
				if channels[u.Channel] == nil {
					channels[u.Channel] = map[uint64]Connection{}
				}
				if _, ok := channels[u.Channel][u.UID]; ok {
					t.Fatalf("duplicate channel/uid %s/%d", u.Channel, u.UID)
				}
				channels[u.Channel][u.UID] = u
			}
		}
	}
	if len(channels) != 200 {
		t.Fatal(len(channels))
	}
	for name, users := range channels {
		if len(users) != 5 {
			t.Fatalf("%s: users %d", name, len(users))
		}
		index, _ := strconv.Atoi(strings.TrimPrefix(name, c.ChannelPrefix+"_"))
		for _, base := range c.UIDs {
			uid := base + uint64(index-c.ChannelStart)
			if _, ok := users[uid]; !ok {
				t.Fatalf("%s misses %d", name, uid)
			}
		}
	}
}

func TestNativeArgumentsAreAudioOnlyAndExplicitPerConnection(t *testing.T) {
	c := Defaults()
	c.Token = "credential with spaces"
	c.AP = "183.131.59.146"
	c.APCert = "ap.130451.agora.local"
	p, _ := Build(c)
	for _, host := range p.Hosts {
		for _, proc := range host.Processes {
			pipes := map[string]string{}
			for _, u := range proc.Connections {
				if u.AudioSource >= 0 {
					pipes[Key(u.Channel, u.UID)] = "/tmp/pcm with spaces.fifo"
				}
			}
			args := NativeArgs(c, proc, pipes)
			sections := [][]string{{}}
			for _, arg := range args {
				if arg == "-O" {
					sections = append(sections, []string{})
				} else {
					sections[len(sections)-1] = append(sections[len(sections)-1], arg)
				}
			}
			if len(sections) != len(proc.Connections) {
				t.Fatal("wrong -O split")
			}
			for i, section := range sections {
				got := map[string]bool{}
				for _, arg := range section {
					got[arg] = true
					if strings.HasPrefix(arg, "--video-input=") || strings.HasPrefix(arg, "--dual-video-file=") {
						t.Fatal("unexpected video source")
					}
				}
				for _, required := range []string{"--token=" + c.Token, "--ap=" + c.AP, "--ap_cert=" + c.APCert, "--mute-video=1", "--v-sub-flag=0", "--a-sub-flag=1", "--dual=0"} {
					if !got[required] {
						t.Fatalf("%s misses %s", proc.ID, required)
					}
				}
				if (proc.Connections[i].AudioSource >= 0) != got["--audio-input=/tmp/pcm with spaces.fifo"] {
					t.Fatal("publisher/receiver audio mismatch")
				}
			}
		}
	}
}

func TestDelayedSenderAndValidation(t *testing.T) {
	s := AudioSchedule{Mode: "staggered", OffsetMS: 5000}
	for _, test := range []struct {
		source int
		ms     int64
		want   bool
	}{{0, -1, false}, {0, 0, true}, {1, 0, false}, {1, 4999, false}, {1, 5000, true}, {0, 9000, true}, {1, 9000, true}} {
		if got := s.Active(test.source, test.ms); got != test.want {
			t.Fatal(test, got)
		}
	}
	c := Defaults()
	c.UIDs[4] = c.UIDs[0]
	if c.Validate(false) == nil {
		t.Fatal("duplicate UID accepted")
	}
	c = Defaults()
	c.UsersPerProcessBC = 0
	if _, err := Build(c); err == nil {
		t.Fatal("zero process capacity accepted")
	}
	c = Defaults()
	c.Channels = 16
	p, _ := Build(c)
	if !reflect.DeepEqual([]int{len(p.Hosts[0].Processes[1].Connections), len(p.Hosts[1].Processes[1].Connections)}, []int{24, 8}) {
		t.Fatal("tail packing incorrect")
	}
}

func TestSDKThousandUIDLayoutAndUnsupportedMultipleServices(t *testing.T) {
	c := Defaults()
	c.Allocations = []RoleAllocation{{Role: "A", NodeID: "A", ProcessCount: 20, MaxUIDsPerProcess: 30}, {Role: "B", NodeID: "B", ProcessCount: 10, MaxUIDsPerProcess: 20}, {Role: "C", NodeID: "C", ProcessCount: 10, MaxUIDsPerProcess: 20}}
	plan, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	if plan.TotalProcesses != 40 || plan.TotalConnections != 1000 {
		t.Fatalf("incorrect SDK capacity: %+v", plan)
	}
	for _, host := range plan.Hosts {
		for _, process := range host.Processes {
			if process.Services != 1 || len(ServiceGroups(c, process)) != 1 {
				t.Fatalf("process must own one Service: %+v", process)
			}
			want := 20
			if host.Host == "A" {
				want = 30
			}
			if len(process.Connections) != want {
				t.Fatalf("%s: got %d connections", process.ID, len(process.Connections))
			}
		}
	}
	c.ServiceLayout = "multi"
	if _, err = Build(c); err == nil || !strings.Contains(err.Error(), "进程单例") {
		t.Fatalf("unsupported native singleton layout accepted: %v", err)
	}
}

func TestDistributedPublisherPlanPreservesFiveUIDChannels(t *testing.T) {
	c := Defaults()
	c.Distribution = "distributed"
	c.PublisherChannels = [3]int{60, 70, 70}
	c.Allocations = []RoleAllocation{{Role: "A", NodeID: "C", ProcessCount: 10, MaxUIDsPerProcess: 36}, {Role: "B", NodeID: "A", ProcessCount: 10, MaxUIDsPerProcess: 36}, {Role: "C", NodeID: "B", ProcessCount: 10, MaxUIDsPerProcess: 36}}
	plan, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	if plan.TotalConnections != 1000 || plan.TotalProcesses != 30 || plan.AudioPublishers != 400 {
		t.Fatalf("wrong totals: %+v", plan)
	}
	channels := map[string]map[uint64]Connection{}
	perChannelHost := map[string][]int{}
	for index, host := range plan.Hosts {
		expected := []int{320, 340, 340}[index]
		if host.Connections != expected || host.AudioPublishers != 2*c.PublisherChannels[index] || host.TargetNode() != c.Allocations[index].NodeID {
			t.Fatalf("incorrect distributed host: %+v", host)
		}
		pubs, streams := 0, 0
		for _, process := range host.Processes {
			if len(process.Connections) != expected/10 || process.Services != 1 {
				t.Fatalf("incorrect packing: %+v", process)
			}
			grouped := map[string][]Connection{}
			for _, user := range process.Connections {
				if channels[user.Channel] == nil {
					channels[user.Channel] = map[uint64]Connection{}
				}
				if _, exists := channels[user.Channel][user.UID]; exists {
					t.Fatal("duplicate UID in channel")
				}
				channels[user.Channel][user.UID] = user
				grouped[user.Channel] = append(grouped[user.Channel], user)
				if user.AudioSource >= 0 {
					pubs++
					streams++
				} else {
					streams += 2
				}
			}
			for channel, users := range grouped {
				perChannelHost[channel] = append(perChannelHost[channel], len(users))
				if len(users) != 1 && len(users) != 3 {
					t.Fatal("split channel group")
				}
				if len(users) == 3 && (users[0].AudioSource != 0 || users[1].AudioSource != 1 || users[2].AudioSource != -1) {
					t.Fatal("wrong publisher triplet")
				}
			}
		}
		if pubs != host.AudioPublishers || streams != []int{520, 540, 540}[index] {
			t.Fatal("wrong publisher/subscription totals")
		}
	}
	for name, users := range channels {
		if len(users) != 5 {
			t.Fatal("channel does not contain five UIDs")
		}
		if len(perChannelHost[name]) != 3 {
			t.Fatal("channel does not span three nodes")
		}
	}
	c.Allocations[1].MaxUIDsPerProcess = 30
	if _, err := Build(c); err == nil {
		t.Fatal("insufficient distributed capacity accepted")
	}
	c.Allocations[1].MaxUIDsPerProcess = 36
	c.PublisherChannels[2] = 69
	if _, err := Build(c); err == nil {
		t.Fatal("missing publishing channel accepted")
	}
}

func TestDistributedSmallPlanWithNoPublishersOnA(t *testing.T) {
	c := Defaults()
	c.Distribution = "distributed"
	c.Channels = 2
	c.Allocations = []RoleAllocation{{Role: "A", NodeID: "A", ProcessCount: 1, MaxUIDsPerProcess: 6}, {Role: "B", NodeID: "B", ProcessCount: 1, MaxUIDsPerProcess: 6}, {Role: "C", NodeID: "C", ProcessCount: 1, MaxUIDsPerProcess: 6}}
	p, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	if p.Hosts[0].HasPublishers() || !p.Hosts[1].HasPublishers() || !p.Hosts[2].HasPublishers() {
		t.Fatal("publisher discovery still assumes A")
	}
	if p.TotalConnections != 10 {
		t.Fatal("small model changed")
	}
}

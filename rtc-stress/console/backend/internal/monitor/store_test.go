package monitor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func number(v float64) *float64 { return &v }
func TestFiveMinuteMeansKeepRealtimeLatestAndMissingValues(t *testing.T) {
	s := NewStore()
	base := time.Now().Truncate(Interval).Add(-3 * Interval).UnixMilli()
	s.Add("A", Sample{At: base + 1000, CPU: number(40), MemoryPercent: number(10), ReceiveBPS: number(0)})
	s.Add("A", Sample{At: base + 2000, CPU: number(60), ReceiveBPS: number(100)})
	s.Add("A", Sample{At: base + 2000, CPU: number(1000)})
	s.Add("A", Sample{At: base + intervalMS + 1000, CPU: number(70)})
	series := s.Read(0)[0]
	if len(series.Samples) != 2 {
		t.Fatalf("got %d buckets", len(series.Samples))
	}
	p := series.Samples[0]
	if p.At != base+intervalMS || *p.CPU != 50 || *p.MemoryPercent != 10 || *p.ReceiveBPS != 50 {
		t.Fatalf("wrong mean: %+v", p)
	}
	if series.Latest.At != base+intervalMS+1000 || *series.Latest.CPU != 70 {
		t.Fatal("realtime latest was averaged")
	}
	if len(s.Read(base + intervalMS)[0].Samples) != 1 {
		t.Fatal("incremental history lost bucket boundary")
	}
}
func TestTwelveHourRetentionAndFiveMinuteBound(t *testing.T) {
	s := NewStore()
	end := time.Now().Truncate(Interval).UnixMilli()
	for i := 156; i >= 1; i-- {
		s.Add("B", Sample{At: end - int64(i)*intervalMS + 1000, CPU: number(float64(i))})
	}
	points := s.Read(0)[1].Samples
	if len(points) != 144 {
		t.Fatalf("want 144 points, got %d", len(points))
	}
	cutoff := time.Now().Add(-Retention).UnixMilli()
	for i, p := range points {
		if p.At <= cutoff || p.At%intervalMS != 0 {
			t.Fatal("point outside retention or interval")
		}
		if i > 0 && p.At-points[i-1].At != intervalMS {
			t.Fatal("unexpected bucket spacing")
		}
	}
}
func TestHistoryPersistsPendingSumsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.json")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Truncate(Interval).Add(-2 * Interval).UnixMilli()
	s.Add("C", Sample{At: base + 1000, CPU: number(10)})
	s.Add("C", Sample{At: base + 2000, CPU: number(20)})
	if err = s.Save(); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	restored.Add("C", Sample{At: base + 3000, CPU: number(30)})
	points := restored.Read(0)[2].Samples
	if len(points) != 1 || *points[0].CPU != 20 {
		t.Fatalf("pending sums lost or counted twice: %+v", points)
	}
}
func TestLegacyRawHistoryImportsWithoutFillingGaps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.json")
	base := time.Now().Truncate(Interval).Add(-7 * time.Hour).UnixMilli()
	raw := storeSnapshot{Series: []Series{{Node: "A", Samples: []Sample{{At: base + 1000, CPU: number(10)}, {At: base + 2000, CPU: number(30)}, {At: base + 3*intervalMS + 1000, CPU: number(70)}}}}}
	data, _ := json.Marshal(raw)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	points := s.Read(0)[0].Samples
	if len(points) != 2 || *points[0].CPU != 20 || points[1].At-points[0].At != 3*intervalMS {
		t.Fatal("legacy migration fabricated or lost history")
	}
}

func TestCustomNodeHistorySurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.json")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Add("node-east-4", Sample{At: time.Now().Add(-Interval).UnixMilli(), CPU: number(12)})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, series := range restored.Read(0) {
		if series.Node == "node-east-4" && series.Latest != nil && *series.Latest.CPU == 12 {
			return
		}
	}
	t.Fatal("custom node history missing")
}

func TestFineFiveSecondMeansDoNotInventGapsOrChangeCoarseHistory(t *testing.T) {
	s := NewStore()
	base := time.Now().Truncate(Interval).Add(-Interval).UnixMilli()
	s.Add("A", Sample{At: base + 1000, CPU: number(10), MemoryPercent: number(12), ReceiveBPS: number(0)})
	s.Add("A", Sample{At: base + 2000, CPU: number(30), ReceiveBPS: number(100)})
	s.Add("A", Sample{At: base + 21000, CPU: number(50)})
	fine := s.ReadInterval(0, FineInterval)[0]
	if len(fine.Samples) != 2 || fine.Samples[0].At != base+5000 || *fine.Samples[0].CPU != 20 || *fine.Samples[0].MemoryPercent != 12 || *fine.Samples[0].ReceiveBPS != 50 {
		t.Fatalf("bad fine means: %+v", fine)
	}
	if fine.Samples[1].At-fine.Samples[0].At != 20000 {
		t.Fatal("fine history filled a missing interval")
	}
	if fine.Latest == nil || *fine.Latest.CPU != 50 || fine.Latest.At != base+21000 {
		t.Fatal("raw latest changed")
	}
	coarse := s.Read(0)[0]
	if len(coarse.Samples) != 1 || *coarse.Samples[0].CPU != 30 {
		t.Fatal("coarse history no longer uses raw weighted samples")
	}
	if len(s.ReadInterval(base+5000, FineInterval)[0].Samples) != 1 {
		t.Fatal("incremental fine boundary incorrect")
	}
}
func TestFineHistoryIsBoundedToOneHour(t *testing.T) {
	s := NewStore()
	end := time.Now().Truncate(FineInterval).UnixMilli()
	for i := 800; i >= 1; i-- {
		s.Add("B", Sample{At: end - int64(i)*fineIntervalMS + 1000, CPU: number(float64(i))})
	}
	points := s.ReadInterval(0, FineInterval)[1].Samples
	if len(points) < 719 || len(points) > 720 {
		t.Fatalf("fine history bound incorrect: %d", len(points))
	}
	cutoff := time.Now().Add(-FineRetention).UnixMilli()
	for i, point := range points {
		if point.At <= cutoff || point.At%fineIntervalMS != 0 {
			t.Fatal("fine point outside window or grid")
		}
		if i > 0 && point.At-points[i-1].At != fineIntervalMS {
			t.Fatal("fine spacing changed")
		}
	}
}
func TestFinePendingMeansPersistAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.json")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Truncate(FineInterval).Add(-2 * FineInterval).UnixMilli()
	s.Add("C", Sample{At: base + 1000, CPU: number(10)})
	s.Add("C", Sample{At: base + 2000, CPU: number(30)})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	restored, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	restored.Add("C", Sample{At: base + 3000, CPU: number(50)})
	fine := restored.ReadInterval(0, FineInterval)[2].Samples
	if len(fine) != 1 || *fine[0].CPU != 30 {
		t.Fatal("fine pending counts were lost or duplicated")
	}
}
func TestCoarseSnapshotDoesNotBecomeFineData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.json")
	at := time.Now().Add(-10 * time.Minute).UnixMilli()
	snapshot := storeSnapshot{IntervalSeconds: 300, Series: []Series{{Node: "A", Samples: []Sample{{At: at, CPU: number(55)}}, Latest: &Sample{At: at + 1000, CPU: number(60)}}}}
	data, _ := json.Marshal(snapshot)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Read(0)[0].Samples) != 1 || len(s.ReadInterval(0, FineInterval)[0].Samples) != 0 {
		t.Fatal("coarse snapshot was lost or used as fine data")
	}
	if latest := s.ReadInterval(0, FineInterval)[0].Latest; latest == nil || *latest.CPU != 60 {
		t.Fatal("raw latest missing in fine view")
	}
}

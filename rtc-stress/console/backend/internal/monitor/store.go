package monitor

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"rtcstress/internal/scenario"
	"sort"
	"sync"
	"time"
)

const Retention = 12 * time.Hour
const Interval = 5 * time.Minute
const FineInterval = 5 * time.Second
const FineRetention = time.Hour
const fineMaxPoints = int(FineRetention / FineInterval)
const fineIntervalMS = int64(FineInterval / time.Millisecond)
const maxPoints = int(Retention / Interval)
const intervalMS = int64(Interval / time.Millisecond)

type Series struct {
	Node    string   `json:"node"`
	Samples []Sample `json:"samples"`
	Latest  *Sample  `json:"latest"`
}
type bucket struct {
	End    int64      `json:"end"`
	Sums   [8]float64 `json:"sums"`
	Counts [8]int     `json:"counts"`
	Last   Sample     `json:"last"`
}

func metrics(sample Sample) [8]*float64 {
	return [8]*float64{sample.CPU, sample.IOWait, sample.MemoryPercent, sample.MemoryUsed, sample.MemoryTotal, sample.ReceiveBPS, sample.TransmitBPS, sample.Load1}
}
func (b *bucket) add(sample Sample) {
	b.Last = sample
	for i, p := range metrics(sample) {
		if p != nil && !math.IsNaN(*p) && !math.IsInf(*p, 0) {
			b.Sums[i] += *p
			b.Counts[i]++
		}
	}
}
func (b *bucket) mean() Sample {
	result := b.Last
	result.At = b.End
	fields := []**float64{&result.CPU, &result.IOWait, &result.MemoryPercent, &result.MemoryUsed, &result.MemoryTotal, &result.ReceiveBPS, &result.TransmitBPS, &result.Load1}
	for i, p := range fields {
		*p = nil
		if b.Counts[i] > 0 {
			value := b.Sums[i] / float64(b.Counts[i])
			*p = &value
		}
	}
	return result
}

type fineSnapshot struct {
	IntervalSeconds int                `json:"intervalSeconds"`
	Series          []Series           `json:"series"`
	Pending         map[string]*bucket `json:"pending,omitempty"`
}
type storeSnapshot struct {
	Fine            *fineSnapshot      `json:"fine,omitempty"`
	IntervalSeconds int                `json:"intervalSeconds"`
	Series          []Series           `json:"series"`
	Pending         map[string]*bucket `json:"pending,omitempty"`
}
type Store struct {
	finePoints  map[string][]Sample
	finePending map[string]*bucket
	fineSavedAt int64
	mu          sync.Mutex
	points      map[string][]Sample
	latest      map[string]Sample
	pending     map[string]*bucket
	path        string
}

func NewStore() *Store {
	return &Store{finePoints: map[string][]Sample{}, finePending: map[string]*bucket{}, points: map[string][]Sample{}, latest: map[string]Sample{}, pending: map[string]*bucket{}}
}
func validNode(node string) bool { return scenario.ValidID(node) }

// OpenStore also imports the previous API's raw samples during migration.
func OpenStore(path string) (*Store, error) {
	s := NewStore()
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.path = path
			return s, nil
		}
		return nil, err
	}
	defer file.Close()
	var snapshot storeSnapshot
	if err = json.NewDecoder(file).Decode(&snapshot); err != nil {
		return nil, fmt.Errorf("read telemetry history: %w", err)
	}
	if snapshot.IntervalSeconds == int(Interval/time.Second) {
		for _, series := range snapshot.Series {
			if !validNode(series.Node) {
				continue
			}
			s.points[series.Node] = series.Samples
			if series.Latest != nil {
				s.latest[series.Node] = *series.Latest
			}
			s.trim(series.Node, time.Now().UnixMilli())
		}
		for node, pending := range snapshot.Pending {
			if validNode(node) && pending != nil && pending.End > time.Now().Add(-Retention).UnixMilli() {
				s.pending[node] = pending
			}
		}
	} else {
		for _, series := range snapshot.Series {
			if !validNode(series.Node) {
				continue
			}
			sort.Slice(series.Samples, func(i, j int) bool { return series.Samples[i].At < series.Samples[j].At })
			for _, sample := range series.Samples {
				if sample.At > time.Now().Add(-Retention).UnixMilli() {
					s.Add(series.Node, sample)
				}
			}
			if series.Latest != nil {
				s.Add(series.Node, *series.Latest)
			}
		}
	}
	if fine := snapshot.Fine; fine != nil && fine.IntervalSeconds == int(FineInterval/time.Second) {
		now := time.Now().UnixMilli()
		for _, series := range fine.Series {
			if !validNode(series.Node) {
				continue
			}
			s.finePoints[series.Node] = series.Samples
			s.trimFine(series.Node, now)
		}
		for node, pending := range fine.Pending {
			if validNode(node) && pending != nil && pending.End > now-int64(FineRetention/time.Millisecond) {
				s.finePending[node] = pending
			}
		}
	}

	s.path = path
	if err = s.Save(); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Store) trim(node string, now int64) {
	points := s.points[node]
	cutoff := now - int64(Retention/time.Millisecond)
	first := sort.Search(len(points), func(i int) bool { return points[i].At > cutoff })
	if len(points)-first > maxPoints {
		first = len(points) - maxPoints
	}
	if first > 0 {
		s.points[node] = append([]Sample(nil), points[first:]...)
	}
}
func (s *Store) Add(node string, sample Sample) {
	if !validNode(node) {
		return
	}
	if sample.At <= 0 {
		sample.At = time.Now().UnixMilli()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous, ok := s.latest[node]; ok && sample.At <= previous.At {
		return
	}
	end := (sample.At/intervalMS + 1) * intervalMS
	pending := s.pending[node]
	completed := pending != nil && end > pending.End
	if completed {
		s.points[node] = append(s.points[node], pending.mean())
		pending = nil
	}
	if pending == nil {
		pending = &bucket{End: end}
		s.pending[node] = pending
	}
	pending.add(sample)
	fineCompleted := s.addFineLocked(node, sample)
	s.latest[node] = sample
	s.trim(node, sample.At)
	if completed || (fineCompleted && time.Now().UnixMilli()-s.fineSavedAt >= 30000) {
		if err := s.saveLocked(); err != nil {
			log.Printf("保存监控历史失败: %v", err)
		}
	}
}
func (s *Store) readLocked(after int64, includePending bool) []Series {
	now := time.Now().UnixMilli()
	cutoff := now - int64(Retention/time.Millisecond)
	if after < cutoff {
		after = cutoff
	}
	nodeSet := map[string]bool{"A": true, "B": true, "C": true}
	for node := range s.latest {
		nodeSet[node] = true
	}
	for node := range s.points {
		nodeSet[node] = true
	}
	for node := range s.pending {
		nodeSet[node] = true
	}
	ids := make([]string, 0, len(nodeSet))
	for node := range nodeSet {
		ids = append(ids, node)
	}
	sort.Strings(ids)
	result := make([]Series, 0, len(ids))
	for _, node := range ids {
		series := Series{Node: node, Samples: []Sample{}}
		if latest, ok := s.latest[node]; ok {
			copy := latest
			series.Latest = &copy
		}
		for _, point := range s.points[node] {
			if point.At > after && point.At <= now {
				series.Samples = append(series.Samples, point)
			}
		}
		if p := s.pending[node]; includePending && p != nil && p.End <= now && p.End > after {
			series.Samples = append(series.Samples, p.mean())
		}
		if len(series.Samples) > maxPoints {
			series.Samples = series.Samples[len(series.Samples)-maxPoints:]
		}
		result = append(result, series)
	}
	return result
}
func (s *Store) Read(after int64) []Series {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked(after, true)
}
func (s *Store) Save() error { s.mu.Lock(); defer s.mu.Unlock(); return s.saveLocked() }
func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	snapshot := storeSnapshot{IntervalSeconds: int(Interval / time.Second), Series: s.readLocked(0, false), Pending: map[string]*bucket{}, Fine: &fineSnapshot{IntervalSeconds: int(FineInterval / time.Second), Series: s.readFineLocked(0, false), Pending: map[string]*bucket{}}}
	for node, pending := range s.finePending {
		if pending.End > time.Now().Add(-FineRetention).UnixMilli() {
			snapshot.Fine.Pending[node] = pending
		}
	}
	for node, p := range s.pending {
		if p.End > time.Now().Add(-Retention).UnixMilli() {
			snapshot.Pending[node] = p
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(s.path), "telemetry-*.tmp")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err = json.NewEncoder(file).Encode(snapshot); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, s.path); err != nil {
		return err
	}
	s.fineSavedAt = time.Now().UnixMilli()
	return nil
}

func (s *Store) trimFine(node string, now int64) {
	points := s.finePoints[node]
	cutoff := now - int64(FineRetention/time.Millisecond)
	first := sort.Search(len(points), func(i int) bool { return points[i].At > cutoff })
	if len(points)-first > fineMaxPoints {
		first = len(points) - fineMaxPoints
	}
	if first > 0 {
		s.finePoints[node] = append([]Sample(nil), points[first:]...)
	}
}
func (s *Store) addFineLocked(node string, sample Sample) bool {
	end := (sample.At/fineIntervalMS + 1) * fineIntervalMS
	pending := s.finePending[node]
	completed := pending != nil && end > pending.End
	if completed {
		s.finePoints[node] = append(s.finePoints[node], pending.mean())
		pending = nil
	}
	if pending == nil {
		pending = &bucket{End: end}
		s.finePending[node] = pending
	}
	pending.add(sample)
	s.trimFine(node, sample.At)
	return completed
}
func (s *Store) readFineLocked(after int64, includePending bool) []Series {
	now := time.Now().UnixMilli()
	cutoff := now - int64(FineRetention/time.Millisecond)
	if after < cutoff {
		after = cutoff
	}
	ids := map[string]bool{"A": true, "B": true, "C": true}
	for node := range s.latest {
		ids[node] = true
	}
	for node := range s.finePoints {
		ids[node] = true
	}
	for node := range s.finePending {
		ids[node] = true
	}
	ordered := make([]string, 0, len(ids))
	for node := range ids {
		ordered = append(ordered, node)
	}
	sort.Strings(ordered)
	result := make([]Series, 0, len(ordered))
	for _, node := range ordered {
		series := Series{Node: node, Samples: []Sample{}}
		if latest, ok := s.latest[node]; ok {
			copy := latest
			series.Latest = &copy
		}
		for _, point := range s.finePoints[node] {
			if point.At > after && point.At <= now {
				series.Samples = append(series.Samples, point)
			}
		}
		if pending := s.finePending[node]; includePending && pending != nil && pending.End <= now && pending.End > after {
			series.Samples = append(series.Samples, pending.mean())
		}
		if len(series.Samples) > fineMaxPoints {
			series.Samples = series.Samples[len(series.Samples)-fineMaxPoints:]
		}
		result = append(result, series)
	}
	return result
}
func (s *Store) ReadInterval(after int64, interval time.Duration) []Series {
	s.mu.Lock()
	defer s.mu.Unlock()
	if interval == FineInterval {
		return s.readFineLocked(after, true)
	}
	return s.readLocked(after, true)
}

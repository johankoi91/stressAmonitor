package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Sample struct {
	At            int64    `json:"at"`
	CPU           *float64 `json:"cpu"`
	IOWait        *float64 `json:"ioWait"`
	MemoryPercent *float64 `json:"memoryPercent"`
	MemoryUsed    *float64 `json:"memoryUsed"`
	MemoryTotal   *float64 `json:"memoryTotal"`
	// Network rates are bits per second, converted from Netdata kilobits/s.
	ReceiveBPS     *float64 `json:"receiveBps"`
	TransmitBPS    *float64 `json:"transmitBps"`
	Load1          *float64 `json:"load1"`
	NetdataVersion string   `json:"netdataVersion,omitempty"`
	Status         string   `json:"status"`
	Error          string   `json:"error,omitempty"`
}

type Collector struct {
	base     string
	client   *http.Client
	version  string
	lastInfo time.Time
}

func NewCollector(base string) *Collector {
	if base == "" {
		base = "http://127.0.0.1:19999"
	}
	return &Collector{base: strings.TrimRight(base, "/"), client: &http.Client{Timeout: 1800 * time.Millisecond}}
}

type chartData struct {
	Labels []string     `json:"labels"`
	Data   [][]*float64 `json:"data"`
}

func (c *Collector) get(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+path, nil)
	if err != nil {
		return err
	}
	response, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("Netdata HTTP %d", response.StatusCode)
	}
	return json.NewDecoder(response.Body).Decode(target)
}
func (c *Collector) chart(ctx context.Context, id string) (map[string]float64, error) {
	query := url.Values{"chart": {id}, "format": {"json"}, "after": {"-3"}, "points": {"3"}, "group": {"average"}, "options": {"seconds"}}
	var chart chartData
	if err := c.get(ctx, "/api/v1/data?"+query.Encode(), &chart); err != nil {
		return nil, err
	}
	var newest []*float64
	for _, row := range chart.Data {
		if len(row) > 1 && row[0] != nil && (newest == nil || *row[0] > *newest[0]) {
			newest = row
		}
	}
	if newest == nil {
		return nil, errors.New("Netdata 尚无采样数据")
	}
	values := map[string]float64{}
	for i, label := range chart.Labels {
		if i > 0 && i < len(newest) && newest[i] != nil && !math.IsNaN(*newest[i]) && !math.IsInf(*newest[i], 0) {
			values[label] = *newest[i]
		}
	}
	if len(values) == 0 {
		return nil, errors.New("Netdata 正在初始化采集")
	}
	return values, nil
}
func value(v float64) *float64 { return &v }
func (c *Collector) Read(ctx context.Context) Sample {
	s := Sample{At: time.Now().UTC().UnixMilli(), Status: "ok"}
	if c.version == "" || time.Since(c.lastInfo) > 5*time.Minute {
		var info struct {
			Version string `json:"version"`
		}
		if c.get(ctx, "/api/v1/info", &info) == nil {
			c.version = info.Version
			c.lastInfo = time.Now()
		}
	}
	s.NetdataVersion = c.version
	types := []string{"system.cpu", "system.ram", "system.net", "system.load"}
	data := make([]map[string]float64, len(types))
	errs := make([]error, len(types))
	var wg sync.WaitGroup
	for i, id := range types {
		wg.Add(1)
		go func(i int, id string) { defer wg.Done(); data[i], errs[i] = c.chart(ctx, id) }(i, id)
	}
	wg.Wait()
	if idle, ok := data[0]["idle"]; ok {
		wait := data[0]["iowait"]
		s.CPU = value(math.Max(0, math.Min(100, 100-idle-wait)))
		s.IOWait = value(wait)
	}
	if s.CPU == nil && len(data[0]) > 0 {
		// Idle is hidden by default in some Netdata API versions.
		busy := 0.0
		for name, amount := range data[0] {
			if name != "idle" && name != "iowait" {
				busy += amount
			}
		}
		s.CPU = value(math.Max(0, math.Min(100, busy)))
		s.IOWait = value(data[0]["iowait"])
	}
	if used, ok := data[1]["used"]; ok {
		total := 0.0
		for _, v := range data[1] {
			total += v
		}
		if total > 0 {
			s.MemoryUsed = value(used * 1024 * 1024)
			s.MemoryTotal = value(total * 1024 * 1024)
			s.MemoryPercent = value(used / total * 100)
		}
	}
	if received, ok := data[2]["received"]; ok {
		s.ReceiveBPS = value(math.Abs(received) * 1000)
	}
	if sent, ok := data[2]["sent"]; ok {
		s.TransmitBPS = value(math.Abs(sent) * 1000)
	}
	if load, ok := data[3]["load1"]; ok {
		s.Load1 = value(load)
	}
	messages := []string{}
	for i, err := range errs {
		if err != nil {
			messages = append(messages, types[i]+": "+err.Error())
		}
	}
	if s.CPU == nil || s.MemoryPercent == nil || s.ReceiveBPS == nil || s.TransmitBPS == nil {
		s.Status = "partial"
		if s.CPU == nil && s.MemoryPercent == nil && s.ReceiveBPS == nil {
			s.Status = "unavailable"
		}
	}
	if len(messages) > 0 {
		s.Error = strings.Join(messages, "; ")
	}
	return s
}
func (c *Collector) Run(ctx context.Context, consume func(Sample)) {
	for ctx.Err() == nil {
		started := time.Now()
		consume(c.Read(ctx))
		wait := time.Second - time.Since(started)
		if wait < 50*time.Millisecond {
			wait = 50 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

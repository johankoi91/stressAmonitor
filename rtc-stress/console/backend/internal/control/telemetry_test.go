package control

import (
	"encoding/json"
	"net/http/httptest"
	"rtcstress/internal/monitor"
	"testing"
	"time"
)

func TestTelemetryAPIExposesTwelveHourFiveMinuteHistory(t *testing.T) {
	hub := NewHub(nil, time.Second)
	defer hub.Close()
	value := 42.0
	at := time.Now().Add(-7 * time.Hour).UnixMilli()
	hub.Telemetry.Add("A", monitor.Sample{At: at, CPU: &value})
	manager, err := NewManager(t.TempDir(), inertTransport{})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	handler := (&HTTP{Hub: hub, Manager: manager}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/telemetry?after=0", nil))
	var result struct {
		Retention int              `json:"retentionSeconds"`
		Interval  int              `json:"intervalSeconds"`
		Series    []monitor.Series `json:"series"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil {
		t.Fatal("invalid telemetry response")
	}
	if result.Retention != 43200 || result.Interval != 300 || len(result.Series[0].Samples) != 1 {
		t.Fatalf("history still restricted: %+v", result)
	}
}

func TestTelemetryAPISwitchesToFiveSecondOneHourHistory(t *testing.T) {
	hub := NewHub(nil, time.Second)
	defer hub.Close()
	value := 42.0
	hub.Telemetry.Add("A", monitor.Sample{At: time.Now().Add(-7 * time.Hour).UnixMilli(), CPU: &value})
	base := time.Now().Truncate(monitor.FineInterval).Add(-3 * monitor.FineInterval).UnixMilli()
	hub.Telemetry.Add("A", monitor.Sample{At: base + 1000, CPU: &value})
	hub.Telemetry.Add("A", monitor.Sample{At: base + 6000, CPU: &value})
	manager, err := NewManager(t.TempDir(), inertTransport{})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	handler := (&HTTP{Hub: hub, Manager: manager}).Handler()
	for _, test := range []struct {
		query               string
		interval, retention int
	}{{"?interval=5&after=0", 5, 3600}, {"?interval=300&after=0", 300, 43200}} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/telemetry"+test.query, nil))
		var result struct {
			Interval  int              `json:"intervalSeconds"`
			Retention int              `json:"retentionSeconds"`
			Series    []monitor.Series `json:"series"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Interval != test.interval || result.Retention != test.retention {
			t.Fatal("wrong telemetry resolution", response.Body.String())
		}
		if test.interval == 5 {
			if len(result.Series[0].Samples) != 2 {
				t.Fatal("fine data missing or includes old coarse data")
			}
			for _, point := range result.Series[0].Samples {
				if point.At%5000 != 0 {
					t.Fatal("not on five-second grid")
				}
			}
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/telemetry?interval=30", nil))
	if response.Code != 400 {
		t.Fatal("unsupported resolution accepted")
	}
}

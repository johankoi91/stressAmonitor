package control

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rtcstress/internal/scenario"
	"rtcstress/internal/wire"
	"rtcstress/internal/worker"
)

type inertTransport struct{}

func (inertTransport) Check(context.Context, scenario.Host, worker.Request) error { return nil }
func (inertTransport) Start(context.Context, scenario.Host, worker.Request, func(worker.Event)) (wire.TaskSession, error) {
	panic("preview must never launch a process")
}

func TestAPISettingsDisplayTokenAndPreviewRedactsIt(t *testing.T) {
	m, err := NewManager(t.TempDir(), inertTransport{})
	if err != nil {
		t.Fatal(err)
	}
	s := Settings{Config: scenario.Defaults(), Hosts: scenario.DefaultHosts()}
	s.Config.Token = "private-rtc-token"
	for i := range s.Hosts {
		s.Hosts[i].Address = []string{"114.132.190.201", "129.204.144.125", "159.75.94.60"}[i]
	}
	if err = m.Update(s); err != nil {
		t.Fatal(err)
	}
	h := (&HTTP{Manager: m}).Handler()
	request := func(method, path string, body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	settings := request("GET", "/api/settings", nil)
	if settings.Code != 200 || !strings.Contains(settings.Body.String(), s.Config.Token) {
		t.Fatal("saved Token/AppId is unavailable to the settings form", settings.Code)
	}
	for _, path := range []string{"/api/tasks", "/api/nodes"} {
		if got := request("GET", path, nil).Code; got != 200 {
			t.Fatalf("%s requires login: %d", path, got)
		}
	}
	dataSettings, _ := json.Marshal(s)
	if got := request("PUT", "/api/settings", dataSettings).Code; got != 200 {
		t.Fatalf("saving configuration requires login: %d", got)
	}
	if got := request("POST", "/api/login", []byte(`{}`)).Code; got != 404 {
		t.Fatalf("obsolete login route remains: %d", got)
	}
	data, _ := json.Marshal(s.Config)
	preview := request("POST", "/api/plan", data)
	if preview.Code != 200 || !strings.Contains(preview.Body.String(), `"totalProcesses":42`) {
		t.Fatal(preview.Body.String())
	}
	if strings.Contains(preview.Body.String(), s.Config.Token) {
		t.Fatal("preview exposes credential")
	}
	csrf := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewReader([]byte(`{}`)))
	csrf.Header.Set("Origin", "https://elsewhere.invalid")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, csrf)
	if w.Code != 403 {
		t.Fatal("cross-origin mutation accepted")
	}
}

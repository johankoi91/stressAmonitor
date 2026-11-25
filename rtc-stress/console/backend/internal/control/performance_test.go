package control

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"rtcstress/internal/scenario"
	"rtcstress/internal/wire"
)

func performanceManager(t *testing.T) *Manager {
	t.Helper()
	manager, err := NewManager(t.TempDir(), inertTransport{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	config := scenario.Defaults()
	config.Token = "must-not-leak"
	plan, err := scenario.Build(config)
	if err != nil {
		t.Fatal(err)
	}
	manager.tasks["history-one"] = &runState{task: Task{ID: "history-one", Name: "history", State: "completed", CreatedAt: time.Now().UTC(), Config: config, Plan: plan}}
	return manager
}
func decodedBody(t *testing.T, response *httptest.ResponseRecorder) []byte {
	t.Helper()
	if response.Header().Get("Content-Encoding") != "gzip" {
		return response.Body.Bytes()
	}
	reader, err := gzip.NewReader(bytes.NewReader(response.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestJSONCompressionAndGzipOptOut(t *testing.T) {
	handler := (&HTTP{Manager: performanceManager(t)}).Handler()
	for _, encoding := range []string{"gzip", "br, gzip", "gzip;q=0", "gzip;q=0.0", "identity"} {
		t.Run(encoding, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/api/tasks", nil)
			request.Header.Set("Accept-Encoding", encoding)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			want := encoding == "gzip" || encoding == "br, gzip"
			if (response.Header().Get("Content-Encoding") == "gzip") != want {
				t.Fatal("incorrect encoding", response.Header())
			}
			var tasks []Task
			if err := json.Unmarshal(decodedBody(t, response), &tasks); err != nil {
				t.Fatal(err)
			}
			if len(tasks) != 1 || tasks[0].Plan.TotalConnections != 1000 {
				t.Fatal("corrupt compressed JSON")
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private API became cacheable")
			}
		})
	}
}
func TestStaticCompressionCachingRevalidationAndRanges(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	source := []byte(strings.Repeat("const app = 'compressed RTC console';\n", 1000))
	asset := filepath.Join(directory, "assets", "app-deadbeef.js")
	if err := os.WriteFile(asset, source, 0600); err != nil {
		t.Fatal(err)
	}
	var packed bytes.Buffer
	writer := gzip.NewWriter(&packed)
	_, _ = writer.Write(source)
	_ = writer.Close()
	if err := os.WriteFile(asset+".gz", packed.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "index.html"), []byte("<!doctype html><title>console</title>"), 0600); err != nil {
		t.Fatal(err)
	}
	handler := (&HTTP{Manager: performanceManager(t), FrontendDir: directory}).Handler()
	request := httptest.NewRequest("GET", "/assets/app-deadbeef.js", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 || !bytes.Equal(decodedBody(t, response), source) || !bytes.Equal(response.Body.Bytes(), packed.Bytes()) {
		t.Fatal("precompressed asset corrupted or recompressed")
	}
	if response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || !strings.Contains(response.Header().Get("Content-Type"), "javascript") || response.Header().Get("ETag") == "" {
		t.Fatal("missing static asset cache headers", response.Header())
	}
	request = httptest.NewRequest("HEAD", "/assets/app-deadbeef.js", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	head := httptest.NewRecorder()
	handler.ServeHTTP(head, request)
	if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("HEAD differs from GET headers", head.Header())
	}
	request = httptest.NewRequest("GET", "/assets/app-deadbeef.js", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	request.Header.Set("If-None-Match", response.Header().Get("ETag"))
	cached := httptest.NewRecorder()
	handler.ServeHTTP(cached, request)
	if cached.Code != 304 || cached.Body.Len() != 0 {
		t.Fatal("ETag revalidation failed")
	}
	request = httptest.NewRequest("GET", "/assets/app-deadbeef.js", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	request.Header.Set("Range", "bytes=0-12")
	ranged := httptest.NewRecorder()
	handler.ServeHTTP(ranged, request)
	if ranged.Code != 206 || ranged.Header().Get("Content-Encoding") != "" || !bytes.Equal(ranged.Body.Bytes(), source[:13]) {
		t.Fatal("range response changed")
	}
	html := httptest.NewRecorder()
	handler.ServeHTTP(html, httptest.NewRequest("GET", "/", nil))
	if html.Header().Get("Cache-Control") != "no-cache" {
		t.Fatal("HTML must revalidate")
	}
	changed := httptest.NewRequest("GET", "/", nil)
	changed.Header.Set("If-None-Match", html.Header().Get("ETag"))
	htmlCached := httptest.NewRecorder()
	handler.ServeHTTP(htmlCached, changed)
	if htmlCached.Code != 304 {
		t.Fatal("HTML revalidation failed")
	}
}
func TestCompressionDoesNotInterceptWebSocketUpgrade(t *testing.T) {
	hub := NewHub([]string{"B"}, time.Second)
	defer hub.Close()
	server := httptest.NewServer((&HTTP{Manager: performanceManager(t), Hub: hub}).Handler())
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/agent?node=B"
	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Accept-Encoding": []string{"gzip"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(wire.Frame{Type: "hello", Node: "B", BootID: "test-compression", Version: wire.Version, WorkDir: "/tmp"}); err != nil {
		t.Fatal(err)
	}
	var response wire.Frame
	if err := conn.ReadJSON(&response); err != nil || response.Type != "welcome" {
		t.Fatal("WebSocket no longer works", err)
	}
}

func TestBootstrapContainsInitialDataInOnePrivateResponse(t *testing.T) {
	handler := (&HTTP{Manager: performanceManager(t)}).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/bootstrap", nil))
	var value map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"settings", "nodes", "audio", "hasToken"} {
		if _, ok := value[key]; !ok {
			t.Fatal("missing bootstrap field", key)
		}
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("bootstrap became cacheable")
	}
}

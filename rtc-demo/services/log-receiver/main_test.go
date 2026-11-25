package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSDKUploadAndExactResponse(t *testing.T) {
	dir := t.TempDir()
	handler := (&receiver{dir: dir}).handler()
	body := []byte(`{"sdk_version":"4.24.9","process_id":"sdk-test-process","payload":"[{\"log_level\":\"info\",\"message\":\"sdk upload marker\"}]"}`)
	request := httptest.NewRequest("POST", "/upload/v1", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://111.230.192.59:8800")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 || response.Body.String() != "OK" {
		t.Fatalf("SDK response: %d %q", response.Code, response.Body.String())
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("CORS missing")
	}
	id := response.Header().Get("X-Capture-Id")
	if id == "" {
		t.Fatal("capture ID missing")
	}
	raw, err := os.ReadFile(filepath.Join(dir, id+".raw"))
	if err != nil || !bytes.Equal(raw, body) {
		t.Fatalf("raw not preserved: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta receipt
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Entries != 1 || meta.SDKVersion != "4.24.9" || meta.ProcessID != "sdk-test-process" {
		t.Fatalf("metadata: %+v", meta)
	}
	readable, err := os.ReadFile(filepath.Join(dir, id+".readable.txt"))
	if err != nil || !strings.Contains(string(readable), "sdk upload marker") {
		t.Fatalf("readable log: %v", err)
	}
	for _, path := range []string{"/", "/data", "/" + id + ".raw"} {
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest("GET", path, nil))
		if res.Code != 404 {
			t.Fatalf("capture exposed at %s", path)
		}
	}
}

func TestPreflightAndGzip(t *testing.T) {
	dir := t.TempDir()
	handler := (&receiver{dir: dir}).handler()
	request := httptest.NewRequest("OPTIONS", "/upload/v1", nil)
	request.Header.Set("Access-Control-Request-Headers", "content-type,x-test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 204 || response.Header().Get("Access-Control-Allow-Headers") != "content-type,x-test" {
		t.Fatal(response.Code, response.Header())
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write([]byte(`{"payload":"[{\"message\":\"gzip marker\"}]"}`))
	_ = writer.Close()
	request = httptest.NewRequest("POST", "/upload/v1", bytes.NewReader(compressed.Bytes()))
	request.Header.Set("Content-Encoding", "gzip")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	id := response.Header().Get("X-Capture-Id")
	body, _ := os.ReadFile(filepath.Join(dir, id+".raw"))
	if !bytes.Equal(body, compressed.Bytes()) {
		t.Fatal("gzip raw changed")
	}
	body, _ = os.ReadFile(filepath.Join(dir, id+".decoded.json"))
	if !strings.Contains(string(body), "gzip marker") {
		t.Fatal(string(body))
	}
}

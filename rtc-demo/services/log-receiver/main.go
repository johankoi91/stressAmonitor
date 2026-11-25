package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const maxBody = 32 << 20

type receiver struct{ dir string }
type receipt struct {
	ID          string      `json:"id"`
	ReceivedAt  string      `json:"receivedAt"`
	RemoteAddr  string      `json:"remoteAddr"`
	Method      string      `json:"method"`
	Path        string      `json:"path"`
	ServerName  string      `json:"serverName,omitempty"`
	Headers     http.Header `json:"headers"`
	Bytes       int         `json:"bytes"`
	SHA256      string      `json:"sha256"`
	SDKVersion  string      `json:"sdkVersion,omitempty"`
	ProcessID   string      `json:"processId,omitempty"`
	Entries     int         `json:"entries"`
	DecodeError string      `json:"decodeError,omitempty"`
}

func main() {
	listen := flag.String("listen", ":6444", "HTTPS listen address")
	cert := flag.String("cert", "/certs/server.pem", "TLS certificate")
	key := flag.String("key", "/certs/server-key.pem", "TLS private key")
	data := flag.String("data", "/data", "Persistent capture directory")
	health := flag.Bool("healthcheck", false, "Check the local HTTPS listener")
	flag.Parse()
	if *health {
		client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
		response, err := client.Get("https://127.0.0.1" + *listen + "/healthz")
		if err != nil {
			log.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	if err := os.MkdirAll(*data, 0700); err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: *listen, Handler: (&receiver{dir: *data}).handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("RTC SDK log receiver HTTPS %s; captures=%s", *listen, *data)
	if err := server.ListenAndServeTLS(*cert, *key); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func (s *receiver) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	mux.HandleFunc("/upload/v1", s.upload)
	return mux
}

func (s *receiver) upload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	requested := r.Header.Get("Access-Control-Request-Headers")
	if requested == "" {
		requested = "Content-Type, Accept, Content-Encoding"
	}
	w.Header().Set("Access-Control-Allow-Headers", requested)
	w.Header().Set("Access-Control-Max-Age", "600")
	w.Header().Set("Vary", "Origin, Access-Control-Request-Headers")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		http.Error(w, "POST required", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			http.Error(w, "body too large", 413)
		} else {
			http.Error(w, "failed to read body", 400)
		}
		return
	}
	now := time.Now().UTC()
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		http.Error(w, "capture ID unavailable", 500)
		return
	}
	id := now.Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(random)
	sum := sha256.Sum256(body)
	meta := receipt{ID: id, ReceivedAt: now.Format(time.RFC3339Nano), RemoteAddr: r.RemoteAddr, Method: r.Method, Path: r.URL.RequestURI(), Headers: r.Header.Clone(), Bytes: len(body), SHA256: hex.EncodeToString(sum[:])}
	if r.TLS != nil {
		meta.ServerName = r.TLS.ServerName
	}
	decoded, decodeErr := decodeBody(body, r.Header.Get("Content-Encoding"))
	if decodeErr != nil {
		meta.DecodeError = decodeErr.Error()
	}
	if object, ok := decoded.(map[string]any); ok {
		meta.SDKVersion, _ = object["sdk_version"].(string)
		meta.ProcessID, _ = object["process_id"].(string)
		if entries, ok := object["payload"].([]any); ok {
			meta.Entries = len(entries)
		}
	}
	normalized, _ := json.MarshalIndent(decoded, "", "  ")
	metadata, _ := json.MarshalIndent(meta, "", "  ")
	files := map[string][]byte{".raw": body, ".request.json": metadata}
	if decodeErr == nil {
		files[".decoded.json"] = append(normalized, '\n')
		files[".readable.txt"] = readable(meta, decoded)
	}
	for suffix, content := range files {
		if err := os.WriteFile(filepath.Join(s.dir, id+suffix), content, 0600); err != nil {
			log.Printf("capture save failed: %v", err)
			http.Error(w, "capture save failed", 500)
			return
		}
	}
	log.Printf("captured id=%s bytes=%d sdk=%s process=%s entries=%d decode_error=%q", id, len(body), meta.SDKVersion, meta.ProcessID, meta.Entries, meta.DecodeError)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Capture-Id", id)
	w.Header().Set("Access-Control-Expose-Headers", "X-Capture-Id")
	_, _ = io.WriteString(w, "OK")
}

func decodeBody(body []byte, encoding string) (any, error) {
	data := body
	if strings.Contains(strings.ToLower(encoding), "gzip") || (len(body) > 1 && body[0] == 0x1f && body[1] == 0x8b) {
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		data, err = io.ReadAll(io.LimitReader(reader, maxBody+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxBody {
			return nil, errors.New("decoded body too large")
		}
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if object, ok := value.(map[string]any); ok {
		if payload, ok := object["payload"].(string); ok {
			var nested any
			nestedDecoder := json.NewDecoder(strings.NewReader(payload))
			nestedDecoder.UseNumber()
			if err := nestedDecoder.Decode(&nested); err == nil {
				object["payload"] = nested
			}
		}
	}
	return value, nil
}

func readable(meta receipt, value any) []byte {
	var out strings.Builder
	fmt.Fprintf(&out, "Capture: %s\nReceived: %s\nSDK: %s\nProcess: %s\nEntries: %d\nSHA-256: %s\n\n", meta.ID, meta.ReceivedAt, meta.SDKVersion, meta.ProcessID, meta.Entries, meta.SHA256)
	object, ok := value.(map[string]any)
	if !ok {
		data, _ := json.MarshalIndent(value, "", "  ")
		return append([]byte(out.String()), data...)
	}
	entries, ok := object["payload"].([]any)
	if !ok {
		data, _ := json.MarshalIndent(value, "", "  ")
		return append([]byte(out.String()), data...)
	}
	for i, entry := range entries {
		data, _ := json.MarshalIndent(entry, "", "  ")
		fmt.Fprintf(&out, "--- entry %d ---\n%s\n\n", i+1, data)
	}
	return []byte(out.String())
}

package control

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"rtcstress/internal/monitor"
	"rtcstress/internal/scenario"
)

type HTTP struct {
	Manager     *Manager
	FrontendDir string
	Hub         *Hub
}

func (h *HTTP) Handler() http.Handler {
	mux := http.NewServeMux()
	if h.Hub != nil {
		mux.Handle("GET /ws/agent", h.Hub)
		mux.HandleFunc("GET /api/telemetry", func(w http.ResponseWriter, r *http.Request) {
			interval, retention := monitor.Interval, monitor.Retention
			if raw := r.URL.Query().Get("interval"); raw != "" {
				switch raw {
				case "5":
					interval, retention = monitor.FineInterval, monitor.FineRetention
				case "300":
				default:
					reply(w, 400, map[string]string{"error": "interval 仅支持 5 或 300 秒"})
					return
				}
			}
			after := time.Now().Add(-retention).UnixMilli()
			if raw := r.URL.Query().Get("after"); raw != "" {
				value, err := strconv.ParseInt(raw, 10, 64)
				if err != nil || value < 0 {
					reply(w, 400, map[string]string{"error": "after 须为毫秒时间戳"})
					return
				}
				if value > after {
					after = value
				}
			}
			w.Header().Set("Cache-Control", "no-store")
			reply(w, 200, map[string]any{"series": h.Hub.Telemetry.ReadInterval(after, interval), "at": time.Now().UTC().UnixMilli(), "retentionSeconds": int(retention / time.Second), "intervalSeconds": int(interval / time.Second)})
		})
	}
	mux.HandleFunc("GET /api/audio", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, h.Manager.audio.List()) })
	mux.HandleFunc("POST /api/audio", h.Manager.audio.Upload)
	mux.HandleFunc("GET /api/audio/{id}/content", h.Manager.audio.Content)
	mux.HandleFunc("GET /api/nodes", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, h.Manager.Nodes())
	})
	mux.HandleFunc("POST /api/nodes/{id}/reconnect", func(w http.ResponseWriter, r *http.Request) {
		cluster, ok := h.Manager.transport.(interface{ Disconnect(string) error })
		if !ok {
			reply(w, 400, map[string]string{"error": "当前模式不支持重连"})
			return
		}
		if err := cluster.Disconnect(r.PathValue("id")); err != nil {
			reply(w, 400, map[string]string{"error": err.Error()})
			return
		}
		reply(w, 202, map[string]string{"message": "已请求节点重新连接并同步任务"})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]any{"ok": true, "service": "rtc-stress-console"})
	})
	mux.HandleFunc("GET /api/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		settings, has := h.Manager.Settings()
		reply(w, 200, map[string]any{"settings": settings, "hasToken": has, "nodes": h.Manager.Nodes(), "audio": h.Manager.audio.List(), "currentTaskId": h.Manager.CurrentID()})
	})
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		settings, has := h.Manager.Settings()
		reply(w, 200, map[string]any{"settings": settings, "hasToken": has})
	})
	mux.HandleFunc("PUT /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var settings Settings
		if !decode(w, r, &settings) {
			return
		}
		if err := h.Manager.Update(settings); err != nil {
			reply(w, 400, map[string]string{"error": err.Error()})
			return
		}
		reply(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/plan", func(w http.ResponseWriter, r *http.Request) {
		var config scenario.Config
		if !decode(w, r, &config) {
			return
		}
		config, err := h.Manager.audio.Normalize(config)
		if err != nil {
			reply(w, 400, map[string]string{"error": err.Error()})
			return
		}
		plan, err := scenario.Build(config)
		if err != nil {
			reply(w, 400, map[string]string{"error": err.Error()})
			return
		}
		reply(w, 200, plan)
	})
	mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, h.Manager.Tasks()) })
	mux.HandleFunc("GET /api/tasks/current", func(w http.ResponseWriter, r *http.Request) {
		if task, ok := h.Manager.Current(); ok {
			reply(w, 200, task)
		} else {
			reply(w, 200, nil)
		}
	})
	mux.HandleFunc("POST /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		var config scenario.Config
		if !decode(w, r, &config) {
			return
		}
		task, err := h.Manager.Create(config)
		if err != nil {
			reply(w, 400, map[string]string{"error": err.Error()})
			return
		}
		reply(w, 202, task)
	})
	mux.HandleFunc("GET /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		task, ok := h.Manager.Get(r.PathValue("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		reply(w, 200, task)
	})
	mux.HandleFunc("POST /api/tasks/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		if err := h.Manager.Stop(r.PathValue("id")); err != nil {
			reply(w, 404, map[string]string{"error": err.Error()})
			return
		}
		reply(w, 202, map[string]bool{"ok": true})
	})
	for _, action := range []string{"check"} {
		action := action
		mux.HandleFunc("POST /api/hosts/{id}/"+action, func(w http.ResponseWriter, r *http.Request) {
			output, err := h.Manager.HostAction(r.Context(), r.PathValue("id"))
			if err != nil {
				reply(w, 400, map[string]string{"error": err.Error(), "output": output})
				return
			}
			reply(w, 200, map[string]string{"message": output})
		})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(405)
			return
		}
		if h.FrontendDir == "" {
			reply(w, 200, map[string]string{"message": "RTC Stress Console API", "frontend": "请运行独立的 frontend React 工程"})
			return
		}
		clean := filepath.Clean("/" + r.URL.Path)
		path := filepath.Join(h.FrontendDir, clean)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			serveFrontend(w, r, path, strings.HasPrefix(clean, "/assets/"))
			return
		}
		serveFrontend(w, r, filepath.Join(h.FrontendDir, "index.html"), false)
	})
	return compressResponses(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != "GET" && r.Method != "HEAD" {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host {
					reply(w, 403, map[string]string{"error": "请求来源与控制台不一致"})
					return
				}
			}
		}
		mux.ServeHTTP(w, r)
	}))
}

func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		reply(w, 400, map[string]string{"error": "请求格式或字段无效: " + err.Error()})
		return false
	}
	return true
}

func Server(handler http.Handler, addr string) *http.Server {
	return &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
}

var fingerprintedAsset = regexp.MustCompile(`-[a-zA-Z0-9_-]{8,}\.[a-zA-Z0-9]+$`)

func serveFrontend(w http.ResponseWriter, r *http.Request, path string, asset bool) {
	info, err := os.Stat(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if asset && fingerprintedAsset.MatchString(filepath.Base(path)) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("ETag", fmt.Sprintf("W/%q", filepath.Base(path)))
	} else {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", fmt.Sprintf("W/%q", fmt.Sprintf("%x-%x", info.ModTime().UnixNano(), info.Size())))
	}
	if asset && (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.Header.Get("Range") == "" && acceptsGzip(r.Header.Get("Accept-Encoding")) {
		if compressed, err := os.Stat(path + ".gz"); err == nil && !compressed.IsDir() {
			w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(path)))
			w.Header().Set("Content-Encoding", "gzip")
			http.ServeFile(w, r, path+".gz")
			return
		}
	}
	http.ServeFile(w, r, path)
}

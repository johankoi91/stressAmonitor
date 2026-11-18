package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func testRouter(t *testing.T) http.Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	t.Setenv("APP_ROOT", root)
	t.Setenv("STRESS_RESULT_DIR", t.TempDir())
	for file, body := range map[string]string{
		"stress-console/index.html": "stress",
		"key/server-key.pem":        "private",
	} {
		file = filepath.Join(root, "web-demo", file)
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	tasks.tasks = map[string]*stressTask{}
	return newRouter()
}

func TestHostedPagesAndPrivateFiles(t *testing.T) {
	router := testRouter(t)
	for _, tc := range []struct {
		path string
		code int
		body string
	}{
		{"/web-demo/stress-console/", 200, "stress"},
		{"/stress-console/index.html", 200, "stress"},
		{"/rtc-examples/index.html", 404, "404 page not found"},
		{"/customVideoSource/", 404, "404 page not found"},
		{"/web-demo/key/server-key.pem", 404, ""},
		{"/web-demo/../go.mod", 404, ""},
		{"/web-demo/.git/config", 404, ""},
		{"/web-demo/assets/", 404, ""},
		{"/web-demo/stress-console/missing.js", 404, ""},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, httptest.NewRequest("GET", tc.path, nil))
			if rr.Code != tc.code || rr.Body.String() != tc.body {
				t.Fatalf("got %d %q", rr.Code, rr.Body.String())
			}
		})
	}
	// An asset symlink cannot escape the public root.
	if err := os.WriteFile(projectPath("go.mod"), []byte("private"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(os.Getenv("APP_ROOT"), "go.mod"), projectPath("web-demo", "stress-console", "escape.js")); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/web-demo/stress-console/escape.js", nil))
	if rr.Code != 404 {
		t.Fatalf("symlink: %d", rr.Code)
	}
}

func TestLocustProxyMethodsAndPaths(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/swarm" || r.URL.RawQuery != "x=1" || r.Method != "POST" || r.Header.Get("X-Forwarded-Prefix") != "/locust" {
			t.Errorf("upstream request: %s %s", r.Method, r.URL)
		}
		data, _ := io.ReadAll(r.Body)
		if string(data) != "user_count=2" {
			t.Errorf("body=%s", data)
		}
		w.Header().Set("Location", "/stats/report")
		w.WriteHeader(302)
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	t.Setenv("LOCUST_ADDR", u.Host)
	router := testRouter(t)
	rr := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	router.ServeHTTP(rr, httptest.NewRequest("POST", "/locust/swarm?x=1", strings.NewReader("user_count=2")).WithContext(ctx))
	if rr.Code != 302 || rr.Header().Get("Location") != "/locust/stats/report" {
		t.Fatalf("response: %d %v", rr.Code, rr.Header())
	}
}

func TestEnvironmentOverrideAndLocustBinding(t *testing.T) {
	values := mergeEnv([]string{"HOST=old", "KEEP=1"}, map[string]string{"HOST": "new"})
	if strings.Join(values, ",") != "KEEP=1,HOST=new" {
		t.Fatal(values)
	}
	testRouter(t)
	task, err := buildTask(createStressTaskRequest{Type: "locust", Host: "http://127.0.0.1:1", AgoraAppID: "test", AgoraAppCertificate: "test", WebUI: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(task.Command, " "), "--web-host 127.0.0.1") {
		t.Fatal(task.Command)
	}
	data, _ := json.Marshal(snapshotTask(task))
	if !strings.Contains(string(data), `"status":"pending"`) {
		t.Fatal(string(data))
	}
}

// Exercise real repository paths, so moving scenario files cannot silently
// leave passing router tests while task execution is broken.
func TestScenarioEntrypoints(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ROOT", root)
	t.Setenv("STRESS_RESULT_DIR", t.TempDir())
	for _, taskType := range []string{"smoke", "locust", "gen-demo-urls"} {
		t.Run(taskType, func(t *testing.T) {
			task, err := buildTask(createStressTaskRequest{Type: taskType, Host: "http://127.0.0.1:1", AgoraAppID: strings.Repeat("1", 32), AgoraAppCertificate: strings.Repeat("2", 32), RoomUUID: "test-room", TokenAPIHost: "http://127.0.0.1:1"})
			if err != nil {
				t.Fatal(err)
			}
			script := ""
			switch taskType {
			case "smoke":
				script = task.Command[1]
			case "locust":
				for i, arg := range task.Command {
					if arg == "-f" {
						script = task.Command[i+1]
					}
				}
				if filepath.Base(script) != "locust_entry.py" {
					t.Fatalf("must load adapter: %v", task.Command)
				}
			default:
				script = task.Command[len(task.Command)-1]
			}
			if info, err := os.Stat(script); err != nil || !info.Mode().IsRegular() {
				t.Fatalf("missing entry %q: %v", script, err)
			}
			workingDir := taskWorkingDirectory(task)
			if info, err := os.Stat(workingDir); err != nil || !info.IsDir() {
				t.Fatalf("missing cwd %q: %v", workingDir, err)
			}
			if taskType != "gen-demo-urls" && workingDir != scenePath() {
				t.Fatal(workingDir)
			}
		})
	}
}

func TestAPaaSServiceBoundary(t *testing.T) {
	router := testRouter(t)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	if rr.Code != http.StatusTemporaryRedirect || rr.Header().Get("Location") != "/web-demo/stress-console/" {
		t.Fatalf("root: %d %v", rr.Code, rr.Header())
	}
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("POST", "/api/rtc/token", strings.NewReader(`{}`)))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("RTC API exposed by APaaS: %d", rr.Code)
	}
}

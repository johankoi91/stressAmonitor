package main

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	for file, body := range map[string]string{
		"index.html": "home", "rtc-examples/index.html": "rtc",
		"key/server-key.pem": "private", "assets/sample.mp4": "0123456789",
	} {
		file = filepath.Join(root, "web-demo", file)
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return newRouter()
}

func TestHostedPagesAndPrivateFiles(t *testing.T) {
	router := testRouter(t)
	for _, tc := range []struct {
		path string
		code int
		body string
	}{
		{"/rtc-examples/example/quickStart/", 200, "rtc"},
		{"/rtc-examples/example/basic/", 200, "rtc"},
		{"/assets/sample.mp4", 200, "0123456789"},
		{"/web-demo/key/server-key.pem", 404, ""},
		{"/web-demo/../go.mod", 404, ""},
		{"/web-demo/.git/config", 404, ""},
		{"/web-demo/assets/", 404, ""},
		{"/rtc-examples/missing.js", 404, ""},
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
	if err := os.Symlink(filepath.Join(os.Getenv("APP_ROOT"), "go.mod"), projectPath("web-demo", "assets", "escape.js")); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/web-demo/assets/escape.js", nil))
	if rr.Code != 404 {
		t.Fatalf("symlink: %d", rr.Code)
	}
}

func TestGzipAndVideoRange(t *testing.T) {
	router := testRouter(t)
	file, err := os.Create(projectPath("web-demo", "assets", "bundle.js"))
	if err != nil {
		t.Fatal(err)
	}
	zw := gzip.NewWriter(file)
	_, _ = zw.Write([]byte("window.test = true;"))
	_ = zw.Close()
	_ = file.Close()
	for _, accept := range []string{"", "gzip"} {
		req := httptest.NewRequest("GET", "/web-demo/assets/bundle.js", nil)
		req.Header.Set("Accept-Encoding", accept)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		body := rr.Body.String()
		if accept == "gzip" {
			if rr.Header().Get("Content-Encoding") != "gzip" {
				t.Fatal("missing gzip header")
			}
			zr, err := gzip.NewReader(rr.Body)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(zr)
			_ = zr.Close()
			body = string(data)
		}
		if rr.Code != 200 || body != "window.test = true;" {
			t.Fatalf("gzip=%q: %d %q", accept, rr.Code, body)
		}
	}
	req := httptest.NewRequest("GET", "/web-demo/assets/sample.mp4", nil)
	req.Header.Set("Range", "bytes=2-5")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != 206 || rr.Body.String() != "2345" {
		t.Fatalf("video range: %d %q", rr.Code, rr.Body.String())
	}
}

func TestRTCTokenWithLocalLibrary(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_ROOT", root)
	router := gin.New()
	router.POST("/api/rtc/token", createRTCToken)
	body := `{"appId":"11111111111111111111111111111111","appCertificate":"22222222222222222222222222222222","channelName":"path-check","uid":123,"expire":600}`
	request := httptest.NewRequest("POST", "/api/rtc/token", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, request)
	var result struct {
		Code int `json:"code"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil || rr.Code != 200 || result.Code != 0 || !strings.HasPrefix(result.Data.Token, "007") {
		t.Fatalf("token endpoint: %d %s (%v)", rr.Code, rr.Body.String(), err)
	}
}

func TestRTCServiceBoundary(t *testing.T) {
	router := testRouter(t)
	for _, route := range []string{"/locust/", "/api/stress/health", "/web-demo/stress-console/", "/stress-console/"} {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest("GET", route, nil))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("APaaS route %s exposed by RTC: %d", route, rr.Code)
		}
	}
}

func TestCatalogueRoutes(t *testing.T) {
	router := testRouter(t)
	for _, group := range []string{"quickStart", "basic", "advanced", "extension", "others"} {
		group := group
		t.Run(group, func(t *testing.T) {
			route := "/rtc-examples/example/" + group + "/"
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, httptest.NewRequest("GET", route, nil))
			if rr.Code != 200 || rr.Body.String() != "rtc" {
				t.Fatalf("catalogue: %d %q", rr.Code, rr.Body.String())
			}
			for _, old := range []string{strings.TrimSuffix(route, "/"), route + "index.html", "/web-demo" + route} {
				rr = httptest.NewRecorder()
				router.ServeHTTP(rr, httptest.NewRequest("GET", old+"?channel=a%2Fb", nil))
				if rr.Code != 307 || rr.Header().Get("Location") != route+"?channel=a%2Fb" {
					t.Fatalf("canonical route %s: %d %v", old, rr.Code, rr.Header())
				}
			}
		})
	}
	for _, old := range []string{"/", "/web-demo/", "/web-demo/index.html", "/rtc-examples/", "/rtc-examples/index.html", "/web-demo/rtc-examples/index.html", "/rtc-examples/example/"} {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest("GET", old+"?language=zh-CN", nil))
		if rr.Code != 307 || rr.Header().Get("Location") != quickStartRoute+"?language=zh-CN" {
			t.Fatalf("default route %s: %d %v", old, rr.Code, rr.Header())
		}
	}
	for _, missing := range []string{"/rtc-examples/example/framework/", "/rtc-examples/example/framework/vue/index.html", "/rtc-examples/example/unknown/", "/rtc-examples/example/basic/missing/index.html"} {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest("GET", missing, nil))
		if rr.Code != 404 {
			t.Fatalf("unknown route %s returned %d", missing, rr.Code)
		}
	}
}

func TestRTMDemoRoutes(t *testing.T) {
	router := testRouter(t)
	root := projectPath("web-demo", "rtm")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	for file, body := range map[string]string{"demo2.2.0-2.html": "rtm-demo", "demo2.2.0-2.js": "window.rtm=true;", "demo2.2.0-2.js.map": "{}", "demo2.2.0-2.js.LICENSE.txt": "license"} {
		if err := os.WriteFile(filepath.Join(root, file), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ path, body string }{
		{rtmDemoRoute, "rtm-demo"},
		{"/rtc-examples/example/others/rtm/demo2.2.0-2.js", "window.rtm=true;"},
		{"/rtc-examples/example/others/rtm/demo2.2.0-2.js.map", "{}"},
		{"/rtc-examples/example/others/rtm/demo2.2.0-2.js.LICENSE.txt", "license"},
	} {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest("GET", tc.path, nil))
		if rr.Code != 200 || rr.Body.String() != tc.body {
			t.Fatalf("%s: %d %q", tc.path, rr.Code, rr.Body.String())
		}
	}
	for _, old := range []string{"/rtm/", "/rtm/index.html", "/rtm/demo2.2.0-2.html", "/web-demo/rtm/demo2.2.0-2.html", "/rtc-examples/example/others/rtm/"} {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest("GET", old+"?probe=1", nil))
		if rr.Code != 307 || rr.Header().Get("Location") != rtmDemoRoute+"?probe=1" {
			t.Fatalf("%s: %d %v", old, rr.Code, rr.Header())
		}
	}
}

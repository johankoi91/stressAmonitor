package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

func main() {
	if err := serve(); err != nil {
		log.Fatal(err)
	}
}

func projectPath(parts ...string) string {
	root, err := filepath.Abs(getEnv("APP_ROOT", "."))
	if err != nil {
		root = getEnv("APP_ROOT", ".")
	}
	return filepath.Join(append([]string{root}, parts...)...)
}

func serve() error {
	// Keep the old server aliases usable with existing docker run commands.
	if len(os.Args) > 1 && os.Args[1] != "demo" && os.Args[1] != "server" {
		return fmt.Errorf("supported server modes: demo, server")
	}
	router := newRouter()
	server := &http.Server{Addr: ":" + getEnv("PORT", "8800"), Handler: router, ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-done:
			return
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	defer close(done)
	var err error
	if strings.EqualFold(getEnv("TLS_ENABLED", "true"), "false") {
		log.Printf("HTTP listening on %s", server.Addr)
		err = server.ListenAndServe()
	} else {
		cert, key, tlsErr := prepareTLS()
		if tlsErr != nil {
			return tlsErr
		}
		log.Printf("HTTPS listening on %s; examples=/rtc-examples/example/quickStart/ custom-video=/rtc-examples/example/advanced/customVideoSource/", server.Addr)
		err = server.ListenAndServeTLS(cert, key)
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func prepareTLS() (string, string, error) {
	// Certificates live outside the public static directory in the image.
	cert := getEnv("TLS_CERT_FILE", projectPath("web-demo", "key", "edge.rtcdevelopers.com.pem"))
	key := getEnv("TLS_KEY_FILE", projectPath("web-demo", "key", "edge.rtcdevelopers.com-key.pem"))
	for _, item := range []struct {
		env, file string
		mode      os.FileMode
	}{
		{"TLS_CERT", cert, 0644}, {"TLS_KEY", key, 0600},
	} {
		if encoded := os.Getenv(item.env); encoded != "" {
			data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(encoded), ""))
			if err != nil {
				return "", "", fmt.Errorf("%s must be base64: %w", item.env, err)
			}
			if err := os.MkdirAll(filepath.Dir(item.file), 0755); err != nil {
				return "", "", err
			}
			if err := os.WriteFile(item.file, data, item.mode); err != nil {
				return "", "", err
			}
		}
	}
	return cert, key, nil
}

var gzipMimeTypes = map[string]string{
	".js":   "text/javascript; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".html": "text/html; charset=utf-8",
}

func newRouter() *gin.Engine {
	r := gin.Default()
	_ = r.SetTrustedProxies(nil)
	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	r.POST("/api/rtc/token", createRTCToken)
	r.GET("/favicon.ico", func(c *gin.Context) { serveStatic(c, "rtc-examples/assets/favicon.ico") })
	r.GET("/", func(c *gin.Context) { redirectExamples(c, quickStartRoute) })
	for _, prefix := range []string{"/web-demo", "/rtc-examples", "/assets", "/rtm"} {
		prefix := prefix
		r.GET(prefix, func(c *gin.Context) { c.Redirect(http.StatusTemporaryRedirect, prefix+"/") })
		handler := func(c *gin.Context) {
			rel := c.Param("filepath")
			if prefix != "/web-demo" {
				rel = strings.TrimPrefix(prefix, "/") + rel
			}
			serveStatic(c, rel)
		}
		r.GET(prefix+"/*filepath", handler)
		r.HEAD(prefix+"/*filepath", handler)
	}
	return r
}

const quickStartRoute = "/rtc-examples/example/quickStart/"
const rtmDemoRoute = "/rtc-examples/example/others/rtm/index.html"

var demoCategorySegments = map[string]bool{
	"quickStart": true, "basic": true, "advanced": true, "extension": true, "others": true,
}

func redirectExamples(c *gin.Context, target string) {
	if c.Request.URL.RawQuery != "" {
		target += "?" + c.Request.URL.RawQuery
	}
	c.Redirect(http.StatusTemporaryRedirect, target)
}

func categoryRoute(rel string) (string, bool) {
	parts := strings.Split(rel, "/")
	if len(parts) < 3 || parts[0] != "rtc-examples" || parts[1] != "example" || !demoCategorySegments[parts[2]] {
		return "", false
	}
	if len(parts) == 3 || (len(parts) == 4 && parts[3] == "index.html") {
		return "/rtc-examples/example/" + parts[2] + "/", true
	}
	return "", false
}

func serveStatic(c *gin.Context, rel string) {
	// Reject traversal, hidden files and TLS material before any file is opened.
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") || strings.EqualFold(part, "key") {
			c.Status(404)
			return
		}
	}
	rel = strings.TrimPrefix(path.Clean("/"+rel), "/")
	if rel == "" || rel == "index.html" || rel == "rtc-examples" || rel == "rtc-examples/index.html" || rel == "rtc-examples/example" {
		redirectExamples(c, quickStartRoute)
		return
	}
	if strings.HasPrefix(c.Request.URL.Path, "/web-demo/rtc-examples/") {
		redirectExamples(c, strings.TrimPrefix(c.Request.URL.Path, "/web-demo"))
		return
	}
	if rel == "rtm" || rel == "rtm/index.html" || rel == "rtm/demo2.2.0-2.html" || rel == "rtc-examples/example/others/rtm" {
		redirectExamples(c, rtmDemoRoute)
		return
	}
	if strings.HasPrefix(rel, "rtc-examples/example/others/rtm/") {
		resource := strings.TrimPrefix(rel, "rtc-examples/example/others/rtm/")
		if resource == "index.html" {
			resource = "demo2.2.0-2.html"
		}
		rel = "rtm/" + resource
	}
	if groupRoute, ok := categoryRoute(rel); ok {
		if c.Request.URL.Path != groupRoute {
			redirectExamples(c, groupRoute)
			return
		}
		rel = "rtc-examples/index.html"
	}
	root := projectPath("web-demo")
	filePath := filepath.Join(root, filepath.FromSlash(rel))
	realPath, err := filepath.EvalSymlinks(filePath)
	rootPath, rootErr := filepath.EvalSymlinks(root)
	if err != nil || rootErr != nil {
		c.Status(404)
		return
	}
	confined, err := filepath.Rel(rootPath, realPath)
	if err != nil || confined == ".." || strings.HasPrefix(confined, ".."+string(filepath.Separator)) {
		c.Status(404)
		return
	}
	info, err := os.Stat(filePath)
	if err != nil {
		c.Status(404)
		return
	}
	if info.IsDir() {
		if !strings.HasSuffix(c.Request.URL.Path, "/") {
			target := c.Request.URL.Path + "/"
			if c.Request.URL.RawQuery != "" {
				target += "?" + c.Request.URL.RawQuery
			}
			c.Redirect(307, target)
			return
		}
		filePath = filepath.Join(filePath, "index.html")
	}
	file, err := os.Open(filePath)
	if err != nil {
		c.Status(404)
		return
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || info.IsDir() {
		c.Status(404)
		return
	}
	magic := make([]byte, 2)
	n, _ := file.Read(magic)
	_, _ = file.Seek(0, io.SeekStart)
	mimeType := gzipMimeTypes[strings.ToLower(filepath.Ext(filePath))]
	if mimeType != "" {
		c.Header("Content-Type", mimeType)
	}
	if strings.EqualFold(filepath.Ext(filePath), ".wasm") {
		c.Header("Content-Type", "application/wasm")
	}
	if n == 2 && magic[0] == 0x1f && magic[1] == 0x8b && mimeType != "" {
		c.Header("Vary", "Accept-Encoding")
		// Decompress pre-gzipped assets for clients that don't support gzip.
		if !strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") {
			reader, err := gzip.NewReader(file)
			if err != nil {
				c.Status(500)
				return
			}
			defer reader.Close()
			if c.Request.Method != http.MethodHead {
				_, _ = io.Copy(c.Writer, reader)
			}
			return
		}
		c.Header("Content-Encoding", "gzip")
	}
	// ServeContent preserves index.html URLs and supports video range requests.
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), file)
}

func createRTCToken(c *gin.Context) {
	var request struct {
		AppID       string          `json:"appId"`
		Certificate string          `json:"appCertificate"`
		Channel     string          `json:"channelName"`
		UID         json.RawMessage `json:"uid"`
		Expire      int             `json:"expire"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16*1024)
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(400, gin.H{"code": 1, "msg": "invalid token request"})
		return
	}
	for _, value := range []string{request.AppID, request.Certificate} {
		if decoded, err := hex.DecodeString(value); err != nil || len(decoded) != 16 {
			c.JSON(400, gin.H{"code": 1, "msg": "App ID and certificate must be 32 hex characters"})
			return
		}
	}
	if request.Channel == "" || len(request.Channel) > 64 || request.Expire < 0 || request.Expire > 86400 {
		c.JSON(400, gin.H{"code": 1, "msg": "channel required (max 64 bytes), expire must be 0..86400"})
		return
	}
	input, _ := json.Marshal(request)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", projectPath("scripts", "rtc_token.py"))
	cmd.Dir = projectPath()
	cmd.Stdin = bytes.NewReader(input)
	output, err := cmd.Output()
	if err != nil {
		c.JSON(500, gin.H{"code": 1, "msg": "local token generation failed"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(200, "application/json", output)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

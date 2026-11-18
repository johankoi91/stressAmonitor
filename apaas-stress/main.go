package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

func scenePath(parts ...string) string {
	return projectPath(append([]string{"apaas_scene"}, parts...)...)
}

func taskWorkingDirectory(task *stressTask) string {
	if task.Type == "locust" || task.Type == "smoke" {
		return scenePath()
	}
	return projectPath()
}

func serve() error {
	// Keep the old server aliases usable with existing docker run commands.
	if len(os.Args) > 1 && os.Args[1] != "stress" && os.Args[1] != "server" {
		return fmt.Errorf("use the web console to start tasks; supported server modes: stress, server")
	}
	router := newRouter()
	server := &http.Server{Addr: ":" + getEnv("PORT", "8801"), Handler: router, ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
		case <-done:
			return
		}
		cancelAllTasks()
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
		log.Printf("HTTPS listening on %s; console=/web-demo/stress-console/ locust=/locust/", server.Addr)
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

func cancelAllTasks() {
	tasks.lock.RLock()
	defer tasks.lock.RUnlock()
	for _, task := range tasks.tasks {
		task.processLock.Lock()
		if task.Status == "running" || task.Status == "pending" {
			task.Status = "cancelled"
			if task.cancel != nil {
				task.cancel()
			}
			_ = saveTaskMetadata(task)
		}
		task.processLock.Unlock()
	}
}

var gzipMimeTypes = map[string]string{
	".js":   "text/javascript; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".html": "text/html; charset=utf-8",
}

type stressTask struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Status      string            `json:"status"`
	Command     []string          `json:"command"`
	Env         map[string]string `json:"env"`
	StartedAt   time.Time         `json:"startedAt"`
	FinishedAt  *time.Time        `json:"finishedAt,omitempty"`
	ExitCode    *int              `json:"exitCode,omitempty"`
	Error       string            `json:"error,omitempty"`
	LogPath     string            `json:"logPath"`
	ResultPath  string            `json:"resultPath,omitempty"`
	MetaPath    string            `json:"metaPath,omitempty"`
	cancel      context.CancelFunc
	processLock sync.Mutex
}

type stressTaskStore struct {
	lock  sync.RWMutex
	tasks map[string]*stressTask
}

var tasks = stressTaskStore{tasks: map[string]*stressTask{}}

// Static pages, task APIs and the Locust proxy share the Go listener.
func newRouter() *gin.Engine {
	r := gin.Default()
	_ = r.SetTrustedProxies(nil)
	loadPersistedTasks()
	registerStressAPI(r)
	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	r.GET("/favicon.ico", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.GET("/", func(c *gin.Context) { c.Redirect(http.StatusTemporaryRedirect, "/web-demo/stress-console/") })
	for _, prefix := range []string{"/web-demo", "/stress-console"} {
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
	registerLocustProxy(r)
	return r
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

func registerLocustProxy(r *gin.Engine) {
	target, _ := url.Parse("http://" + getEnv("LOCUST_ADDR", "127.0.0.1:8089"))
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		req.URL.Path = strings.TrimPrefix(req.URL.Path, "/locust")
		req.URL.RawPath = ""
		req.Header.Set("X-Forwarded-Prefix", "/locust")
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		if location := resp.Header.Get("Location"); strings.HasPrefix(location, "/") && !strings.HasPrefix(location, "//") {
			resp.Header.Set("Location", "/locust"+location)
		}
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `<p>Locust 尚未启动，请先在<a href="/web-demo/stress-console/">压测控制台</a>启动 Locust 任务。</p>`)
	}
	r.GET("/locust", func(c *gin.Context) { c.Redirect(307, "/locust/") })
	r.Any("/locust/*filepath", gin.WrapH(proxy))
}

func registerStressAPI(r *gin.Engine) {
	api := r.Group("/api/stress")
	api.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true, "time": time.Now().Format(time.RFC3339)})
	})
	api.POST("/tasks", createStressTask)
	api.GET("/tasks", listStressTasks)
	api.GET("/tasks/:id", getStressTask)
	api.GET("/tasks/:id/log", getStressTaskLog)
	api.GET("/tasks/:id/logs.zip", downloadStressTaskLogs)
	api.POST("/tasks/:id/cancel", cancelStressTask)
}

type createStressTaskRequest struct {
	Type                string            `json:"type"`
	Host                string            `json:"host"`
	AgoraAppID          string            `json:"agoraAppId"`
	AgoraAppCertificate string            `json:"agoraAppCertificate"`
	Users               int               `json:"users"`
	SpawnRate           int               `json:"spawnRate"`
	RunTime             string            `json:"runTime"`
	Prefix              string            `json:"prefix"`
	LogLevel            string            `json:"logLevel"`
	WebUI               bool              `json:"webUI"`
	AutoStart           bool              `json:"autostart"`
	RoomUUID            string            `json:"roomUuid"`
	TokenAPIHost        string            `json:"tokenApiHost"`
	BaseURL             string            `json:"baseUrl"`
	Version             string            `json:"version"`
	BuildID             string            `json:"buildId"`
	Page                string            `json:"page"`
	Count               int               `json:"count"`
	ExtraEnv            map[string]string `json:"extraEnv"`
}

var createTaskLock sync.Mutex

func createStressTask(c *gin.Context) {
	createTaskLock.Lock()
	defer createTaskLock.Unlock()
	var req createStressTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	task, err := buildTask(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if task.Type == "locust" && isLocustWebUITask(task) {
		if err := cancelRunningWebUITasks(task.ID); err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
	}

	tasks.lock.Lock()
	tasks.tasks[task.ID] = task
	tasks.lock.Unlock()
	task.processLock.Lock()
	_ = saveTaskMetadata(task)
	task.processLock.Unlock()

	go runTask(task)
	c.JSON(http.StatusAccepted, snapshotTask(task))
}

func buildTask(req createStressTaskRequest) (*stressTask, error) {
	taskType := strings.TrimSpace(req.Type)
	if taskType == "" {
		taskType = "smoke"
	}

	id := newTaskID()
	resultDir := filepath.Join(getEnv("STRESS_RESULT_DIR", "/results"), "web-console", id)
	if err := os.MkdirAll(resultDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create result dir: %w", err)
	}

	logPath := filepath.Join(resultDir, "task.log")
	env := map[string]string{}
	for key, value := range req.ExtraEnv {
		env[key] = value
	}
	if req.AgoraAppID != "" {
		env["AGORA_APP_ID"] = req.AgoraAppID
	}
	if req.AgoraAppCertificate != "" {
		env["AGORA_APP_CERTIFICATE"] = req.AgoraAppCertificate
	}
	if req.Host != "" {
		env["HOST"] = req.Host
		env["APAAS_HOST"] = req.Host
	}

	var command []string
	switch taskType {
	case "smoke":
		if env["AGORA_APP_ID"] == "" || env["AGORA_APP_CERTIFICATE"] == "" || env["APAAS_HOST"] == "" {
			return nil, errors.New("smoke requires AGORA_APP_ID, AGORA_APP_CERTIFICATE and host")
		}
		command = []string{"python3", scenePath("apaas_smoke.py")}
	case "locust":
		if env["AGORA_APP_ID"] == "" || env["AGORA_APP_CERTIFICATE"] == "" || env["HOST"] == "" {
			return nil, errors.New("locust requires AGORA_APP_ID, AGORA_APP_CERTIFICATE and host")
		}
		users := defaultInt(req.Users, 1)
		spawnRate := defaultInt(req.SpawnRate, 1)
		runTime := defaultString(req.RunTime, "60s")
		prefix := defaultString(req.Prefix, "web_console_"+id)
		logLevel := normalizeLocustLogLevel(req.LogLevel)
		command = []string{
			"python3", "-m", "locust",
			"-f", scenePath("locust_entry.py"),
			"--host", req.Host,
			"--csv", filepath.Join(resultDir, prefix),
			"-L", logLevel,
		}
		if req.WebUI {
			command = append(command,
				"-u", strconv.Itoa(users),
				"-r", strconv.Itoa(spawnRate),
				"-t", runTime,
				"--legacy-ui",
				"--web-host", "127.0.0.1",
				"--web-port", "8089",
			)
			if req.AutoStart {
				command = append(command, "--autostart")
			}
		} else {
			command = append(command,
				"--headless",
				"-u", strconv.Itoa(users),
				"-r", strconv.Itoa(spawnRate),
				"-t", runTime,
			)
		}
	case "gen-demo-urls":
		if env["AGORA_APP_ID"] == "" || req.TokenAPIHost == "" || req.RoomUUID == "" {
			return nil, errors.New("gen-demo-urls requires app id, token api host and room uuid")
		}
		count := defaultInt(req.Count, 1)
		env["APP_ID"] = req.AgoraAppID
		env["TOKEN_API_HOST"] = req.TokenAPIHost
		env["ROOM_UUID"] = req.RoomUUID
		env["BASE_URL"] = defaultString(req.BaseURL, "https://solutions-apaas.agora.io")
		env["VERSION"] = defaultString(req.Version, "release_3.9.1")
		env["BUILD_ID"] = defaultString(req.BuildID, "20260204_4784")
		env["PAGE"] = defaultString(req.Page, "recording")
		command = []string{"bash", "-c", fmt.Sprintf(`for ((i=0;i<%d;i++)); do bash "$1" || exit $?; echo; done`, count), "gen-demo-urls", projectPath("scripts", "gen_demo_urls", "build-apaas-meeting-url.sh")}
	default:
		return nil, fmt.Errorf("unsupported task type: %s", taskType)
	}

	return &stressTask{
		ID:         id,
		Type:       taskType,
		Status:     "pending",
		Command:    command,
		Env:        env,
		StartedAt:  time.Now(),
		LogPath:    logPath,
		ResultPath: resultDir,
		MetaPath:   filepath.Join(resultDir, "task.json"),
	}, nil
}

func runTask(task *stressTask) {
	ctx, cancel := context.WithCancel(context.Background())
	task.processLock.Lock()
	if task.Status == "cancelled" {
		task.processLock.Unlock()
		cancel()
		return
	}
	task.cancel = cancel
	task.Status = "running"
	task.processLock.Unlock()
	defer cancel()

	logFile, err := os.OpenFile(task.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		finishTask(task, -1, err.Error())
		return
	}
	defer logFile.Close()

	cmd := exec.CommandContext(ctx, task.Command[0], task.Command[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.Env = mergeEnv(os.Environ(), task.Env)
	cmd.Dir = taskWorkingDirectory(task)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	_, _ = fmt.Fprintf(logFile, "task_id=%s\ntype=%s\ncommand=%s\nstarted_at=%s\n\n", task.ID, task.Type, strings.Join(task.Command, " "), task.StartedAt.Format(time.RFC3339))
	err = cmd.Run()

	exitCode := 0
	errMsg := ""
	if err != nil {
		exitCode = 1
		errMsg = err.Error()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	finishTask(task, exitCode, errMsg)
}

func finishTask(task *stressTask, exitCode int, errMsg string) {
	now := time.Now()
	task.processLock.Lock()
	defer task.processLock.Unlock()
	task.FinishedAt = &now
	task.ExitCode = &exitCode
	task.Error = errMsg
	if task.Status == "cancelled" {
		_ = saveTaskMetadata(task)
		return
	}
	if exitCode == 0 {
		task.Status = "success"
	} else {
		task.Status = "failed"
	}
	_ = saveTaskMetadata(task)
}

func saveTaskMetadata(task *stressTask) error {
	if task.MetaPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(task.MetaPath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(task.MetaPath, data, 0644)
}

func loadPersistedTasks() {
	root := filepath.Join(getEnv("STRESS_RESULT_DIR", "/results"), "web-console")
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || info.Name() != "task.json" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		var task stressTask
		if jsonErr := json.Unmarshal(data, &task); jsonErr != nil {
			return nil
		}
		if task.Status == "running" || task.Status == "pending" {
			now := time.Now()
			task.Status = "failed"
			task.Error = "server stopped before task completed"
			task.FinishedAt = &now
		}
		task.MetaPath = path
		if task.ResultPath == "" {
			task.ResultPath = filepath.Dir(path)
		}
		if task.LogPath == "" {
			task.LogPath = filepath.Join(task.ResultPath, "task.log")
		}
		tasks.lock.Lock()
		tasks.tasks[task.ID] = &task
		tasks.lock.Unlock()
		return nil
	})
}

func listStressTasks(c *gin.Context) {
	tasks.lock.RLock()
	defer tasks.lock.RUnlock()
	list := make([]map[string]interface{}, 0, len(tasks.tasks))
	for _, task := range tasks.tasks {
		list = append(list, snapshotTask(task))
	}
	c.JSON(http.StatusOK, gin.H{"tasks": list})
}

func getStressTask(c *gin.Context) {
	task := findTask(c.Param("id"))
	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	c.JSON(http.StatusOK, snapshotTask(task))
}

func getStressTaskLog(c *gin.Context) {
	task := findTask(c.Param("id"))
	if task == nil {
		c.String(http.StatusNotFound, "task not found")
		return
	}
	content, err := readLastBytes(task.LogPath, 200*1024)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	c.Data(http.StatusOK, "text/plain; charset=utf-8", content)
}

func downloadStressTaskLogs(c *gin.Context) {
	task := findTask(c.Param("id"))
	if task == nil {
		c.String(http.StatusNotFound, "task not found")
		return
	}

	archivePath, err := createTaskLogArchive(task)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	filename := "apaas-stress-logs-" + task.ID + ".zip"
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.File(archivePath)
}

func createTaskLogArchive(task *stressTask) (string, error) {
	archivePath := filepath.Join(task.ResultPath, "logs-"+task.ID+".zip")
	if err := os.MkdirAll(task.ResultPath, 0755); err != nil {
		return "", err
	}

	file, err := os.Create(archivePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	defer zw.Close()

	manifest := buildArchiveManifest(task)
	if err := addBytesToZip(zw, "manifest.txt", []byte(manifest)); err != nil {
		return "", err
	}

	if err := addPathToZip(zw, task.ResultPath, "results"); err != nil {
		return "", err
	}
	_ = addPathToZip(zw, scenePath("logs"), "apaas_scene_logs")

	return archivePath, nil
}

func buildArchiveManifest(task *stressTask) string {
	task.processLock.Lock()
	defer task.processLock.Unlock()
	finishedAt := ""
	if task.FinishedAt != nil {
		finishedAt = task.FinishedAt.Format(time.RFC3339)
	}
	exitCode := ""
	if task.ExitCode != nil {
		exitCode = strconv.Itoa(*task.ExitCode)
	}
	return fmt.Sprintf(`task_id=%s
type=%s
status=%s
started_at=%s
finished_at=%s
exit_code=%s
error=%s
command=%s
result_path=%s
log_path=%s
`, task.ID, task.Type, task.Status, task.StartedAt.Format(time.RFC3339), finishedAt, exitCode, task.Error, strings.Join(task.Command, " "), task.ResultPath, task.LogPath)
}

func addPathToZip(zw *zip.Writer, sourcePath string, zipRoot string) error {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		return addFileToZip(zw, sourcePath, zipRoot)
	}

	return filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if strings.HasSuffix(info.Name(), ".zip") {
			return nil
		}
		rel, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return err
		}
		return addFileToZip(zw, path, filepath.ToSlash(filepath.Join(zipRoot, rel)))
	})
}

func addFileToZip(zw *zip.Writer, sourcePath string, zipName string) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(zipName)
	header.Method = zip.Deflate

	writer, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, file)
	return err
}

func addBytesToZip(zw *zip.Writer, zipName string, content []byte) error {
	writer, err := zw.Create(zipName)
	if err != nil {
		return err
	}
	_, err = writer.Write(content)
	return err
}

func cancelRunningWebUITasks(newTaskID string) error {
	var cancelled []string

	tasks.lock.RLock()
	runningTasks := make([]*stressTask, 0, len(tasks.tasks))
	for _, task := range tasks.tasks {
		if task.ID != newTaskID && (snapshotTask(task)["status"] == "running" || snapshotTask(task)["status"] == "pending") && task.Type == "locust" && isLocustWebUITask(task) {
			runningTasks = append(runningTasks, task)
		}
	}
	tasks.lock.RUnlock()

	for _, task := range runningTasks {
		task.processLock.Lock()
		if task.Status == "running" || task.Status == "pending" {
			if task.cancel != nil {
				task.cancel()
			}
			task.Status = "cancelled"
			cancelled = append(cancelled, task.ID)
			_ = saveTaskMetadata(task)
		}
		task.processLock.Unlock()
	}

	if len(cancelled) > 0 {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if !isTCPPortOpen("127.0.0.1:8089") {
				return nil
			}
			time.Sleep(200 * time.Millisecond)
		}
		return fmt.Errorf("cancelled existing Locust Web UI task(s) %s, but port 8089 is still in use", strings.Join(cancelled, ","))
	}

	if isTCPPortOpen("127.0.0.1:8089") {
		return errors.New("Locust Web UI port 8089 is already in use; please stop the existing process before starting a new Web UI task")
	}

	return nil
}

func isLocustWebUITask(task *stressTask) bool {
	for _, arg := range task.Command {
		if arg == "--web-port" || arg == "8089" {
			return true
		}
	}
	return false
}

func isTCPPortOpen(address string) bool {
	conn, err := net.DialTimeout("tcp", address, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func cancelStressTask(c *gin.Context) {
	task := findTask(c.Param("id"))
	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	task.processLock.Lock()
	if task.Status == "running" || task.Status == "pending" {
		if task.cancel != nil {
			task.cancel()
		}
		task.Status = "cancelled"
		_ = saveTaskMetadata(task)
	}
	task.processLock.Unlock()
	c.JSON(http.StatusOK, snapshotTask(task))
}
func findTask(id string) *stressTask {
	tasks.lock.RLock()
	defer tasks.lock.RUnlock()
	return tasks.tasks[id]
}

func readLastBytes(path string, maxBytes int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	var reader io.Reader = file
	if info.Size() > maxBytes {
		_, err = file.Seek(-maxBytes, io.SeekEnd)
		if err != nil {
			return nil, err
		}
		reader = file
	}

	var buf bytes.Buffer
	_, err = io.Copy(&buf, reader)
	return buf.Bytes(), err
}

func mergeEnv(base []string, extra map[string]string) []string {
	result := make([]string, 0, len(base)+len(extra))
	for _, item := range base {
		key, _, _ := strings.Cut(item, "=")
		if _, replaced := extra[key]; !replaced {
			result = append(result, item)
		}
	}
	for key, value := range extra {
		result = append(result, key+"="+value)
	}
	return result
}

func newTaskID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return time.Now().Format("20060102150405") + "-" + hex.EncodeToString(buf)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func defaultInt(value int, defaultValue int) int {
	if value > 0 {
		return value
	}
	return defaultValue
}

func defaultString(value string, defaultValue string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return defaultValue
}

func normalizeLocustLogLevel(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "DEBUG", "INFO", "WARNING", "ERROR", "CRITICAL":
		return strings.ToUpper(strings.TrimSpace(value))
	default:
		return "INFO"
	}
}

// saveTaskMetadata is called with processLock held once a task is registered.
func snapshotTask(task *stressTask) map[string]interface{} {
	task.processLock.Lock()
	defer task.processLock.Unlock()
	data, _ := json.Marshal(task)
	var snapshot map[string]interface{}
	_ = json.Unmarshal(data, &snapshot)
	return snapshot
}

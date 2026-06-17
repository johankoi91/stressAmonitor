package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/unrolled/secure"
)

func main() {
	GinHttps(true)
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

// servePrecompressedGzip detects pre-gzipped static files and sets the correct headers.
func servePrecompressedGzip(staticRoot string) gin.HandlerFunc {
	return func(c *gin.Context) {
		urlPath := c.Request.URL.Path

		if !strings.HasPrefix(urlPath, "/web-demo/") {
			c.Next()
			return
		}

		rel := strings.TrimPrefix(urlPath, "/web-demo/")
		diskPath := filepath.Join(staticRoot, rel)

		info, err := os.Stat(diskPath)
		if err != nil || info.IsDir() {
			c.Next()
			return
		}

		ext := strings.ToLower(filepath.Ext(diskPath))
		mimeType, ok := gzipMimeTypes[ext]
		if !ok {
			c.Next()
			return
		}

		f, err := os.Open(diskPath)
		if err != nil {
			c.Next()
			return
		}
		magic := make([]byte, 2)
		n, _ := f.Read(magic)
		f.Close()

		if n == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
			c.Header("Content-Encoding", "gzip")
			c.Header("Vary", "Accept-Encoding")
			c.Header("Content-Type", mimeType)
			http.ServeFile(c.Writer, c.Request, diskPath)
			c.Abort()
			return
		}

		c.Next()
	}
}

func GinHttps(isHttps bool) error {
	r := gin.Default()

	loadPersistedTasks()
	registerStressAPI(r)

	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/web-demo/") || strings.HasPrefix(path, "/api/") {
			c.Status(http.StatusNotFound)
			return
		}

		trimmed := strings.TrimPrefix(path, "/")
		if trimmed == "" {
			c.Redirect(http.StatusMovedPermanently, "/web-demo/stress-console/index.html")
			return
		}

		c.Redirect(http.StatusMovedPermanently, "/web-demo/"+trimmed)
	})

	r.Use(servePrecompressedGzip("./web-demo"))
	r.Static("/web-demo", "./web-demo")

	if isHttps {
		r.Use(TlsHandler(8800))
		return r.RunTLS(":"+strconv.Itoa(8800), "./web-demo/key/edge.rtcdevelopers.com.pem", "./web-demo/key/edge.rtcdevelopers.com-key.pem")
	}

	return r.Run(":" + strconv.Itoa(8800))
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

func createStressTask(c *gin.Context) {
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
	_ = saveTaskMetadata(task)

	go runTask(task)
	c.JSON(http.StatusAccepted, task)
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
		command = []string{"python3", "/opt/apaas_scene/apaas_smoke.py"}
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
			"-f", "/opt/apaas_scene/locustfile.py",
			"--host", req.Host,
			"--csv", filepath.Join(resultDir, prefix),
			"-L", logLevel,
		}
		if req.WebUI {
			command = append(command,
				"-u", strconv.Itoa(users),
				"-r", strconv.Itoa(spawnRate),
				"-t", runTime,
				"--web-host", "0.0.0.0",
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
		command = []string{"bash", "-lc", fmt.Sprintf("for i in $(seq 1 %d); do /opt/apaas_scene/gen_demo_urls/build-apaas-meeting-url.sh; echo; done", count)}
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
	cmd.Env = mergeEnv(os.Environ(), task.Env)
	cmd.Dir = "/opt/apaas_scene"
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
	list := make([]*stressTask, 0, len(tasks.tasks))
	for _, task := range tasks.tasks {
		list = append(list, task)
	}
	c.JSON(http.StatusOK, gin.H{"tasks": list})
}

func getStressTask(c *gin.Context) {
	task := findTask(c.Param("id"))
	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	c.JSON(http.StatusOK, task)
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
	_ = addPathToZip(zw, "/opt/apaas_scene/logs", "apaas_scene_logs")
	_ = addPathToZip(zw, "/var/log/go-demo-server.log", "container_logs/go-demo-server.log")
	_ = addPathToZip(zw, "/var/log/locust.log", "container_logs/locust.log")
	_ = addPathToZip(zw, "/var/log/supervisord.log", "container_logs/supervisord.log")
	_ = addPathToZip(zw, "/var/log/supervisor", "container_logs/supervisor")

	return archivePath, nil
}

func buildArchiveManifest(task *stressTask) string {
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
		if task.ID != newTaskID && task.Status == "running" && task.Type == "locust" && isLocustWebUITask(task) {
			runningTasks = append(runningTasks, task)
		}
	}
	tasks.lock.RUnlock()

	for _, task := range runningTasks {
		task.processLock.Lock()
		if task.cancel != nil && task.Status == "running" {
			task.cancel()
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
	if task.cancel != nil && task.Status == "running" {
		task.cancel()
		task.Status = "cancelled"
		_ = saveTaskMetadata(task)
	}
	task.processLock.Unlock()
	c.JSON(http.StatusOK, task)
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
	result := append([]string{}, base...)
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

func TlsHandler(port int) gin.HandlerFunc {
	return func(c *gin.Context) {
		secureMiddleware := secure.New(secure.Options{
			SSLRedirect: true,
			SSLHost:     ":" + strconv.Itoa(port),
		})
		err := secureMiddleware.Process(c.Writer, c.Request)
		if err != nil {
			return
		}
		c.Next()
	}
}

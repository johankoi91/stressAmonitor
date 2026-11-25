package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"rtcstress/internal/scenario"
	"rtcstress/internal/worker"
	"strings"
	"testing"
)

func TestDistributedPublisherDownloadsAudioOnAgents(t *testing.T) {
	data := []byte{1, 2, 3, 4}
	sum := sha256.Sum256(data)
	id := strings.Repeat("a", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/audio/"+id+"/content" {
			t.Errorf("wrong download path %s", r.URL.Path)
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()
	for _, node := range []string{"B", "C"} {
		a := &Agent{ctx: context.Background(), options: Options{Node: node, Center: strings.Replace(server.URL, "http:", "ws:", 1) + "/ws/agent", WorkDir: t.TempDir()}}
		r := worker.Request{Config: scenario.Config{AudioFiles: [2]string{"asset:" + id, "builtin:660"}}, Plan: scenario.HostPlan{Host: node, Processes: []scenario.Process{{Connections: []scenario.Connection{{AudioSource: 0}}}}}, Assets: []worker.AudioAsset{{ID: id, SHA256: hex.EncodeToString(sum[:]), Extension: ".pcm", Size: int64(len(data))}}}
		ready, err := a.prepareAudio(r)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(ready.Config.AudioFiles[0], "asset:") {
			t.Fatal("publisher audio was not prepared on Agent")
		}
		actual, err := os.ReadFile(ready.Config.AudioFiles[0])
		if err != nil || string(actual) != string(data) {
			t.Fatal("downloaded audio differs", err)
		}
	}
}

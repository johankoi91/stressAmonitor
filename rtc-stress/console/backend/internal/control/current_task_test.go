package control

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"rtcstress/internal/scenario"
)

func TestOnlyCurrentTaskLivesInMemoryWithoutHistoryFiles(t *testing.T) {
	directory := t.TempDir()
	history := filepath.Join(directory, "tasks")
	if err := os.Mkdir(history, 0700); err != nil {
		t.Fatal(err)
	}
	old, _ := json.Marshal(Task{ID: "archived", Name: "old task", State: "completed"})
	if err := os.WriteFile(filepath.Join(history, "archived.json"), old, 0600); err != nil {
		t.Fatal(err)
	}
	transport := &distributedTransport{epochs: map[string]time.Time{}}
	manager, err := NewManager(directory, transport)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		manager.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := manager.Wait(ctx); err != nil {
			t.Error(err)
		}
	}()
	if manager.CurrentID() != "" || len(manager.Tasks()) != 0 {
		t.Fatal("archived tasks were restored")
	}
	config := scenario.Defaults()
	config.Channels = 1
	config.Token = "test-app"
	config.AP = "127.0.0.1"
	config.APCert = "ap.test.local"
	settings := Settings{Config: config, Hosts: scenario.DefaultHosts()}
	for i := range settings.Hosts {
		settings.Hosts[i].Address = []string{"127.0.0.1", "127.0.0.2", "127.0.0.3"}[i]
	}
	if err := manager.Update(settings); err != nil {
		t.Fatal(err)
	}
	manager.tasks["previous"] = &runState{task: Task{ID: "previous", State: "completed"}}
	task, err := manager.Create(config)
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { current, ok := manager.Get(task.ID); return ok && current.State == "running" })
	if _, ok := manager.Get("previous"); ok {
		t.Fatal("previous task remained as history")
	}
	if manager.CurrentID() != task.ID || len(manager.Tasks()) != 1 {
		t.Fatal("current task was lost")
	}
	if err := manager.Stop(task.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { current, _ := manager.Get(task.ID); return current.State == "stopped" })
	entries, err := os.ReadDir(history)
	if err != nil || len(entries) != 1 || entries[0].Name() != "archived.json" {
		t.Fatal("current task was saved to disk", err)
	}
	restarted, err := NewManager(directory, transport)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if restarted.CurrentID() != "" {
		t.Fatal("task survived a manager restart")
	}
	if err := manager.Update(settings); err != nil {
		t.Fatal(err)
	}
	if manager.CurrentID() != "" {
		t.Fatal("finished task did not clear when preparing new settings")
	}
}

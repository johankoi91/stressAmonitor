package control

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rtcstress/internal/scenario"
)

func bundledDefaults(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "audio"), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../../deploy/defaults/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	settings.Config.Token = "test-app"
	if err := writeJSON(filepath.Join(dir, "settings.json"), settings); err != nil {
		t.Fatal(err)
	}
	for _, ref := range settings.Config.AudioFiles {
		name := strings.TrimPrefix(ref, "default:")
		data, err := os.ReadFile(filepath.Join("../../../res", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "audio", name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestBundledDefaultsInitializeAndPreserveSettings(t *testing.T) {
	defaults, data := bundledDefaults(t), t.TempDir()
	m, err := NewManager(data, nil, defaults)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := m.Settings()
	plan, err := scenario.Build(s.Config)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]int{"A": 300, "B": 300, "C": 200, "D": 200}
	if len(s.Hosts) != 4 || len(plan.Hosts) != 4 || !plan.Coverage.Verified {
		t.Fatalf("invalid bundled machine/channel plan: %d hosts", len(plan.Hosts))
	}
	for _, host := range plan.Hosts {
		if host.Connections != expected[host.Host] {
			t.Fatalf("%s has %d connections", host.Host, host.Connections)
		}
	}
	if len(m.audio.List()) != 2 || s.Config.AudioInputMode != "opus" || s.Config.AudioChannels != 1 {
		t.Fatal("default Opus assets are not registered")
	}
	for _, ref := range s.Config.AudioFiles {
		f, path, ok := m.audio.Get(strings.TrimPrefix(ref, "asset:"))
		if !ok || f.SampleRate != 48000 || f.FrameDurationMS != 20 || f.Codec != "opus" {
			t.Fatal("invalid default audio format")
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	s.Config.Name = "用户修改的配置"
	s.Config.DurationSec = 1234
	s.Config.AudioFiles[0], s.Config.AudioFiles[1] = s.Config.AudioFiles[1], s.Config.AudioFiles[0]
	if err := m.Update(s); err != nil {
		t.Fatal(err)
	}
	// A new image may carry changed presets, but saved choices must win.
	m, err = NewManager(data, nil, defaults)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := m.Settings()
	if got.Config.Name != s.Config.Name || got.Config.DurationSec != 1234 || got.Config.AudioFiles != s.Config.AudioFiles || len(m.audio.List()) != 2 {
		t.Fatal("restart overwrote user settings or duplicated default audio")
	}
	_, path, _ := m.audio.Get(strings.TrimPrefix(s.Config.AudioFiles[0], "asset:"))
	if err := os.WriteFile(path, []byte("damaged audio"), 0600); err != nil {
		t.Fatal(err)
	}
	m, err = NewManager(data, nil, defaults)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.audio.Normalize(s.Config); err != nil {
		t.Fatal(err)
	}
	contents, _ := os.ReadFile(path)
	if string(contents) == "damaged audio" {
		t.Fatal("default audio was not repaired")
	}
	// Uploaded copies use random IDs. Upgrades must reuse them by checksum.
	oldID := strings.TrimPrefix(s.Config.AudioFiles[0], "asset:")
	newID := "0123456789abcdef0123456789abcdef"
	file := m.audio.files[oldID]
	file.ID = newID
	delete(m.audio.files, oldID)
	m.audio.files[newID] = file
	if err := os.Rename(path, filepath.Join(m.audio.dir, newID+file.Extension)); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(m.audio.dir, "index.json"), m.audio.files); err != nil {
		t.Fatal(err)
	}
	s.Config.AudioFiles[0] = "asset:" + newID
	if err := m.Update(s); err != nil {
		t.Fatal(err)
	}
	m, err = NewManager(data, nil, defaults)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = m.Settings()
	if len(m.audio.List()) != 2 || got.Config.AudioFiles != s.Config.AudioFiles {
		t.Fatal("upgrade duplicated an already uploaded default asset")
	}
}

func TestMissingBundledAudioFailsStartup(t *testing.T) {
	dir := bundledDefaults(t)
	if err := os.Remove(filepath.Join(dir, "audio", "remote-decision-hq-48k-20ms.opus")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(t.TempDir(), nil, dir); err == nil {
		t.Fatal("missing image asset silently accepted")
	}
}

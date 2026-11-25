package control

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"rtcstress/internal/worker"
)

// Bundled defaults seed a new data directory; persisted settings take precedence.
// Audio is registered on every startup so it is also selectable after an upgrade.
func (m *Manager) loadDefaults(dir string) (*Settings, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		return nil, fmt.Errorf("读取默认配置: %w", err)
	}
	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("解析默认配置: %w", err)
	}
	for i, ref := range settings.Config.AudioFiles {
		name, ok := strings.CutPrefix(ref, "default:")
		if !ok || name == "." || name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `\`+"\x00") {
			return nil, fmt.Errorf("默认音源 %d 文件名无效", i+1)
		}
		id, err := m.audio.importDefaultOpus(filepath.Join(dir, "audio", name))
		if err != nil {
			return nil, fmt.Errorf("注册默认音源 %s: %w", name, err)
		}
		settings.Config.AudioFiles[i] = "asset:" + id
	}
	return &settings, nil
}

func (s *AudioStore) importDefaultOpus(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	opus, err := worker.ReadOpus(path, "")
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	digest := hex.EncodeToString(hash[:])
	id := digest[:32]
	s.mu.Lock()
	defer s.mu.Unlock()
	extension := ".opus"
	for existingID, file := range s.files {
		if file.SHA256 == digest && file.Codec == "opus" {
			id, extension = existingID, file.Extension
			break
		}
	}
	if file, ok := s.files[id]; ok && file.SHA256 != digest {
		return "", fmt.Errorf("默认音源标识冲突: %s", id)
	}
	dest := filepath.Join(s.dir, id+extension)
	existing, readErr := os.ReadFile(dest)
	if readErr != nil || sha256.Sum256(existing) != hash {
		if err := os.WriteFile(dest+".tmp", data, 0600); err != nil {
			return "", err
		}
		if err := os.Rename(dest+".tmp", dest); err != nil {
			return "", err
		}
	}
	if _, ok := s.files[id]; ok {
		return id, nil
	}
	s.files[id] = AudioFile{
		ID: id, Name: filepath.Base(path), Size: int64(len(data)), SHA256: digest,
		SampleRate: opus.SampleRate, Channels: opus.Channels, Codec: "opus",
		FrameDurationMS: 20, Bitrate: opus.Bitrate, DurationSec: opus.DurationSec,
		Extension: ".opus", CreatedAt: time.Now().UTC(),
	}
	if err := writeJSON(filepath.Join(s.dir, "index.json"), s.files); err != nil {
		delete(s.files, id)
		return "", err
	}
	return id, nil
}

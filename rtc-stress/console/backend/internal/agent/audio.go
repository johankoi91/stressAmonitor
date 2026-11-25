package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"rtcstress/internal/worker"
)

func (a *Agent) prepareAudio(request worker.Request) (worker.Request, error) {
	request.WorkDir = a.options.WorkDir
	request.Binary = ""
	if !request.Plan.HasPublishers() && request.Config.AudioInputMode != "opus" {
		return request, nil
	}
	dir := filepath.Join(a.options.WorkDir, "console-runs", "audio-cache")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return request, err
	}
	for i, ref := range request.Config.AudioFiles {
		if !strings.HasPrefix(ref, "asset:") {
			continue
		}
		id := strings.TrimPrefix(ref, "asset:")
		var asset *worker.AudioAsset
		for n := range request.Assets {
			if request.Assets[n].ID == id {
				asset = &request.Assets[n]
				break
			}
		}
		if asset == nil || !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(id) || (asset.Extension != ".wav" && asset.Extension != ".pcm" && asset.Extension != ".opus" && asset.Extension != ".ogg") || asset.Size <= 0 || asset.Size > 128<<20 {
			return request, errors.New("音频资源信息无效")
		}
		path := filepath.Join(dir, id+asset.Extension)
		if !matchesAudio(path, *asset) {
			if err := a.downloadAudio(path, *asset); err != nil {
				return request, err
			}
		}
		request.Config.AudioFiles[i] = path
	}
	return request, nil
}
func matchesAudio(path string, asset worker.AudioAsset) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != asset.Size {
		return false
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return false
	}
	return hex.EncodeToString(h.Sum(nil)) == asset.SHA256
}
func (a *Agent) downloadAudio(path string, asset worker.AudioAsset) error {
	u, _ := url.Parse(a.options.Center)
	u.Scheme = "http"
	u.Path = "/api/audio/" + asset.ID + "/content"
	u.RawQuery = ""
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return err
	}
	response, err := (&http.Client{Timeout: 2 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("下载音频 %s 失败: HTTP %d", asset.Name, response.StatusCode)
	}
	f, err := os.CreateTemp(filepath.Dir(path), "audio-*.part")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(response.Body, asset.Size+1))
	_ = f.Close()
	if err != nil {
		return err
	}
	if n != asset.Size || hex.EncodeToString(hash.Sum(nil)) != asset.SHA256 {
		return errors.New("音频大小或校验值不匹配")
	}
	return os.Rename(f.Name(), path)
}

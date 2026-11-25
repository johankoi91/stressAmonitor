package control

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"rtcstress/internal/scenario"
	"rtcstress/internal/worker"
)

type AudioFile struct {
	Codec           string    `json:"codec,omitempty"`
	FrameDurationMS int       `json:"frameDurationMs,omitempty"`
	Bitrate         int       `json:"bitrate,omitempty"`
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Size            int64     `json:"size"`
	SHA256          string    `json:"sha256"`
	SampleRate      int       `json:"sampleRate"`
	Channels        int       `json:"channels"`
	DurationSec     float64   `json:"durationSec"`
	CreatedAt       time.Time `json:"createdAt"`
	Extension       string    `json:"extension"`
}
type AudioStore struct {
	mu    sync.Mutex
	dir   string
	files map[string]AudioFile
}

func NewAudioStore(dataDir string) (*AudioStore, error) {
	dir := filepath.Join(dataDir, "audio")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &AudioStore{dir: dir, files: map[string]AudioFile{}}
	if data, err := os.ReadFile(filepath.Join(dir, "index.json")); err == nil {
		if err = json.Unmarshal(data, &s.files); err != nil {
			return nil, err
		}
	}
	return s, nil
}
func (s *AudioStore) List() []AudioFile {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []AudioFile{}
	for _, f := range s.files {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}
func (s *AudioStore) Get(id string) (AudioFile, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[id]
	if !ok {
		return AudioFile{}, "", false
	}
	return f, filepath.Join(s.dir, f.ID+f.Extension), true
}

func (s *AudioStore) Upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 129<<20)
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		reply(w, 400, map[string]string{"error": "音频须小于 128 MiB，支持 WAV、PCM、Ogg/Opus"})
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	input, header, err := r.FormFile("file")
	if err != nil {
		reply(w, 400, map[string]string{"error": "请选择音频文件"})
		return
	}
	defer input.Close()
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".wav" && ext != ".pcm" && ext != ".opus" && ext != ".ogg" {
		reply(w, 400, map[string]string{"error": "支持 PCM16 WAV、原始 PCM，以及 20 ms Ogg/Opus 文件"})
		return
	}
	rate, _ := strconv.Atoi(r.FormValue("sampleRate"))
	channels, _ := strconv.Atoi(r.FormValue("channels"))
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		reply(w, 500, map[string]string{"error": "无法创建文件标识"})
		return
	}
	id := hex.EncodeToString(random[:])
	path := filepath.Join(s.dir, id+ext)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		reply(w, 500, map[string]string{"error": "无法保存音频"})
		return
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(path)
		}
	}()
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(input, (128<<20)+1))
	_ = file.Close()
	if err != nil || size > 128<<20 {
		reply(w, 400, map[string]string{"error": "音频上传失败或超过 128 MiB"})
		return
	}
	var opus *worker.OpusAudio
	if ext == ".opus" || ext == ".ogg" {
		opus, err = worker.ReadOpus(path, "")
		if err != nil {
			reply(w, 400, map[string]string{"error": err.Error()})
			return
		}
		rate = opus.SampleRate
		channels = opus.Channels
	}
	if ext == ".wav" {
		rate, channels, err = wavFormat(path)
		if err != nil {
			reply(w, 400, map[string]string{"error": err.Error()})
			return
		}
	}
	if channels != 1 && channels != 2 {
		reply(w, 400, map[string]string{"error": "请选择单声道或双声道"})
		return
	}
	validRate := false
	for _, v := range []int{8000, 16000, 32000, 44100, 48000} {
		validRate = validRate || rate == v
	}
	if !validRate {
		reply(w, 400, map[string]string{"error": "音频采样率不支持，请使用 8000/16000/32000/44100/48000 Hz"})
		return
	}
	info := AudioFile{ID: id, Name: filepath.Base(header.Filename), Size: size, SHA256: hex.EncodeToString(hash.Sum(nil)), SampleRate: rate, Channels: channels, CreatedAt: time.Now().UTC(), Extension: ext, Codec: "pcm"}
	if opus != nil {
		info.Codec = "opus"
		info.FrameDurationMS = 20
		info.Bitrate = opus.Bitrate
		info.DurationSec = opus.DurationSec
	} else {
		pcm, err := worker.ReadAudio(path, "", rate, channels)
		if err != nil {
			reply(w, 400, map[string]string{"error": err.Error()})
			return
		}
		info.DurationSec = float64(len(pcm)) / float64(rate*channels*2)
	}

	s.mu.Lock()
	s.files[id] = info
	err = writeJSON(filepath.Join(s.dir, "index.json"), s.files)
	if err != nil {
		delete(s.files, id)
	}
	s.mu.Unlock()
	if err != nil {
		reply(w, 500, map[string]string{"error": "无法保存音频索引"})
		return
	}
	keep = true
	reply(w, 201, info)
}

func wavFormat(path string) (int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	info, _ := f.Stat()
	header := make([]byte, 12)
	if _, err = io.ReadFull(f, header); err != nil || string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" {
		return 0, 0, errors.New("不是有效的 WAV 文件")
	}
	for position := int64(12); position+8 <= info.Size(); {
		h := make([]byte, 8)
		if _, err = f.ReadAt(h, position); err != nil {
			return 0, 0, err
		}
		size := int64(binary.LittleEndian.Uint32(h[4:]))
		position += 8
		if size > info.Size()-position {
			return 0, 0, errors.New("WAV 文件内容不完整")
		}
		if string(h[:4]) == "fmt " {
			data := make([]byte, 16)
			if size < 16 {
				return 0, 0, errors.New("WAV fmt 数据不完整")
			}
			if _, err = f.ReadAt(data, position); err != nil {
				return 0, 0, err
			}
			if binary.LittleEndian.Uint16(data) != 1 || binary.LittleEndian.Uint16(data[14:]) != 16 {
				return 0, 0, errors.New("WAV 须为未压缩 PCM16 格式")
			}
			return int(binary.LittleEndian.Uint32(data[4:])), int(binary.LittleEndian.Uint16(data[2:])), nil
		}
		position += size + size%2
	}
	return 0, 0, errors.New("WAV 缺少 fmt 数据")
}

var audioID = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (s *AudioStore) Content(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !audioID.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	file, path, ok := s.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Audio-SHA256", file.SHA256)
	http.ServeFile(w, r, path)
}

func (s *AudioStore) Normalize(c scenario.Config) (scenario.Config, error) {
	modes := [2]string{"pcm", "pcm"}
	for i, ref := range c.AudioFiles {
		if strings.HasPrefix(ref, "asset:") {
			f, _, ok := s.Get(strings.TrimPrefix(ref, "asset:"))
			if !ok {
				return c, fmt.Errorf("音频 %d 不存在，请重新选择", i+1)
			}
			if f.SampleRate != c.SampleRate || f.Channels != c.AudioChannels {
				return c, fmt.Errorf("音频 %s 为 %d Hz / %d 声道，与任务采样格式不匹配", f.Name, f.SampleRate, f.Channels)
			}
			if f.Codec == "opus" {
				modes[i] = "opus"
			}
			c.AudioLabels[i] = f.Name
		} else if strings.HasPrefix(ref, "builtin:") {
			c.AudioLabels[i] = "内置 " + strings.TrimPrefix(ref, "builtin:") + " Hz 测试音"
		} else {
			ext := strings.ToLower(filepath.Ext(ref))
			if ext == ".opus" || ext == ".ogg" {
				modes[i] = "opus"
			}
			c.AudioLabels[i] = filepath.Base(ref)
		}
	}
	if modes[0] != modes[1] {
		return c, errors.New("两路音频须同为 PCM 或同为 Opus")
	}
	c.AudioInputMode = modes[0]
	return c, nil
}

func (s *AudioStore) Request(id string, c scenario.Config, plan scenario.HostPlan, h scenario.Host) (worker.Request, error) {
	r := worker.Request{TaskID: id, Config: c, Plan: plan, WorkDir: h.WorkDir}
	if !plan.HasPublishers() && c.AudioInputMode != "opus" {
		return r, nil
	}
	for i, ref := range c.AudioFiles {
		if !strings.HasPrefix(ref, "asset:") {
			continue
		}
		file, path, ok := s.Get(strings.TrimPrefix(ref, "asset:"))
		if !ok {
			return r, errors.New("所选音频已不存在")
		}
		if h.ID == "A" {
			r.Config.AudioFiles[i] = path
		} else {
			r.Assets = append(r.Assets, worker.AudioAsset{ID: file.ID, Name: file.Name, SHA256: file.SHA256, Size: file.Size, Extension: file.Extension})
		}
	}
	return r, nil
}

package worker

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ReadAudio returns signed little-endian PCM16, stripping a WAV container when present.
// Rate/channel conversion is deliberately not implicit: incompatible inputs fail before launch.
func ReadAudio(name, dir string, rate, channels int) ([]byte, error) {
	if strings.HasPrefix(name, "builtin:") {
		hz, e := strconv.Atoi(strings.TrimPrefix(name, "builtin:"))
		if e != nil || hz < 100 || hz > 4000 {
			return nil, errors.New("内置音频频率无效")
		}
		data := make([]byte, rate*channels*2*5)
		for i := 0; i < rate*5; i++ {
			value := int16(math.Sin(2*math.Pi*float64(hz)*float64(i)/float64(rate)) * 6000)
			for ch := 0; ch < channels; ch++ {
				binary.LittleEndian.PutUint16(data[(i*channels+ch)*2:], uint16(value))
			}
		}
		return data, nil
	}
	if !filepath.IsAbs(name) {
		name = filepath.Join(dir, name)
	}
	f, e := os.Open(name)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() || info.Size() > 128<<20 {
		return nil, errors.New("音频须为不超过 128 MiB 的普通文件")
	}
	raw, e := io.ReadAll(f)
	if e != nil {
		return nil, e
	}
	if bytes.HasPrefix(raw, []byte("OggS")) {
		return nil, errors.New("Ogg/Opus requires encoded audio mode")
	}
	if len(raw) >= 12 && string(raw[:4]) == "RIFF" && string(raw[8:12]) == "WAVE" {
		var pcm []byte
		validFmt := false
		for pos := 12; pos+8 <= len(raw); {
			size := int(binary.LittleEndian.Uint32(raw[pos+4 : pos+8]))
			start := pos + 8
			if size < 0 || size > len(raw)-start {
				return nil, errors.New("WAV 数据块不完整")
			}
			chunk := raw[start : start+size]
			switch string(raw[pos : pos+4]) {
			case "fmt ":
				if len(chunk) < 16 || binary.LittleEndian.Uint16(chunk) != 1 || int(binary.LittleEndian.Uint16(chunk[2:])) != channels || int(binary.LittleEndian.Uint32(chunk[4:])) != rate || binary.LittleEndian.Uint16(chunk[14:]) != 16 {
					return nil, fmt.Errorf("WAV 须为 %d Hz、%d 声道、PCM16", rate, channels)
				}
				validFmt = true
			case "data":
				pcm = chunk
			}
			pos = start + size + size%2
		}
		if !validFmt || len(pcm) == 0 {
			return nil, errors.New("WAV 缺少有效 fmt/data 块")
		}
		raw = pcm
	}
	if len(raw) < rate/100*channels*2 || len(raw)%(channels*2) != 0 {
		return nil, errors.New("PCM 文件为空、过短或采样帧不完整")
	}
	return raw, nil
}

func DifferentAudio(a, b []byte) bool { return !bytes.Equal(hash(a), hash(b)) }
func hash(b []byte) []byte            { h := sha256.Sum256(b); return h[:] }

func CopyLoop(dst, src []byte, offset *int) {
	for i := 0; i < len(dst); {
		n := copy(dst[i:], src[*offset:])
		i += n
		*offset = (*offset + n) % len(src)
	}
}

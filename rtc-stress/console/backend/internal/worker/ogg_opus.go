package worker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type OpusAudio struct {
	Packets     [][]byte
	Channels    int
	SampleRate  int
	Bitrate     int
	DurationSec float64
}

func oggCRC(page []byte) uint32 {
	var crc uint32
	for i, b := range page {
		if i >= 22 && i < 26 {
			b = 0
		}
		crc ^= uint32(b) << 24
		for bit := 0; bit < 8; bit++ {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
func ReadOpus(name, dir string) (*OpusAudio, error) {
	if !filepath.IsAbs(name) {
		name = filepath.Join(dir, name)
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 128<<20 {
		return nil, errors.New("Opus must be a regular file up to 128 MiB")
	}
	raw := make([]byte, info.Size())
	if _, err = f.ReadAt(raw, 0); err != nil {
		return nil, err
	}
	return ParseOpus(raw)
}

// ParseOpus demuxes a single Ogg/Opus stream. Audio payload is never decoded.
func ParseOpus(raw []byte) (*OpusAudio, error) {
	result := &OpusAudio{SampleRate: 48000}
	var pending []byte
	offset := 0
	packets := 0
	totalBytes := 0
	var serial, sequence uint32
	ended := false
	for offset < len(raw) {
		if ended || len(raw)-offset < 27 || string(raw[offset:offset+4]) != "OggS" || raw[offset+4] != 0 {
			return nil, errors.New("invalid Ogg page")
		}
		count := int(raw[offset+26])
		if len(raw)-offset < 27+count {
			return nil, errors.New("truncated Ogg lacing")
		}
		laces := raw[offset+27 : offset+27+count]
		body := offset + 27 + count
		end := body
		for _, n := range laces {
			end += int(n)
		}
		if end > len(raw) {
			return nil, errors.New("truncated Ogg payload")
		}
		page := raw[offset:end]
		if oggCRC(page) != binary.LittleEndian.Uint32(page[22:26]) {
			return nil, errors.New("Ogg checksum mismatch")
		}
		flags := page[5]
		nextSerial := binary.LittleEndian.Uint32(page[14:18])
		nextSequence := binary.LittleEndian.Uint32(page[18:22])
		if offset == 0 {
			serial = nextSerial
			sequence = nextSequence
			if flags&2 == 0 {
				return nil, errors.New("missing Ogg beginning")
			}
		} else {
			if nextSerial != serial || nextSequence != sequence+1 {
				return nil, errors.New("chained or unordered Ogg stream")
			}
			sequence = nextSequence
		}
		if (flags&1 != 0) != (len(pending) > 0) {
			return nil, errors.New("invalid Ogg continuation")
		}
		cursor := body
		for _, n := range laces {
			pending = append(pending, raw[cursor:cursor+int(n)]...)
			cursor += int(n)
			if len(pending) > 1<<20 {
				return nil, errors.New("oversized Opus packet")
			}
			if n == 255 {
				continue
			}
			packet := pending
			pending = nil
			switch packets {
			case 0:
				if len(packet) < 19 || !bytes.HasPrefix(packet, []byte("OpusHead")) || packet[8] != 1 || (packet[9] != 1 && packet[9] != 2) || packet[18] != 0 {
					return nil, errors.New("unsupported Opus header (mono/stereo mapping 0 required)")
				}
				result.Channels = int(packet[9])
			case 1:
				if !bytes.HasPrefix(packet, []byte("OpusTags")) {
					return nil, errors.New("missing OpusTags")
				}
			default:
				duration, channels := OpusPacketInfo(packet)
				if duration != 20000 || channels != result.Channels || len(packet) > 8192 {
					return nil, fmt.Errorf("Opus audio must contain 20 ms packets with %d channels", result.Channels)
				}
				result.Packets = append(result.Packets, packet)
				totalBytes += len(packet)
			}
			packets++
		}
		ended = flags&4 != 0
		offset = end
	}
	if !ended || len(pending) != 0 || len(result.Packets) == 0 {
		return nil, errors.New("incomplete or empty Ogg/Opus file")
	}
	result.DurationSec = float64(len(result.Packets)) * 0.02
	result.Bitrate = int(float64(totalBytes) * 8 / result.DurationSec)
	return result, nil
}

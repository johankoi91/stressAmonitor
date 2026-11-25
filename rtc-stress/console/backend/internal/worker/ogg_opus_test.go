package worker

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

func opusPage(seq uint32, flags byte, laces []byte, payload []byte) []byte {
	b := make([]byte, 27+len(laces)+len(payload))
	copy(b, "OggS")
	b[5] = flags
	binary.LittleEndian.PutUint32(b[14:], 7)
	binary.LittleEndian.PutUint32(b[18:], seq)
	b[26] = byte(len(laces))
	copy(b[27:], laces)
	copy(b[27+len(laces):], payload)
	binary.LittleEndian.PutUint32(b[22:], oggCRC(b))
	return b
}
func TestOpusDemuxContinuationAndRejectCorruption(t *testing.T) {
	fixture, err := os.ReadFile("testdata/silence-20ms.opus")
	if err != nil {
		t.Fatal(err)
	}
	good, err := ParseOpus(fixture)
	if err != nil || good.Channels != 1 || len(good.Packets) != 1 || good.DurationSec != 0.02 {
		t.Fatal("invalid fixture", err)
	}
	head := append([]byte(nil), fixture[28:47]...)
	tags := []byte("OpusTags")
	packet := append([]byte{0xf8}, make([]byte, 319)...)
	raw := append(opusPage(0, 2, []byte{19}, head), opusPage(1, 0, []byte{8}, tags)...)
	raw = append(raw, opusPage(2, 0, []byte{255}, packet[:255])...)
	raw = append(raw, opusPage(3, 5, []byte{65}, packet[255:])...)
	got, err := ParseOpus(raw)
	if err != nil || !bytes.Equal(got.Packets[0], packet) {
		t.Fatal("cross-page packet lost", err)
	}
	corrupt := append([]byte(nil), raw...)
	corrupt[len(corrupt)-1] ^= 1
	if _, err := ParseOpus(corrupt); err == nil {
		t.Fatal("checksum corruption accepted")
	}
	if _, err := ParseOpus(raw[:len(raw)-1]); err == nil {
		t.Fatal("truncated packet accepted")
	}
	wrong := append(opusPage(0, 2, []byte{19}, head), opusPage(1, 0, []byte{8}, tags)...)
	wrong = append(wrong, opusPage(2, 4, []byte{1}, []byte{0x90})...)
	if _, err := ParseOpus(wrong); err == nil {
		t.Fatal("10ms packet accepted")
	}
	if _, err := ParseOpus(append(raw, raw...)); err == nil {
		t.Fatal("chained streams accepted")
	}
}

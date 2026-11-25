package worker

import "testing"

func TestOpusTOCMetadata(t *testing.T) {
	for _, tc := range []struct {
		packet       []byte
		us, channels int
	}{
		{[]byte{0x80}, 2500, 1}, {[]byte{0x88}, 5000, 1},
		{[]byte{0x94}, 10000, 2}, {[]byte{0x9c}, 20000, 2},
		{[]byte{0x99}, 40000, 1}, {[]byte{0x9b, 3}, 60000, 1},
		{[]byte{0x18}, 60000, 1}, {[]byte{0x60}, 10000, 1},
		{[]byte{0x9b, 7}, 0, 0}, {[]byte{0x9b, 0}, 0, 0},
		{[]byte{0x9b}, 0, 0}, {nil, 0, 0},
	} {
		us, channels := OpusPacketInfo(tc.packet)
		if us != tc.us || channels != tc.channels {
			t.Fatalf("TOC %x: got %d/%d, want %d/%d", tc.packet, us, channels, tc.us, tc.channels)
		}
	}
}

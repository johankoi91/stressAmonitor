package worker

// OpusPacketInfo reads the RFC 6716 TOC header without decoding any audio.
// It returns packet duration in microseconds and the coded channel count.
func OpusPacketInfo(packet []byte) (int, int) {
	if len(packet) == 0 {
		return 0, 0
	}
	config := int(packet[0] >> 3)
	frameUS := 0
	switch {
	case config < 12:
		frameUS = []int{10000, 20000, 40000, 60000}[config&3]
	case config < 16:
		frameUS = []int{10000, 20000}[config&1]
	default:
		frameUS = []int{2500, 5000, 10000, 20000}[config&3]
	}
	count := 1
	switch packet[0] & 3 {
	case 1, 2:
		count = 2
	case 3:
		if len(packet) < 2 {
			return 0, 0
		}
		count = int(packet[1] & 63)
	}
	if count == 0 || frameUS*count > 120000 {
		return 0, 0
	}
	channels := 1
	if packet[0]&4 != 0 {
		channels = 2
	}
	return frameUS * count, channels
}

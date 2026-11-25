package wire

import (
	"rtcstress/internal/worker"
	"sort"
	"strconv"
)

func formatUID(uid uint64) string { return strconv.FormatUint(uid, 10) }
func sortEvents(events []worker.Event) {
	sort.Slice(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })
}

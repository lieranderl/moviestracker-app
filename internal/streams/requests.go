package streams

import (
	"net/url"
	"strconv"
	"strings"
)

// HLSTrack reads the ?index=&audio= of a GStreamer stream: file 1 and
// audio track 0 when absent. It fails on anything but those numbers.
func HLSTrack(q url.Values) (index, audio int, ok bool) {
	index, audio = 1, 0
	var err error
	if v := q.Get("index"); v != "" {
		if index, err = strconv.Atoi(v); err != nil || index <= 0 {
			return 0, 0, false
		}
	}
	if v := q.Get("audio"); v != "" {
		if audio, err = strconv.Atoi(v); err != nil || audio < 0 {
			return 0, 0, false
		}
	}
	return index, audio, true
}

// RangeStart is the first byte of a "bytes=N-…" Range header (0 without one).
func RangeStart(header string) int64 {
	spec, ok := strings.CutPrefix(header, "bytes=")
	if !ok {
		return 0
	}
	first, _, _ := strings.Cut(spec, "-")
	n, _ := strconv.ParseInt(strings.TrimSpace(first), 10, 64)
	return max(n, 0)
}

// HLSPosition reads a GStreamer HLS request: the master playlist names the
// file and audio track, a segment its number (-1 for anything else).
func HLSPosition(path string, q url.Values) (file, audio, segment int) {
	segment = -1
	switch {
	case path == "master.m3u8":
		file, audio, _ = HLSTrack(q)
	case strings.HasPrefix(path, "seg/"):
		name := strings.TrimPrefix(path, "seg/")
		if n, err := strconv.Atoi(strings.TrimSuffix(name, ".m4s")); err == nil && n >= 0 {
			segment = n
		}
	}
	return file, audio, segment
}

package torrserver

import (
	"strconv"
	"strings"
)

// HLSOutput describes what TorrServer's GStreamer pipeline actually serves,
// as advertised by the first variant of its HLS master playlist.
type HLSOutput struct {
	VideoCodec       string // RFC 6381 codec string, e.g. "hvc1.2.4.H150.B0"
	AudioCodec       string // e.g. "mp4a.40.2"
	Width, Height    int
	FrameRate        string
	VideoRange       string // SDR, PQ or HLG
	Bandwidth        int64  // peak bits per second
	AverageBandwidth int64
	Subtitles        int // WebVTT subtitle renditions
}

// ParseMasterPlaylist extracts the output description from an HLS master playlist.
func ParseMasterPlaylist(playlist string) HLSOutput {
	var out HLSOutput
	variantSeen := false
	for line := range strings.Lines(playlist) {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "#EXT-X-MEDIA:"):
			if hlsAttrs(line)["TYPE"] == "SUBTITLES" {
				out.Subtitles++
			}
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF:") && !variantSeen:
			variantSeen = true
			attrs := hlsAttrs(line)
			for codec := range strings.SplitSeq(attrs["CODECS"], ",") {
				codec = strings.TrimSpace(codec)
				if isAudioCodec(codec) {
					out.AudioCodec = codec
				} else if codec != "" {
					out.VideoCodec = codec
				}
			}
			if w, h, ok := strings.Cut(attrs["RESOLUTION"], "x"); ok {
				out.Width, _ = strconv.Atoi(w)
				out.Height, _ = strconv.Atoi(h)
			}
			out.FrameRate = attrs["FRAME-RATE"]
			out.VideoRange = attrs["VIDEO-RANGE"]
			out.Bandwidth, _ = strconv.ParseInt(attrs["BANDWIDTH"], 10, 64)
			out.AverageBandwidth, _ = strconv.ParseInt(attrs["AVERAGE-BANDWIDTH"], 10, 64)
		}
	}
	return out
}

func isAudioCodec(codec string) bool {
	for _, p := range []string{"mp4a", "ac-3", "ec-3", "opus", "flac", "alac"} {
		if strings.HasPrefix(codec, p) {
			return true
		}
	}
	return false
}

// hlsAttrs parses an HLS attribute list (KEY=VALUE,KEY="quoted, value").
func hlsAttrs(line string) map[string]string {
	_, list, _ := strings.Cut(line, ":")
	attrs := map[string]string{}
	for list != "" {
		key, rest, ok := strings.Cut(list, "=")
		if !ok {
			break
		}
		var val string
		if strings.HasPrefix(rest, `"`) {
			end := strings.Index(rest[1:], `"`)
			if end < 0 {
				break
			}
			val, rest = rest[1:end+1], rest[end+2:]
			rest = strings.TrimPrefix(rest, ",")
		} else {
			val, rest, _ = strings.Cut(rest, ",")
		}
		attrs[strings.TrimSpace(key)] = val
		list = rest
	}
	return attrs
}

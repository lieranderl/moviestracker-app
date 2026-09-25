package views

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

// mediaInfo is the player's source-vs-HLS-output comparison, one row per aspect.
type mediaInfo struct {
	Rows []mediaInfoRow
}

type mediaInfoRow struct {
	Label, Source, Output string
	OutputTone            string // badge tone for remux/transcode verdicts
}

var capsProfile = regexp.MustCompile(`profile=\(string\)([\w-]+)`)

func newMediaInfo(p *torrserver.ProbeResult, audioIndex int, out *torrserver.HLSOutput) mediaInfo {
	var video, audio *torrserver.ProbeTrack
	subs := 0
	for i := range p.Tracks {
		switch trk := &p.Tracks[i]; trk.Type {
		case "video":
			if video == nil {
				video = trk
			}
		case "audio":
			if audio == nil || trk.Index == audioIndex {
				audio = trk
			}
		case "subtitle", "sub":
			subs++
		}
	}

	rows := []mediaInfoRow{
		{Label: "Container", Source: p.Container, Output: "HLS"},
		{Label: "Video", Source: sourceVideo(video)},
		{Label: "HDR", Source: sourceHDR(video)},
		{Label: "Audio", Source: sourceAudio(audio)},
		{Label: "Bitrate", Source: sourceBitrate(p)},
		{Label: "Size", Source: joinDot(torrserver.FormatBytes(p.FileSize), p.DurationFormatted())},
		{Label: "Subtitles", Source: fmt.Sprintf("%d text", subs)},
	}
	if out == nil {
		for i := range rows[1:] {
			rows[i+1].Output = "Waiting for stream…"
		}
		return mediaInfo{Rows: rows}
	}

	videoOut := codecFamily(out.VideoCodec)
	res := ""
	if out.Width > 0 {
		res = fmt.Sprintf("%d×%d", out.Width, out.Height)
	}
	fps := ""
	if out.FrameRate != "" {
		fps = out.FrameRate + " fps"
	}
	remux := video != nil && codecFamily(video.CapsName) == videoOut && video.Width == out.Width
	rows[1].Output = joinDot(videoOut, verdict(remux), res, fps)
	rows[1].OutputTone = verdictTone(rows[1].Output)
	rows[2].Output, rows[2].OutputTone = outputHDR(video, out.VideoRange, remux)
	audioOut := codecFamily(out.AudioCodec)
	rows[3].Output = joinDot(audioOut, verdict(audio != nil && codecFamily(audio.CapsName) == audioOut))
	rows[3].OutputTone = verdictTone(rows[3].Output)
	rows[4].Output = joinDot(mbps(out.AverageBandwidth)+" avg", mbps(out.Bandwidth)+" peak")
	rows[5].Output = "—"
	rows[6].Output = fmt.Sprintf("%d WebVTT", out.Subtitles)
	return mediaInfo{Rows: rows}
}

func sourceVideo(v *torrserver.ProbeTrack) string {
	if v == nil {
		return "—"
	}
	codec := codecFamily(v.CapsName)
	if m := capsProfile.FindStringSubmatch(v.Codec); m != nil {
		codec += " " + profileName(m[1])
	}
	var res, fps, depth string
	if v.Width > 0 {
		res = fmt.Sprintf("%d×%d", v.Width, v.Height)
	}
	if v.FrameRateNum > 0 && v.FrameRateDen > 0 {
		fps = strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.3f", float64(v.FrameRateNum)/float64(v.FrameRateDen)), "0"), ".") + " fps"
	}
	if v.BitDepth > 0 {
		depth = fmt.Sprintf("%d-bit", v.BitDepth)
	}
	return joinDot(codec, res, fps, depth)
}

// videoTransfer is the source's transfer curve: "pq", "hlg" or "" (SDR).
// TorrServer fills VideoTransfer only when the colorimetry names
// smpte2084, so the colorimetry and caps are read too: GStreamer may call
// it bt2100-pq, or give transfer 14 (PQ) or 15 (HLG) in numeric form.
// Mastering display data is only ever sent with PQ.
func videoTransfer(v *torrserver.ProbeTrack) string {
	if v.VideoTransfer != "" {
		return v.VideoTransfer
	}
	for _, c := range []string{v.Colorimetry, capsField(v.Codec, "colorimetry")} {
		c = strings.ToLower(c)
		parts := strings.Split(c, ":")
		switch {
		case strings.Contains(c, "pq"), strings.Contains(c, "smpte2084"), len(parts) == 4 && parts[2] == "14":
			return "pq"
		case strings.Contains(c, "hlg"), strings.Contains(c, "arib-std-b67"), len(parts) == 4 && parts[2] == "15":
			return "hlg"
		}
	}
	if v.HasMasteringDisplayInfo {
		return "pq"
	}
	return ""
}

// capsField is the value of one field of a GStreamer caps string.
func capsField(caps, name string) string {
	for _, f := range strings.Split(caps, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(f), "=")
		if ok && key == name {
			if _, v, typed := strings.Cut(value, ")"); typed {
				return v
			}
			return value
		}
	}
	return ""
}

// trackCaps is a track's caps without binary blobs such as codec_data,
// which say nothing to a person.
func trackCaps(caps string) string {
	fields := strings.Split(caps, ", ")
	kept := fields[:0]
	for _, f := range fields {
		if !strings.Contains(f, "=(buffer)") {
			kept = append(kept, f)
		}
	}
	return strings.Join(kept, ", ")
}

func sourceHDR(v *torrserver.ProbeTrack) string {
	if v == nil {
		return "—"
	}
	var hdr string
	switch transfer := videoTransfer(v); {
	case transfer == "pq" && v.HasMasteringDisplayInfo:
		hdr = "HDR10"
	case transfer == "pq":
		hdr = "PQ HDR"
	case transfer == "hlg":
		hdr = "HLG"
	default:
		hdr = "SDR"
	}
	if v.IsDolbyVision {
		hdr = joinDot(fmt.Sprintf("Dolby Vision P%d", v.DolbyVisionProfile), hdr)
	}
	return hdr
}

// outputHDR names what the HLS stream carries in the source's terms, and
// says whether the HDR was kept or tone mapped to SDR. HLS labels its range
// SDR, PQ (HDR10's curve) or HLG.
//
// Remuxed video is the source's own bytes, so it keeps the source's HDR
// whatever VIDEO-RANGE the playlist declares (TorrServer may leave it out).
func outputHDR(v *torrserver.ProbeTrack, videoRange string, remux bool) (string, string) {
	source := ""
	if v != nil {
		source = videoTransfer(v)
	}
	if remux && source != "" {
		return sourceHDR(v) + " · Kept", "text-success"
	}
	switch videoRange {
	case "PQ":
		name := "PQ HDR"
		if v != nil && v.HasMasteringDisplayInfo {
			name = "HDR10"
		}
		if source == "pq" {
			return name + " · Kept", "text-success"
		}
		return name + " · Converted", "text-warning"
	case "HLG":
		if source == "hlg" {
			return "HLG · Kept", "text-success"
		}
		return "HLG · Converted", "text-warning"
	}
	if source == "pq" || source == "hlg" {
		return "SDR · Tone mapped", "text-warning"
	}
	return "SDR", ""
}

func sourceAudio(a *torrserver.ProbeTrack) string {
	if a == nil {
		return "—"
	}
	codec := codecFamily(a.CapsName)
	if ch := channelLayout(a.Channels); ch != "" {
		codec += " " + ch
	}
	rate := ""
	if a.Rate > 0 {
		rate = fmt.Sprintf("%g kHz", float64(a.Rate)/1000)
	}
	label := strings.TrimSpace(strings.ToUpper(a.Language) + " " + a.Title)
	return joinDot(codec, rate, label)
}

func sourceBitrate(p *torrserver.ProbeResult) string {
	if p.FileSize <= 0 || p.DurationNS <= 0 {
		return "—"
	}
	return mbps(int64(float64(p.FileSize)*8/(float64(p.DurationNS)/1e9))) + " avg"
}

// codecFamily maps GStreamer caps names and RFC 6381 codec strings to a
// familiar codec name, so source and output can be compared.
func codecFamily(codec string) string {
	for _, m := range []struct{ prefix, name string }{
		{"video/x-h265", "HEVC"}, {"hvc1", "HEVC"}, {"hev1", "HEVC"},
		{"video/x-h264", "H.264"}, {"avc1", "H.264"}, {"avc3", "H.264"},
		{"video/x-av1", "AV1"}, {"av01", "AV1"},
		{"video/x-vp9", "VP9"}, {"vp09", "VP9"},
		{"audio/x-eac3", "E-AC3"}, {"ec-3", "E-AC3"},
		{"audio/x-ac3", "AC3"}, {"ac-3", "AC3"},
		{"audio/mpeg", "AAC"}, {"mp4a", "AAC"},
		{"audio/x-dts", "DTS"}, {"audio/x-true-hd", "TrueHD"},
		{"audio/x-opus", "Opus"}, {"opus", "Opus"},
		{"audio/x-flac", "FLAC"}, {"flac", "FLAC"},
	} {
		if strings.HasPrefix(codec, m.prefix) {
			return m.name
		}
	}
	return codec
}

func profileName(p string) string {
	switch p {
	case "main-10":
		return "Main 10"
	case "main":
		return "Main"
	case "high":
		return "High"
	case "high-10":
		return "High 10"
	}
	return p
}

func channelLayout(ch int) string {
	switch ch {
	case 0:
		return ""
	case 1:
		return "Mono"
	case 2:
		return "Stereo"
	case 6:
		return "5.1"
	case 8:
		return "7.1"
	}
	return fmt.Sprintf("%d ch", ch)
}

func verdict(same bool) string {
	if same {
		return "Remux"
	}
	return "Transcoded"
}

func verdictTone(s string) string {
	if strings.Contains(s, "Transcoded") {
		return "text-warning"
	}
	return "text-success"
}

func mbps(bps int64) string {
	return fmt.Sprintf("%.1f Mbps", float64(bps)/1_000_000)
}

func joinDot(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " · ")
}

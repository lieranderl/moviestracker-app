package views_test

import (
	"html"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// TorrServer fills VideoTransfer only for some spellings of the colorimetry,
// so an HDR source must still be recognised from its caps.
func TestHDRSourcesAreRecognisedFromTheirColorimetry(t *testing.T) {
	const caps = "video/x-h265, profile=(string)main-10, colorimetry=(string)%s, width=(int)3840"
	for _, tc := range []struct {
		name  string
		track torrserver.ProbeTrack
		want  string
	}{
		{"bt2100-pq with mastering data", torrserver.ProbeTrack{Colorimetry: "bt2100-pq", HasMasteringDisplayInfo: true}, "HDR10"},
		{"numeric PQ transfer", torrserver.ProbeTrack{Colorimetry: "2:9:14:9"}, "PQ HDR"},
		{"bt2100-hlg in the caps only", torrserver.ProbeTrack{Codec: strings.Replace(caps, "%s", "bt2100-hlg", 1)}, "HLG"},
		{"mastering data alone", torrserver.ProbeTrack{HasMasteringDisplayInfo: true}, "HDR10"},
		{"bt709 stays SDR", torrserver.ProbeTrack{Colorimetry: "bt709", Codec: strings.Replace(caps, "%s", "bt709", 1)}, "SDR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := tc.track
			v.Type, v.CapsName = "video", "video/x-h265"
			probe := &torrserver.ProbeResult{Container: "Matroska", Tracks: []torrserver.ProbeTrack{v}}
			out := html.UnescapeString(render(t, views.TorrMediaInfo(probe, 0, &torrserver.HLSOutput{VideoCodec: "hvc1.2.4.L153", VideoRange: "PQ"})))
			if !strings.Contains(out, ">"+tc.want+"<") {
				t.Errorf("source HDR is not %q:\n%s", tc.want, out)
			}
		})
	}
	hdr10 := &torrserver.ProbeResult{Tracks: []torrserver.ProbeTrack{{Type: "video", CapsName: "video/x-h265", Colorimetry: "bt2100-pq", HasMasteringDisplayInfo: true}}}
	if out := html.UnescapeString(render(t, views.TorrMediaInfo(hdr10, 0, &torrserver.HLSOutput{VideoCodec: "hvc1.2.4.L153", VideoRange: "PQ"}))); !strings.Contains(out, "HDR10 · Kept") {
		t.Error("remuxed HDR10 reads as converted")
	}
}

func TestMediaAnalysisShowsEachTracksWholeDescription(t *testing.T) {
	long := "audio/x-eac3, framed=(boolean)true, rate=(int)48000, channels=(int)6, alignment=(string)frame, channel-mask=(bitmask)0x000000000000003f"
	probe := &torrserver.ProbeResult{Container: "Matroska", Tracks: []torrserver.ProbeTrack{
		{Index: 3, Type: "audio", CapsName: "audio/x-eac3", Codec: long, Channels: 6},
		{Index: 0, Type: "video", CapsName: "video/x-h265", Codec: "video/x-h265, profile=(string)main-10, codec_data=(buffer)01220000000090000000000099f000fcfdfafa00000f04a0, colorimetry=(string)bt2100-pq", Title: "Main"},
	}}
	out := html.UnescapeString(render(t, views.TorrServerProbeFragment(probe)))
	if !strings.Contains(out, long) || !strings.Contains(out, "colorimetry=(string)bt2100-pq") {
		t.Errorf("track descriptions are cut:\n%s", out)
	}
	if strings.Contains(out, "01220000000090") {
		t.Error("binary codec data is noise, not a description")
	}
	if strings.Contains(out, "truncate") {
		t.Error("the analysis truncates text")
	}
}

func TestTheToneMappingBadgeSaysWhatHappensToHDR(t *testing.T) {
	without := html.UnescapeString(render(t, views.GStreamerRuntime(torrserver.EchoInfo{GSTAvailable: true, GSTVersion: "1.28.7"})))
	if !strings.Contains(without, "HDR stays HDR") || !strings.Contains(without, "data-tip=") {
		t.Errorf("without tone mapping the badge should say HDR is kept, and why that matters:\n%s", without)
	}
	with := html.UnescapeString(render(t, views.GStreamerRuntime(torrserver.EchoInfo{GSTAvailable: true, HDRTonemap: true})))
	if !strings.Contains(with, "HDR tone mapping") || !strings.Contains(with, "data-tip=") {
		t.Errorf("with tone mapping:\n%s", with)
	}
}

// Remuxed video is the source's own bytes, whatever range the playlist
// declares, so its HDR is kept; only transcoded video can be tone mapped.
func TestRemuxedHDRIsKeptWhateverThePlaylistSays(t *testing.T) {
	probe := &torrserver.ProbeResult{Tracks: []torrserver.ProbeTrack{{Type: "video", CapsName: "video/x-h265", Width: 3840, Height: 1920, Colorimetry: "bt2100-pq", HasMasteringDisplayInfo: true}}}
	for _, videoRange := range []string{"", "SDR"} {
		out := html.UnescapeString(render(t, views.TorrMediaInfo(probe, 0, &torrserver.HLSOutput{VideoCodec: "hvc1.2.4.L153", Width: 3840, Height: 1920, VideoRange: videoRange})))
		if !strings.Contains(out, ">HDR10 · Kept<") || strings.Contains(out, "Tone mapped") {
			t.Errorf("range %q: remuxed HDR10 should read as kept:\n%s", videoRange, out)
		}
	}
	transcoded := html.UnescapeString(render(t, views.TorrMediaInfo(probe, 0, &torrserver.HLSOutput{VideoCodec: "avc1.640033", Width: 3840, Height: 1920, VideoRange: "SDR"})))
	if !strings.Contains(transcoded, ">SDR · Tone mapped<") {
		t.Errorf("transcoded to SDR is tone mapped:\n%s", transcoded)
	}
}

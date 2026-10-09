package handlers

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// fieldKind is how a setting is edited.
type fieldKind string

const (
	kindBool     fieldKind = "bool"
	kindInt      fieldKind = "int"
	kindText     fieldKind = "text"
	kindTextarea fieldKind = "textarea"
	kindSelect   fieldKind = "select"
)

// choice is one option of a select.
type choice struct {
	Value any // what TorrServer stores (int or string)
	Label string
}

// settingField is one TorrServer setting as the UI shows it.
type settingField struct {
	Key   string // TorrServer field (BTSets / GStreamer config) or startup option
	Label string
	Help  string
	Kind  fieldKind
	// Int fields: shown value range and unit; TorrServer stores shown*Scale.
	Min, Max, Scale int64
	Unit            string
	// Invert shows a Disable* field as the thing it enables.
	Invert  bool
	Choices []choice
	// Startup marks a command-line-only option of the managed engine.
	Startup bool
	// External marks a setting only a TorrServer Moviestracker does not run
	// shows: Moviestracker sets it for the engine it runs.
	External bool
	// AtStart marks a setting TorrServer reads only when it starts, so the
	// managed engine restarts when it changes while it serves HTTPS.
	AtStart bool
	// Check validates a text field (trimmed); nil accepts anything.
	Check func(string) error
}

// settingsSection is one page of engine settings.
type settingsSection struct {
	ID, Title, Icon, Intro string
	Fields                 []settingField
}

// engineSections are TorrServer's engine settings (BTSets) and the managed
// engine's startup options, grouped by what they are about. Settings that
// duplicate Moviestracker or serve other clients (Rutor, Torznab, TorrServer's
// TMDB key, MCP, storage backend…) are left out and kept as they are.
var engineSections = []settingsSection{
	{
		ID: "engine", Title: "Torrent engine", Icon: "cpu",
		Intro: "How TorrServer finds peers and downloads.",
		Fields: []settingField{
			{Key: "ConnectionsLimit", Label: "Connections per torrent", Kind: kindInt, Min: 1, Max: 500, Scale: 1, Help: "More connections find pieces faster but cost memory and bandwidth. TorrServer's default is 25."},
			{Key: "DownloadRateLimit", Label: "Download limit", Kind: kindInt, Min: 0, Max: 10_000_000, Scale: 1, Unit: "KB/s", Help: "0 means no limit."},
			{Key: "UploadRateLimit", Label: "Upload limit", Kind: kindInt, Min: 0, Max: 10_000_000, Scale: 1, Unit: "KB/s", Help: "0 means no limit. A limit also turns seeding on."},
			{Key: "DisableUpload", Label: "Upload to other peers", Kind: kindBool, Invert: true, Help: "Sharing back keeps swarms healthy; switching it off can make some trackers slower."},
			{Key: "TorrentDisconnectTimeout", Label: "Disconnect idle torrents after", Kind: kindInt, Min: 5, Max: 3600, Scale: 1, Unit: "s", Help: "How long a torrent stays connected after nothing reads from it."},
			{Key: "DisableDHT", Label: "DHT", Kind: kindBool, Invert: true, Help: "Find peers without trackers."},
			{Key: "DisablePEX", Label: "Peer exchange (PEX)", Kind: kindBool, Invert: true, Help: "Learn peers from other peers."},
			{Key: "EnableLPD", Label: "Local peer discovery", Kind: kindBool, Help: "Find peers on your own network."},
			{Key: "ForceEncrypt", Label: "Require encryption", Kind: kindBool, Help: "Talk only to peers that encrypt; fewer peers, harder to throttle."},
			{Key: "DisableTCP", Label: "TCP connections", Kind: kindBool, Invert: true},
			{Key: "DisableUTP", Label: "µTP connections", Kind: kindBool, Invert: true},
			{Key: "ResponsiveMode", Label: "Responsive reading", Kind: kindBool, Help: "Start playback before a piece is fully verified: faster starts, rarely a glitch."},
			{Key: "RetrackersMode", Label: "Extra trackers", Kind: kindSelect, Choices: []choice{
				{0, "Leave torrents as they are"}, {1, "Add the default trackers"}, {2, "Remove all trackers"}, {3, "Replace with the default trackers"},
			}},
			{Key: "TrackersListURL", Label: "Tracker list address", Kind: kindText, Check: optionalHTTPURL, Help: "Where to download the default trackers from; empty uses TorrServer's built-in mirrors."},
			{Key: "DefaultTrackers", Label: "Default trackers", Kind: kindTextarea, Help: "One announce URL per line, used when the list address cannot be reached."},
		},
	},
	{
		ID: "streaming", Title: "Streaming", Icon: "monitor-play",
		Intro: "How much TorrServer reads ahead of the player.",
		Fields: []settingField{
			{Key: "CacheSize", Label: "RAM cache", Kind: kindInt, Min: 32, Max: 16384, Scale: 1 << 20, Unit: "MB", Help: "Memory for the pieces around what is playing. 64–256 MB suits most machines."},
			{Key: "ReaderReadAHead", Label: "Read-ahead", Kind: kindInt, Min: 5, Max: 100, Scale: 1, Unit: "% of the cache", Help: "How much of the cache is spent on pieces after the playback position."},
			{Key: "PreloadCache", Label: "Buffer before playing", Kind: kindInt, Min: 0, Max: 100, Scale: 1, Unit: "% of the cache", Help: "Filled before playback starts; higher means fewer stalls on slow swarms."},
			{Key: "TrackTimecode", Label: "Remember where playback stopped", Kind: kindBool},
			{Key: "MaxSize", Label: "Largest file to stream", Kind: kindInt, Min: 0, Max: 10_000, Scale: 1 << 30, Unit: "GB", Startup: true, Help: "0 means no limit."},
		},
	},
	{
		ID: "storage", Title: "Storage", Icon: "hard-drive",
		Intro: "Whether downloaded pieces stay only in memory or also on disk.",
		Fields: []settingField{
			{Key: "UseDisk", Label: "Cache on disk", Kind: kindBool, Help: "Keeps downloaded pieces on disk so rewatching and seeking need no download."},
			{Key: "TorrentsSavePath", Label: "Disk cache folder", Kind: kindText, Check: optionalAbsolutePath},
			{Key: "RemoveCacheOnDrop", Label: "Delete a torrent's disk cache when it is dropped", Kind: kindBool},
			{Key: "TorrentsDir", Label: "Watch folder", Kind: kindText, Startup: true, Check: optionalAbsolutePath, Help: ".torrent files put here are added automatically."},
		},
	},
	{
		ID: "network", Title: "Network", Icon: "network",
		Intro: "How peers reach TorrServer. Moviestracker keeps TorrServer's web API on this machine; these are for BitTorrent traffic.",
		Fields: []settingField{
			{Key: "PeersListenPort", Label: "Peer port", Kind: kindInt, Min: 0, Max: 65535, Scale: 1, Help: "0 picks a random port. Forward it on your router for more peers."},
			{Key: "DisableUPNP", Label: "Open the peer port with UPnP", Kind: kindBool, Invert: true},
			{Key: "EnableIPv6", Label: "IPv6", Kind: kindBool},
			{Key: "PublicIPv4", Label: "Public IPv4 address", Kind: kindText, Startup: true, Check: optionalIPv4, Help: "Empty lets TorrServer find it."},
			{Key: "PublicIPv6", Label: "Public IPv6 address", Kind: kindText, Startup: true, Check: optionalIPv6},
			{Key: "ProxyURL", Label: "Proxy", Kind: kindText, Startup: true, Check: optionalProxyURL, Help: "socks5://user:password@host:port, or http:// / socks4:// / socks5h://. Empty: no proxy."},
			{Key: "ProxyMode", Label: "Send through the proxy", Kind: kindSelect, Startup: true, Choices: []choice{
				{"tracker", "Tracker requests only"}, {"peers", "Peer connections only"}, {"full", "All BitTorrent traffic"},
			}},
		},
	},
	{
		ID: "sharing", Title: "Other devices", Icon: "cast",
		Intro: "TorrServer can also offer your torrents to DLNA TVs and announce itself on the network.",
		Fields: []settingField{
			{Key: "EnableDLNA", Label: "DLNA media server", Kind: kindBool, Help: "TVs and players that browse DLNA see your torrents."},
			{Key: "EnableBonjour", Label: "Announce with Bonjour", Kind: kindBool, Help: "Lets TorrServer apps find it. Moviestracker keeps it off for the engine it runs."},
			{Key: "FriendlyName", Label: "Name on the network", Kind: kindText},
		},
	},
	{
		ID: "https", Title: "HTTPS", Icon: "lock",
		Intro: "TorrServer's HTTPS certificate. Other devices and the Moviestracker web app use it through Other apps, which also serves HTTPS. TorrServer serves it only when started with --ssl.",
		Fields: []settingField{
			{Key: "HTTPS", Label: "Serve HTTPS", Kind: kindBool, Startup: true, Help: "Without a certificate of your own, TorrServer makes a self-signed one, which each browser asks you to accept once."},
			{Key: "SslPort", Label: "HTTPS port", Kind: kindInt, Min: 0, Max: 65535, Scale: 1, AtStart: true, External: true, Help: "0 uses TorrServer's default, 8091."},
			{Key: "SslCert", Label: "Certificate file", Kind: kindText, AtStart: true, Check: optionalAbsolutePath, Help: "Full path, on the TorrServer machine, to a PEM certificate with its chain. Empty: TorrServer makes a self-signed one."},
			{Key: "SslKey", Label: "Private key file", Kind: kindText, AtStart: true, Check: optionalAbsolutePath, Help: "Full path, on the TorrServer machine, to the certificate's PEM private key."},
		},
	},
}

// gstreamerSection is TorrServer's GStreamer configuration.
var gstreamerSection = settingsSection{
	ID: "gstreamer", Title: "GStreamer", Icon: "film",
	Intro: "How TorrServer turns MKV files into HLS the browser plays. Changes apply at once to new streams.",
	Fields: []settingField{
		{Key: "MaxTasks", Label: "Pipelines at once", Kind: kindInt, Min: 0, Max: 32, Scale: 1, Help: "0 means no limit. Each transcoding pipeline uses CPU or GPU."},
		{Key: "InactiveMinutes", Label: "Stop idle pipelines after", Kind: kindInt, Min: 1, Max: 240, Scale: 1, Unit: "min"},
		{Key: "SegmentSeconds", Label: "Segment length", Kind: kindInt, Min: 2, Max: 30, Scale: 1, Unit: "s"},
		{Key: "SegmentDiff", Label: "Segments prepared ahead", Kind: kindInt, Min: 0, Max: 200, Scale: 1},
		{Key: "Source", Label: "Read files through", Kind: kindSelect, Choices: []choice{{"stream", "/stream (default)"}, {"play", "/play"}}},
		{Key: "Subtitles", Label: "Subtitles", Kind: kindBool, Help: "Offer text subtitles from the file."},
		{Key: "AACBitrateKbps", Label: "Audio bitrate", Kind: kindInt, Min: 64, Max: 640, Scale: 1, Unit: "kbit/s"},
		{Key: "AACChannels", Label: "Audio channels", Kind: kindSelect, Choices: []choice{{0, "Keep the source's"}, {2, "Stereo"}, {6, "5.1"}}},
		{Key: "AACSamplerate", Label: "Audio sample rate", Kind: kindSelect, Choices: []choice{{0, "Keep the source's"}, {44100, "44.1 kHz"}, {48000, "48 kHz"}}},
		{Key: "TranscodeH264", Label: "Transcode H.264", Kind: kindBool, Help: "Off copies the video as is (remux): no quality loss, no CPU."},
		{Key: "TranscodeH265", Label: "Transcode HEVC (H.265)", Kind: kindBool, Help: "On for browsers that cannot play HEVC."},
		{Key: "TranscodeAV1", Label: "Transcode AV1", Kind: kindBool},
		{Key: "TranscodeVP9", Label: "Transcode VP9", Kind: kindBool},
		{Key: "TranscodeVP8", Label: "Transcode VP8", Kind: kindBool},
		{Key: "TranscodeAVI", Label: "Transcode AVI", Kind: kindBool},
		{Key: "HDRToSDR", Label: "Convert HDR to SDR", Kind: kindBool, Help: "Makes HDR files look right on screens and browsers without HDR, by transcoding the video, which costs CPU or GPU."},
		{Key: "HardwareAcceleration", Label: "Hardware encoding", Kind: kindBool},
		{Key: "UseGPU", Label: "Use the GPU", Kind: kindBool},
		{Key: "X264Ultrafast", Label: "Fastest software encoding", Kind: kindBool, Help: "Less CPU, lower quality when transcoding without hardware."},
		{Key: "VideoBitrate", Label: "Video bitrate", Kind: kindInt, Min: 500, Max: 200_000, Scale: 1, Unit: "kbit/s"},
		{Key: "GSTPath", Label: "GStreamer folder", Kind: kindText, Check: optionalAbsolutePath, Help: "Empty uses the system's GStreamer."},
	},
}

func sectionByID(id string) (settingsSection, bool) {
	for _, s := range engineSections {
		if s.ID == id {
			return s, true
		}
	}
	return settingsSection{}, false
}

func optionalHTTPURL(v string) error {
	if v == "" {
		return nil
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("must be an http(s) address")
	}
	return nil
}

func optionalProxyURL(v string) error {
	if v == "" {
		return nil
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" {
		return fmt.Errorf("must be a proxy address like socks5://host:port")
	}
	switch u.Scheme {
	case "http", "socks4", "socks5", "socks5h":
		return nil
	}
	return fmt.Errorf("must start with http://, socks4://, socks5:// or socks5h://")
}

func optionalIPv4(v string) error {
	if ip := net.ParseIP(v); v != "" && (ip == nil || ip.To4() == nil) {
		return fmt.Errorf("must be an IPv4 address like 203.0.113.7")
	}
	return nil
}

func optionalIPv6(v string) error {
	if ip := net.ParseIP(v); v != "" && (ip == nil || ip.To4() != nil) {
		return fmt.Errorf("must be an IPv6 address")
	}
	return nil
}

// windowsPath is a full Windows path (C:\… or \\server\…). A TorrServer's
// folders may be on another system than this one, so full paths of either
// kind are accepted everywhere.
var windowsPath = regexp.MustCompile(`^(?:[A-Za-z]:[\\/]|\\\\[^\\]+\\)`)

func optionalAbsolutePath(v string) error {
	if v != "" && !filepath.IsAbs(v) && !strings.HasPrefix(v, "/") && !windowsPath.MatchString(v) {
		return fmt.Errorf("must be a full path, like /Volumes/Media/cache")
	}
	if strings.ContainsRune(v, 0) {
		return fmt.Errorf("contains an invalid character")
	}
	return nil
}

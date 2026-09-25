package torrserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/config"
)

// Video file extensions commonly streamed.
var videoExtensions = map[string]bool{
	".mp4":  true,
	".mkv":  true,
	".avi":  true,
	".webm": true,
	".mov":  true,
	".wmv":  true,
	".flv":  true,
	".m4v":  true,
	".ts":   true,
	".m2ts": true,
}

// IsVideoFile returns true if the filename has a known video extension.
func IsVideoFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return videoExtensions[ext]
}

// FormatBytes formats byte sizes in human-readable units (B, KB, MB, GB, TB).
func FormatBytes(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	return fmt.Sprintf("%.2f %s", float64(bytes)/float64(div), units[exp])
}

// MainVideoFile picks the largest video file in the torrent to serve as the default playback target.
func MainVideoFile(files []FileStat) *FileStat {
	var best *FileStat
	for i := range files {
		f := &files[i]
		if !IsVideoFile(f.Path) {
			continue
		}
		if best == nil || f.Length > best.Length {
			best = f
		}
	}
	if best == nil && len(files) > 0 {
		return &files[0]
	}
	return best
}

// ErrUnauthorized is TorrServer refusing the login (or asking for one).
var ErrUnauthorized = errors.New("TorrServer refused the login")

// EchoInfo holds TorrServer health and capability data.
type EchoInfo struct {
	Version      string `json:"version"`
	GSTAvailable bool   `json:"gst_available"`
	GSTVersion   string `json:"gst_version"`
	Discoverer   bool   `json:"discoverer"`
	HDRTonemap   bool   `json:"hdr_tonemap"`
}

// Torrent represents a summarized torrent item in the TorrServer database.
type Torrent struct {
	Hash        string  `json:"hash"`
	Title       string  `json:"title"`
	Name        string  `json:"name,omitempty"`
	Data        string  `json:"data,omitempty"`
	Poster      string  `json:"poster,omitempty"`
	Category    string  `json:"category,omitempty"`
	TorrentSize int64   `json:"torrent_size"`
	Stat        int     `json:"stat"`
	StatString  string  `json:"stat_string"`
	LoadedSize  int64   `json:"loaded_size"`
	Preloaded   int64   `json:"preloaded_bytes"`
	Download    float64 `json:"download_speed"`
	Upload      float64 `json:"upload_speed"`
	Connected   int     `json:"connected_seeders"`
	ActivePeers int     `json:"active_peers"`
	TotalPeers  int     `json:"total_peers"`
	// Downloaded and Uploaded are the torrent's data since TorrServer
	// connected it (active torrents only).
	Downloaded int64      `json:"bytes_read_data"`
	Uploaded   int64      `json:"bytes_written_data"`
	FileStats  []FileStat `json:"file_stats,omitempty"`
}

// DisplayName returns Title if available, otherwise Name, otherwise Hash.
func (t Torrent) DisplayName() string {
	if t.Title != "" {
		return t.Title
	}
	if t.Name != "" {
		return t.Name
	}
	return t.Hash
}

// FormattedSize returns a human-readable size for the torrent.
func (t Torrent) FormattedSize() string {
	return FormatBytes(t.TorrentSize)
}

// FormattedSpeed returns formatted download rate (e.g. "0 B/s" or "4.25 MB/s").
func (t Torrent) FormattedSpeed() string {
	return t.FormattedDownloadSpeed()
}

// FormattedDownloadSpeed returns formatted download rate (e.g. "0 B/s" or "4.25 MB/s").
func (t Torrent) FormattedDownloadSpeed() string {
	if t.Download <= 0 {
		return "0 B/s"
	}
	return FormatBytes(int64(t.Download)) + "/s"
}

// BufferPercent is how full TorrServer's read-ahead cache is, 0–100.
func (t Torrent) BufferPercent(cacheSize int64) int {
	if cacheSize <= 0 || t.Preloaded <= 0 {
		return 0
	}
	return int(min(100, t.Preloaded*100/cacheSize))
}

// FormattedUploadSpeed returns formatted upload rate (e.g. "0 B/s" or "250 KB/s").
func (t Torrent) FormattedUploadSpeed() string {
	if t.Upload <= 0 {
		return "0 B/s"
	}
	return FormatBytes(int64(t.Upload)) + "/s"
}

// FormattedPeers returns connected / total peers display string (e.g. "0", "12 / 48").
func (t Torrent) FormattedPeers() string {
	if t.Connected > 0 || t.TotalPeers > 0 {
		return fmt.Sprintf("%d / %d", t.Connected, t.TotalPeers)
	}
	return "0"
}

// statInDB is TorrServer's status of a torrent kept in its database but not
// running.
const statInDB = 5

// GettingInfo reports whether TorrServer is still getting the torrent's info
// (just added or asking peers). It saves a torrent only once it has the
// info, so dropping one before that removes it.
func (t Torrent) GettingInfo() bool {
	return t.Stat == 0 || t.Stat == 1
}

// StatusLabel returns a user-friendly label for the torrent status.
func (t Torrent) StatusLabel() string {
	if t.Stat == 5 || t.StatString == "Torrent in db" {
		return "Dropped (in db)"
	}
	if t.StatString != "" {
		return t.StatString
	}
	switch t.Stat {
	case 0:
		return "Added"
	case 1:
		return "Getting Info"
	case 2:
		return "Preload"
	case 3:
		return "Working"
	case 4:
		return "Closed"
	case 5:
		return "Dropped (in db)"
	default:
		return "Dropped"
	}
}

// StatusBadgeClass returns a DaisyUI badge class for the torrent status.
func (t Torrent) StatusBadgeClass() string {
	switch t.Stat {
	case 3:
		return "badge-success badge-soft text-success"
	case 1, 2:
		return "badge-warning badge-soft text-warning"
	case 5:
		return "badge-neutral badge-soft text-base-content/70"
	default:
		return "badge-ghost text-base-content/60"
	}
}

// MagnetLink generates a standard magnet URI with BTIH hash and display name.
func (t Torrent) MagnetLink() string {
	if t.Hash == "" {
		return ""
	}
	dn := t.DisplayName()
	if dn != "" && dn != t.Hash {
		return fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", t.Hash, url.QueryEscape(dn))
	}
	return fmt.Sprintf("magnet:?xt=urn:btih:%s", t.Hash)
}

// VideoFiles returns only the video files within the torrent.
func (t Torrent) VideoFiles() []FileStat {
	var out []FileStat
	for _, f := range t.FileStats {
		if IsVideoFile(f.Path) {
			out = append(out, f)
		}
	}
	return out
}

// FileStat represents individual file details within a torrent.
type FileStat struct {
	ID     int    `json:"id"`
	Path   string `json:"path"`
	Length int64  `json:"length"`
}

// FormattedLength returns formatted human-readable file size.
func (f FileStat) FormattedLength() string {
	return FormatBytes(f.Length)
}

// TorrentDetails represents full torrent information with individual files.
type TorrentDetails struct {
	Hash        string     `json:"hash"`
	Title       string     `json:"title"`
	Name        string     `json:"name,omitempty"`
	Poster      string     `json:"poster,omitempty"`
	TorrentSize int64      `json:"torrent_size"`
	Stat        int        `json:"stat"`
	StatString  string     `json:"stat_string"`
	TotalPeers  int        `json:"total_peers"`
	FileStats   []FileStat `json:"file_stats"`
}

// DisplayName returns Title if available, otherwise Name, otherwise Hash.
func (d TorrentDetails) DisplayName() string {
	if d.Title != "" {
		return d.Title
	}
	if d.Name != "" {
		return d.Name
	}
	return d.Hash
}

// FormattedSize returns formatted human-readable size for details.
func (d TorrentDetails) FormattedSize() string {
	return FormatBytes(d.TorrentSize)
}

// MagnetLink generates a standard magnet URI with BTIH hash and display name.
func (d TorrentDetails) MagnetLink() string {
	if d.Hash == "" {
		return ""
	}
	dn := d.DisplayName()
	if dn != "" && dn != d.Hash {
		return fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", d.Hash, url.QueryEscape(dn))
	}
	return fmt.Sprintf("magnet:?xt=urn:btih:%s", d.Hash)
}

// ProbeTrack describes an individual stream track discovered by GStreamer.
type ProbeTrack struct {
	Index    int    `json:"Index"`
	Type     string `json:"Type"` // video, audio, sub
	CapsName string `json:"CapsName"`
	Codec    string `json:"Codec"`
	Title    string `json:"Title"`
	Language string `json:"Language"`
	Width    int    `json:"Width,omitempty"`
	Height   int    `json:"Height,omitempty"`
	FPS      string `json:"FPS,omitempty"`
	Channels int    `json:"Channels,omitempty"`
	Rate     int    `json:"Rate,omitempty"`

	FrameRateNum            int    `json:"FrameRateNum,omitempty"`
	FrameRateDen            int    `json:"FrameRateDen,omitempty"`
	BitDepth                int    `json:"BitDepth,omitempty"`
	Colorimetry             string `json:"Colorimetry,omitempty"`
	VideoTransfer           string `json:"VideoTransfer,omitempty"` // pq, hlg or empty (SDR)
	IsDolbyVision           bool   `json:"IsDolbyVision,omitempty"`
	DolbyVisionProfile      int    `json:"DolbyVisionProfile,omitempty"`
	HasMasteringDisplayInfo bool   `json:"HasMasteringDisplayInfo,omitempty"`
	HasContentLightLevel    bool   `json:"HasContentLightLevel,omitempty"`
}

// ProbeResult contains parsed media metadata from GStreamer gst probe.
type ProbeResult struct {
	Container  string       `json:"Container"`
	DurationNS int64        `json:"DurationNS"`
	FileSize   int64        `json:"FileSize"`
	Tracks     []ProbeTrack `json:"Tracks"`
}

// DurationFormatted formats duration in HH:MM:SS.
func (p ProbeResult) DurationFormatted() string {
	if p.DurationNS <= 0 {
		return "N/A"
	}
	d := time.Duration(p.DurationNS)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// StreamFileName is the name a stream link ends with, which players show:
// the file's base name, or the hash for a nameless file.
func StreamFileName(hash string, f FileStat) string {
	name := path.Base(f.Path)
	if name == "." || name == "/" || name == "" {
		name = hash
	}
	return name
}

// GenerateM3UPlaylist creates an M3U playlist for external players (VLC,
// IINA…) with one entry per video file (every file when there is only one).
// link returns the absolute URL of a file's entry.
func GenerateM3UPlaylist(title string, files []FileStat, link func(FileStat) string) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for _, f := range files {
		if !IsVideoFile(f.Path) && len(files) > 1 {
			continue
		}
		displayName := f.Path
		if displayName == "" {
			displayName = title
		}
		fmt.Fprintf(&b, "#EXTINF:-1,%s\n%s\n", displayName, link(f))
	}
	return b.String()
}

// maxResponseBytes caps every TorrServer JSON response. A torrent list with
// file data stays far below it; a wrong endpoint must not stream an
// unbounded body into memory.
const maxResponseBytes = 32 << 20

func decodeJSON(body io.Reader, out any) error {
	return json.NewDecoder(io.LimitReader(body, maxResponseBytes)).Decode(out)
}

// Client interacts with a TorrServer instance over HTTP.
type Client struct {
	baseURL    string
	httpClient *http.Client
	filesCache sync.Map // map[string][]FileStat
	cacheSize  atomic.Pointer[cachedSize]
	now        func() time.Time
}

// cacheSizeTTL is how long a fetched CacheSize setting is trusted: the player
// reads it every second, and it changes only when someone edits the settings.
const cacheSizeTTL = time.Minute

// cachedSize is TorrServer's CacheSize setting as fetched at a point in time.
type cachedSize struct {
	bytes     int64
	fetchedAt time.Time
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithBasicAuth sends HTTP Basic credentials with every request, for a
// TorrServer started with --httpauth.
func WithBasicAuth(user, password string) ClientOption {
	return func(c *Client) {
		if user == "" {
			return
		}
		next := c.httpClient.Transport
		if next == nil {
			next = http.DefaultTransport
		}
		authed := *c.httpClient
		authed.Transport = basicAuthTransport{user: user, password: password, next: next}
		c.httpClient = &authed
	}
}

// basicAuthTransport adds Basic credentials to requests that carry none.
type basicAuthTransport struct {
	user, password string
	next           http.RoundTripper
}

func (t basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Authorization") == "" {
		req = req.Clone(req.Context())
		req.SetBasicAuth(t.user, t.password)
	}
	return t.next.RoundTrip(req)
}

// WithClock overrides the time source used to expire cached settings.
func WithClock(now func() time.Time) ClientOption {
	return func(c *Client) {
		if now != nil {
			c.now = now
		}
	}
}

// NewClient initializes a new TorrServer API client.
func NewClient(baseURL string, httpClient *http.Client, opts ...ClientOption) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 10 * time.Second,
		}
	}
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// CacheFiles stores a list of file stats into the client in-memory cache.
func (c *Client) CacheFiles(hash string, files []FileStat) {
	if len(files) > 0 {
		c.filesCache.Store(hash, files)
	}
}

// GetCachedFiles retrieves cached file stats for a hash if available.
func (c *Client) GetCachedFiles(hash string) ([]FileStat, bool) {
	if cached, ok := c.filesCache.Load(hash); ok {
		if files, valid := cached.([]FileStat); valid && len(files) > 0 {
			return files, true
		}
	}
	return nil, false
}

// gstComponent is one component's status in TorrServer's /gst/echo answer.
type gstComponent struct {
	Available bool   `json:"available"`
	Works     bool   `json:"works"`
	Version   string `json:"version"`
}

// Echo inspects TorrServer version and tests for GStreamer HLS/transcoding support.
func (c *Client) Echo(ctx context.Context) (EchoInfo, error) {
	var info EchoInfo

	// 1. Fetch version from /echo
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/echo", nil)
	if err != nil {
		return info, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return info, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("torrserver echo returned status %d", resp.StatusCode)
	}

	// A version string is tiny; the cap keeps a wrong endpoint from
	// streaming an arbitrary body into memory.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	info.Version = strings.TrimSpace(string(body))

	// 2. Fetch GStreamer info from /gst/echo
	gstReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/gst/echo", nil)
	if err == nil {
		gstResp, gstErr := c.httpClient.Do(gstReq)
		if gstErr == nil {
			defer func() { _ = gstResp.Body.Close() }()
			if gstResp.StatusCode == http.StatusOK {
				var gstData struct {
					GStreamer     gstComponent `json:"gstreamer"`
					Discoverer    gstComponent `json:"gst_discoverer"`
					HDRToneMapper gstComponent `json:"hdr_tone_mapping"`
				}
				if decodeJSON(gstResp.Body, &gstData) == nil {
					info.GSTAvailable = gstData.GStreamer.Available && gstData.GStreamer.Works
					info.GSTVersion = gstData.GStreamer.Version
					info.Discoverer = gstData.Discoverer.Works
					info.HDRTonemap = gstData.HDRToneMapper.Works
				}
			}
		}
	}

	return info, nil
}

// resolveFiles fills a listed torrent's files: from the list itself (active
// torrents), from files fetched earlier, or from the TorrServer DB data of a
// dropped torrent. It makes no request.
func (c *Client) resolveFiles(t *Torrent) {
	if len(t.FileStats) > 0 {
		c.filesCache.Store(t.Hash, t.FileStats)
		return
	}
	if files, ok := c.GetCachedFiles(t.Hash); ok {
		t.FileStats = files
		return
	}
	if t.Data == "" {
		return
	}
	var parsed struct {
		TorrServer struct {
			Files []FileStat `json:"Files"`
		} `json:"TorrServer"`
	}
	if err := json.Unmarshal([]byte(t.Data), &parsed); err == nil && len(parsed.TorrServer.Files) > 0 {
		t.FileStats = parsed.TorrServer.Files
		c.filesCache.Store(t.Hash, t.FileStats)
	}
}

// ListTorrents retrieves all torrents stored in the TorrServer instance with
// their files. TorrServer's list already carries each active torrent's live
// speeds and peers; nothing else is requested, because TorrServer's per-torrent
// endpoints (/cache, "get") keep an active torrent alive and start a dropped one.
func (c *Client) ListTorrents(ctx context.Context) ([]Torrent, error) {
	reqBody := `{"action":"list"}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/torrents", strings.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("torrserver returned status %d", resp.StatusCode)
	}

	var list []Torrent
	if err := decodeJSON(resp.Body, &list); err != nil {
		return nil, err
	}
	for i := range list {
		c.resolveFiles(&list[i])
	}
	return list, nil
}

// GetTorrent fetches full details and file stats for a torrent by its hash.
func (c *Client) GetTorrent(ctx context.Context, hash string) (*TorrentDetails, error) {
	var details TorrentDetails
	if err := c.getTorrent(ctx, hash, &details); err != nil {
		return nil, err
	}
	return &details, nil
}

// ErrTorrentNotFound reports a hash TorrServer does not list.
var ErrTorrentNotFound = errors.New("torrent not found on TorrServer")

// TorrentStats returns a torrent's live swarm and cache statistics. It reads
// them from the list, so polling it neither starts a dropped torrent nor keeps
// an idle one from disconnecting.
func (c *Client) TorrentStats(ctx context.Context, hash string) (*Torrent, error) {
	list, err := c.ListTorrents(ctx)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if strings.EqualFold(list[i].Hash, hash) {
			return &list[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrTorrentNotFound, hash)
}

func (c *Client) getTorrent(ctx context.Context, hash string, out any) error {
	reqBody, _ := json.Marshal(map[string]string{
		"action": "get",
		"hash":   hash,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/torrents", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("torrserver returned status %d", resp.StatusCode)
	}
	return decodeJSON(resp.Body, out)
}

// CacheSize returns TorrServer's configured cache size in bytes. The player
// polls it every second, so a fetched value is reused for cacheSizeTTL.
func (c *Client) CacheSize(ctx context.Context) (int64, error) {
	if cached := c.cacheSize.Load(); cached != nil && c.now().Sub(cached.fetchedAt) < cacheSizeTTL {
		return cached.bytes, nil
	}
	reqBody, _ := json.Marshal(map[string]string{"action": "get"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/settings", bytes.NewReader(reqBody))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("settings failed with status %d", resp.StatusCode)
	}
	var settings struct {
		CacheSize int64 `json:"CacheSize"`
	}
	if err := decodeJSON(resp.Body, &settings); err != nil {
		return 0, err
	}
	c.cacheSize.Store(&cachedSize{bytes: settings.CacheSize, fetchedAt: c.now()})
	return settings.CacheSize, nil
}

// FetchTorrentFiles explicitly fetches files for a torrent upon user request, caches them,
// and drops the torrent cache so TorrServer stays dropped and does not download in the background.
func (c *Client) FetchTorrentFiles(ctx context.Context, hash string) (*TorrentDetails, error) {
	details, err := c.GetTorrent(ctx, hash)
	if err != nil {
		return nil, err
	}
	if details != nil && len(details.FileStats) > 0 {
		c.filesCache.Store(hash, details.FileStats)
	}
	// Asking for a torrent kept in the database wakes it: put it back to
	// sleep so it does not download. Leave any other alone: one still getting
	// info is not saved yet (dropping would remove it), one working may play.
	if details != nil && details.Stat == statInDB {
		_ = c.DropTorrent(ctx, hash)
	}
	return details, nil
}

// UploadTorrent adds a .torrent file and has TorrServer keep it; title
// names it when not empty.
func (c *Client) UploadTorrent(ctx context.Context, filename string, data []byte, title string) error {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return err
	}
	if _, err := fw.Write(data); err != nil {
		return err
	}
	_ = mw.WriteField("save", "true")
	if title != "" {
		_ = mw.WriteField("title", title)
	}
	if err := mw.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/torrent/upload", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upload torrent failed with status %d", resp.StatusCode)
	}
	return nil
}

// AddTorrent adds a torrent by magnet URI, HTTP/HTTPS link, or hash.
// It instructs TorrServer to save the torrent to DB once metadata is fetched.
func (c *Client) AddTorrent(ctx context.Context, link, title, poster, category string) error {
	payload := map[string]any{
		"action":     "add",
		"link":       link,
		"title":      title,
		"poster":     poster,
		"category":   category,
		"save_to_db": true,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/torrents", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("add torrent failed with status %d", resp.StatusCode)
	}

	return nil
}

// RemoveTorrent deletes a torrent from the TorrServer database and invalidates local cache.
func (c *Client) RemoveTorrent(ctx context.Context, hash string) error {
	c.filesCache.Delete(hash)
	payload := map[string]string{
		"action": "rem",
		"hash":   hash,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/torrents", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remove torrent failed with status %d", resp.StatusCode)
	}
	return nil
}

// DropTorrent drops the active cache and closes connections for a torrent.
func (c *Client) DropTorrent(ctx context.Context, hash string) error {
	payload := map[string]string{
		"action": "drop",
		"hash":   hash,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/torrents", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("drop torrent cache failed with status %d", resp.StatusCode)
	}
	return nil
}

// Probe calls GStreamer probe endpoint to extract container, audio, and subtitle streams.
func (c *Client) Probe(ctx context.Context, hash string, fileIndex int) (*ProbeResult, error) {
	endpoint := fmt.Sprintf("%s/gst/%s/probe?index=%d", c.baseURL, url.PathEscape(hash), fileIndex)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("probe failed with status %d", resp.StatusCode)
	}

	var res ProbeResult
	if err := decodeJSON(resp.Body, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Heartbeat keeps the GStreamer transcode/remux pipeline alive while playing.
func (c *Client) Heartbeat(ctx context.Context, hash string) error {
	endpoint := fmt.Sprintf("%s/gst/%s/heartbeat", c.baseURL, url.PathEscape(hash))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return nil
}

// Manager holds the TorrServer this app talks to. The address is set by the
// administrator (Settings → Sources) and can change while the app runs.
type Manager struct {
	mu             sync.RWMutex
	activeURL      string
	user, password string
	client         *Client
}

// NewManager creates a Manager for rawURL, or http://127.0.0.1:8090 when it
// is not a valid address.
func NewManager(rawURL string) *Manager {
	u, err := NormalizeServerURL(rawURL)
	if err != nil {
		u = "http://127.0.0.1:8090"
	}
	return &Manager{activeURL: u, client: NewClient(u, nil)}
}

// NewManagerFor creates a Manager for a saved TorrServer: its address and,
// for one started with --httpauth, its login.
func NewManagerFor(ts config.TorrServer) *Manager {
	m := NewManager(ts.URL)
	if ts.User != "" {
		_ = m.SetEndpoint(m.ActiveURL(), ts.User, ts.Password)
	}
	return m
}

// Client returns the client for the current TorrServer address.
func (m *Manager) Client() *Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.client
}

// ActiveURL returns the current TorrServer address.
func (m *Manager) ActiveURL() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeURL
}

// NormalizeServerURL validates a TorrServer endpoint (absolute http/https URL
// with a host) and strips trailing slashes and any login in it.
func NormalizeServerURL(raw string) (string, error) {
	addr, _, _, err := ParseServerURL(raw)
	return addr, err
}

// ParseServerURL splits a TorrServer address typed as
// http(s)://[user:password@]host[:port] into the address and its login, so
// the login never travels in (or is shown with) the address.
func ParseServerURL(raw string) (addr, user, password string, err error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", "", "", fmt.Errorf("invalid TorrServer URL: want http(s)://host[:port]")
	}
	if u.User != nil {
		user = u.User.Username()
		password, _ = u.User.Password()
		u.User = nil
	}
	return strings.TrimRight(u.String(), "/"), user, password, nil
}

// SetURL switches to another TorrServer address that needs no credentials.
func (m *Manager) SetURL(raw string) error {
	return m.SetEndpoint(raw, "", "")
}

// SetEndpoint switches to the TorrServer at raw, reached with the given Basic
// credentials (none when user is empty).
func (m *Manager) SetEndpoint(raw, user, password string) error {
	u, err := NormalizeServerURL(raw)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeURL != u || m.user != user || m.password != password {
		m.activeURL, m.user, m.password = u, user, password
		m.client = NewClient(u, nil, WithBasicAuth(user, password))
	}
	return nil
}

// Credentials are the Basic credentials of the current TorrServer, for
// requests made outside Client (the stream proxy).
func (m *Manager) Credentials() (user, password string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.user, m.password
}

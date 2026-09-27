package handlers

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/engine"
	"github.com/lieranderl/moviestracker-app/internal/gstinstall"
	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/live"
	"github.com/lieranderl/moviestracker-app/internal/sources"
	"github.com/lieranderl/moviestracker-app/internal/stats"
	playback "github.com/lieranderl/moviestracker-app/internal/streams"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"

	"github.com/a-h/templ"
	"github.com/starfederation/datastar-go/datastar"
)

// slowRefresh is how often the system topic re-reads TorrServer's storage
// settings and walks the disk cache folder (a walk can be long).
const slowRefresh = time.Minute

// systemState is this machine plus what the engine's settings say about storage.
type systemState struct {
	System stats.System
	// EngineElsewhere: TorrServer runs on another machine, so this one
	// cannot measure it.
	EngineElsewhere bool
	DataDir         string
	CacheSize       int64  // TorrServer's RAM cache, bytes
	DiskCache       string // the disk cache folder, when disk caching is on
	SegmentSeconds  int    // GStreamer HLS segment length
	// The last few minutes, oldest first: CPU and memory in %, network in B/s.
	CPUHist, MemHist, NetDownHist, NetUpHist []float64
}

// systemFetch samples the machine every call; TorrServer's storage settings
// and the disk cache's size are refreshed at most every slowRefresh.
func (s *Server) systemFetch() func(context.Context) (any, error) {
	var (
		mu        sync.Mutex
		readAt    time.Time
		cacheSize int64
		diskCache string
		segment   = 6
		folderAt  time.Time
		folder    int64
		portAt    time.Time
		portPID   int32
		cpuH      = stats.NewHistory(sparkSamples)
		memH      = stats.NewHistory(sparkSamples)
		netDownH  = stats.NewHistory(sparkSamples)
		netUpH    = stats.NewHistory(sparkSamples)
	)
	return func(ctx context.Context) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(readAt) > slowRefresh {
			// Read in the background: the machine's figures never wait on a
			// slow or unreachable TorrServer.
			readAt = time.Now()
			client := s.torrServer.Client()
			go func() {
				ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				defer cancel()
				sets, setsErr := client.Settings(ctx)
				gst, gstErr := client.GSTSettings(ctx)
				mu.Lock()
				defer mu.Unlock()
				if setsErr == nil {
					cacheSize, diskCache = sets.Int64("CacheSize"), ""
					if sets.Bool("UseDisk") {
						diskCache = sets.String("TorrentsSavePath")
					}
				}
				if gstErr == nil && gst.BuiltIn {
					segment = max(gst.Config.Int("SegmentSeconds"), 1)
				}
			}()
		}
		watch := stats.Watch{Disks: []string{s.store.Dir()}}
		elsewhere := false
		switch port, local := engineLocalPort(s.torrServer.ActiveURL()); {
		case s.managed():
			if st := s.engine.Status(); st.State == engine.Running {
				watch.EnginePID = int32(st.PID) // #nosec G115 -- PIDs fit in int32
			}
		case local:
			// The user's own TorrServer on this machine: find it by its port,
			// once a minute (the socket table is slow to read).
			if time.Since(portAt) > slowRefresh {
				portAt = time.Now()
				portPID, _ = stats.ListeningPID(ctx, port)
			}
			watch.EnginePID = portPID
		default:
			elsewhere = true
		}
		if diskCache != "" {
			watch.Disks = append(watch.Disks, diskCache)
			if time.Since(folderAt) > slowRefresh {
				watch.Folders = []string{diskCache}
			}
		}
		sys := s.sampler.Sample(ctx, watch)
		if size, ok := sys.Folders[diskCache]; ok {
			folder, folderAt = size, time.Now()
		}
		if diskCache != "" {
			sys.Folders = map[string]int64{diskCache: folder}
		}
		if sys.CPU.Cores > 0 {
			cpuH.Add(sys.CPU.Percent)
		}
		if sys.Memory.Total > 0 {
			memH.Add(float64(sys.Memory.Used) * 100 / float64(sys.Memory.Total))
		}
		if sys.Net.Measured {
			netDownH.Add(sys.Net.Down)
			netUpH.Add(sys.Net.Up)
		}
		return systemState{
			System: sys, EngineElsewhere: elsewhere, DataDir: s.store.Dir(), CacheSize: cacheSize, DiskCache: diskCache, SegmentSeconds: segment,
			CPUHist: cpuH.Values(), MemHist: memH.Values(), NetDownHist: netDownH.Values(), NetUpHist: netUpH.Values(),
		}, nil
	}
}

func (s *Server) handleDashboardPage(w http.ResponseWriter, r *http.Request) {
	user := s.pageUser(w, r)
	if user == nil {
		return
	}
	templ.Handler(views.DashboardPage(user)).ServeHTTP(w, r)
}

// dashRefresh redraws the dashboard even when no topic changed: people,
// problems and finished streams change between polls.
const dashRefresh = 5 * time.Second

// handleDashboardStream keeps every dashboard card current from the shared
// topics; a card is patched only when its HTML changes.
func (s *Server) handleDashboardStream(w http.ResponseWriter, r *http.Request) {
	user := s.apiUser(w, r)
	if user == nil {
		return
	}
	if !s.acquireSSE() {
		w.Header().Set("Retry-After", "2")
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}
	defer s.releaseSSE()
	sse := datastar.NewSSE(w, r)
	ctx, cancel := s.streamContext(r)
	defer cancel()
	sub := s.live.Subscribe(topicEngine, topicTorrents, topicPlays, topicSystem, topicSources, topicGStreamer)
	defer sub.Close()
	tick := time.NewTicker(dashRefresh)
	defer tick.Stop()

	last := map[string]*string{}
	patch := func(id string, c templ.Component) error {
		if last[id] == nil {
			last[id] = new(string)
		}
		return patchIfChanged(ctx, sse, c, last[id])
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.C:
		case <-tick.C:
		}
		now := time.Now()
		echo := sub.Snapshot(topicEngine)
		torrents := liveValue[torrentsState](sub, topicTorrents)
		system := liveValue[systemState](sub, topicSystem)
		plays := liveValue[[]playback.Session](sub, topicPlays)
		people := s.dashPeople(ctx, now)
		played := s.dashPlayed(ctx, now, torrents.List)
		problems, engineNote := s.dashProblems(ctx)
		for _, card := range []struct {
			id string
			c  templ.Component
		}{
			{"pulse", views.DashPulseStrip(dashPulse(ctx, plays, people, torrents, system))},
			{"streams", views.DashStreamsCard(s.dashStreams(ctx, plays, torrents.List, system))},
			{"played", views.DashPlayedCard(played)},
			{"swarm", views.DashSwarmCard(dashSwarm(ctx, torrents, plays, system.CacheSize))},
			{"app", views.DashAppCard(s.dashApp(ctx, system, echo, plays))},
			{"system", views.DashSystemCard(dashMachine(ctx, system))},
			{"people", views.DashPeopleCard(people)},
			{"sources", views.DashSourcesCard(dashSources(ctx, liveValue[map[string]sources.ServiceHealth](sub, topicSources)))},
			{"problems", views.DashProblemsCard(problems, engineNote)},
			{"gstreamer", views.GStreamerCard(s.gstSetup(user, liveValue[gstinstall.Status](sub, topicGStreamer), liveValue[torrserver.EchoInfo](sub, topicEngine)), true)},
		} {
			if err := patch(card.id, card.c); err != nil {
				logSSEError(r, "patch dashboard "+card.id, err)
				return
			}
		}
	}
}

// dashPulse is the headline strip.
func dashPulse(ctx context.Context, plays []playback.Session, people []views.DashPerson, t torrentsState, sys systemState) views.DashPulse {
	shared, other := 0, 0
	for _, p := range plays {
		switch {
		case p.OtherApp:
			other++
		case p.Viewer == sharedViewer:
			shared++
		}
	}
	devices := 0
	for _, p := range people {
		devices += len(p.Devices)
	}
	totals := stats.TotalsOf(t.List)
	v := views.DashPulse{
		Playing: fmt.Sprint(len(plays)), Online: fmt.Sprint(len(people)),
		OnlineNote: i18n.N(ctx, devices, "%d device", "%d devices"),
		Down:       speed(totals.Download), Up: speed(totals.Upload),
		DownNote:  i18n.N(ctx, totals.Active, "%d active torrent", "%d active torrents"),
		UpNote:    i18n.N(ctx, totals.Peers, "%d peer", "%d peers"),
		DownSpark: t.Down, UpSpark: t.Up,
	}
	var where []string
	if inApp := len(plays) - shared - other; inApp > 0 {
		where = append(where, i18n.Tf(ctx, "%d in the app", inApp))
	}
	if shared > 0 {
		where = append(where, i18n.N(ctx, shared, "%d shared link", "%d shared links"))
	}
	if other > 0 {
		where = append(where, i18n.N(ctx, other, "%d in another app", "%d in other apps"))
	}
	v.PlayingNote = strings.Join(where, " · ")
	if len(plays) == 0 {
		v.PlayingNote = i18n.T(ctx, "nothing right now")
	}
	return v
}

// countOf says "1 device", "3 devices".
// viewerName is who plays, as the page shows them: shared links are named
// in its language, people by their account.
func viewerName(ctx context.Context, viewer string) string {
	if viewer == sharedViewer {
		return i18n.T(ctx, sharedViewer)
	}
	return viewer
}

func (s *Server) dashStreams(ctx context.Context, plays []playback.Session, list []torrserver.Torrent, sys systemState) []views.DashStream {
	byHash := map[string]torrserver.Torrent{}
	for _, t := range list {
		byHash[strings.ToLower(t.Hash)] = t
	}
	out := make([]views.DashStream, 0, len(plays))
	for _, p := range plays {
		t := byHash[p.Hash]
		row := views.DashStream{
			Title:    t.DisplayName(),
			Poster:   t.Poster,
			Viewer:   viewerName(ctx, p.Viewer),
			OtherApp: p.OtherApp,
			Client:   p.Client,
			Since:    humanDuration(ctx, time.Since(p.Started)),
			Sent:     size(p.Bytes),
			Delivery: speed(p.Rate),
			Download: speed(t.Download),
			Peers:    t.FormattedPeers(),
			Buffer:   t.BufferPercent(sys.CacheSize),
		}
		if row.Title == "" {
			row.Title = p.Hash[:min(len(p.Hash), 12)] + "…"
		}
		var size int64
		for _, f := range t.FileStats {
			if f.ID == p.File {
				row.File, size = torrserver.StreamFileName(p.Hash, f), f.Length
			}
		}
		if p.Kind == playback.HLS {
			row.Kind = "HLS"
			row.Position = i18n.T(ctx, "starting")
			if p.Segment >= 0 {
				row.Position = formatDuration(p.Position(time.Duration(sys.SegmentSeconds) * time.Second))
			}
			if out, ok := s.hlsOutputs.load(hlsOutputKey{p.Hash, p.File, p.Audio}); ok {
				row.Output = strings.TrimSpace(fmt.Sprintf("%s %s", codecName(out.VideoCodec), resolution(out.Width, out.Height)))
			}
		} else {
			row.Kind = i18n.T(ctx, "Direct")
			row.Position = fmt.Sprintf("%.0f%%", p.Progress(size))
		}
		// The player takes data faster than the torrent brings it, and little
		// is buffered ahead: playback is about to wait for the network.
		if p.Rate > 0 && t.Download < p.Rate && row.Buffer < 30 && (size == 0 || t.LoadedSize < size) {
			row.Warn = i18n.T(ctx, "Downloading slower than it plays: it may pause to buffer.")
		}
		out = append(out, row)
	}
	return out
}

// dashPlayed lists today's finished streams, newest first.
func (s *Server) dashPlayed(ctx context.Context, now time.Time, list []torrserver.Torrent) []views.DashPlayed {
	byHash := map[string]torrserver.Torrent{}
	for _, t := range list {
		byHash[strings.ToLower(t.Hash)] = t
	}
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	var rows []views.DashPlayed
	for _, p := range s.plays.Finished() {
		if p.Ended.Before(today) {
			continue
		}
		t := byHash[p.Hash]
		title := t.DisplayName()
		if title == "" {
			title = p.Hash[:min(len(p.Hash), 12)] + "…"
		}
		kind := i18n.T(ctx, "Direct")
		if p.Kind == playback.HLS {
			kind = "HLS"
		}
		rows = append(rows, views.DashPlayed{
			When: p.Started.Format("15:04"), Duration: humanDuration(ctx, p.Ended.Sub(p.Started)),
			Title: title, Viewer: viewerOf(ctx, p), Device: p.Client, Kind: kind, Sent: size(p.Bytes),
		})
	}
	return rows
}

// viewerOf names who played p in a line of text: an other app says so.
func viewerOf(ctx context.Context, p playback.Session) string {
	switch {
	case !p.OtherApp:
		return viewerName(ctx, p.Viewer)
	case p.Viewer == "":
		return i18n.T(ctx, "Other app")
	default:
		return i18n.Tf(ctx, "%s (other app)", p.Viewer)
	}
}

func dashSwarm(ctx context.Context, t torrentsState, plays []playback.Session, cacheSize int64) views.DashSwarm {
	totals := stats.TotalsOf(t.List)
	v := views.DashSwarm{
		Torrents: totals.Torrents, Active: totals.Active, Peers: totals.Peers, Seeders: totals.Seeders,
		Down: speed(totals.Download), Up: speed(totals.Upload), Loaded: size(totals.Loaded),
		Downloaded: size(totals.Downloaded), Uploaded: size(totals.Uploaded),
		DownSpark: t.Down, UpSpark: t.Up,
	}
	active := slices.DeleteFunc(slices.Clone(t.List), func(tr torrserver.Torrent) bool { return tr.Stat < 1 || tr.Stat > 3 })
	slices.SortStableFunc(active, func(a, b torrserver.Torrent) int { return cmp.Compare(b.Download, a.Download) })
	for _, tr := range active {
		row := views.DashTorrent{
			Title: tr.DisplayName(), Status: i18n.T(ctx, tr.StatusLabel()), StatusClass: tr.StatusBadgeClass(),
			Size: size(tr.TorrentSize), Down: speed(tr.Download), Up: speed(tr.Upload),
			Peers:      i18n.Tf(ctx, "%d of %d · %d seeders", tr.ActivePeers, tr.TotalPeers, tr.Connected),
			Downloaded: size(tr.Downloaded), Uploaded: size(tr.Uploaded),
			Buffer: tr.BufferPercent(cacheSize),
		}
		for _, p := range plays {
			if p.Hash != strings.ToLower(tr.Hash) {
				continue
			}
			file := i18n.Tf(ctx, "File %d", p.File)
			for _, f := range tr.FileStats {
				if f.ID == p.File {
					file = torrserver.StreamFileName(p.Hash, f)
				}
			}
			row.Playing = append(row.Playing, views.DashTorrentPlay{File: file, Viewer: viewerName(ctx, p.Viewer), OtherApp: p.OtherApp})
		}
		v.Rows = append(v.Rows, row)
	}
	return v
}

// dashPeople groups the devices in use now by person.
func (s *Server) dashPeople(ctx context.Context, now time.Time) []views.DashPerson {
	var out []views.DashPerson
	for _, d := range s.presence.online(now) {
		name := d.User
		if u := s.accounts.Lookup(d.User); u != nil && u.Name != "" && u.Name != d.User {
			name = u.Name + " (" + d.User + ")"
		}
		if len(out) == 0 || out[len(out)-1].Name != name {
			out = append(out, views.DashPerson{Name: name})
		}
		seen := i18n.T(ctx, "live page open")
		if !d.Live {
			seen = ago(ctx, now.Sub(d.LastSeen))
		}
		p := &out[len(out)-1]
		p.Devices = append(p.Devices, views.DashDevice{Name: localDevice(ctx, d.Name), IP: d.IP, Seen: seen, Live: d.Live})
	}
	return out
}

// problemRows is how many recent problems the card lists.
const problemRows = 6

// dashProblems lists recent warnings and errors, and the managed engine's
// restarts, if any.
func (s *Server) dashProblems(ctx context.Context) ([]views.DashProblem, string) {
	var rows []views.DashProblem
	if s.events != nil {
		for _, e := range s.events.Recent() {
			if len(rows) == problemRows {
				break
			}
			rows = append(rows, views.DashProblem{Time: e.Time.Format("15:04"), Message: e.Message, Detail: e.Detail, Error: e.Level >= slog.LevelError})
		}
	}
	engineNote := ""
	if s.managed() {
		st := s.engine.Status()
		switch {
		case st.State != engine.Running && st.LastError != "":
			engineNote = i18n.Tf(ctx, "TorrServer is not running: %s", st.LastError)
		case st.Restarts > 0:
			engineNote = i18n.N(ctx, st.Restarts, "TorrServer restarted %d time since Moviestracker started.", "TorrServer restarted %d times since Moviestracker started.")
		}
	}
	return rows, engineNote
}

// engineLocalPort is the port of a TorrServer address on this machine.
func engineLocalPort(address string) (int, bool) {
	u, err := url.Parse(address)
	if err != nil {
		return 0, false
	}
	host := strings.ToLower(u.Hostname())
	ip, ipErr := netip.ParseAddr(host)
	if host != "localhost" && (ipErr != nil || !ip.IsLoopback()) {
		return 0, false
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		port = 80
		if u.Scheme == "https" {
			port = 443
		}
	}
	return port, true
}

// dashMachine is the machine card: CPU, memory, network and disks of the
// whole computer.
func dashMachine(ctx context.Context, sys systemState) views.DashSystem {
	v := views.DashSystem{NetDownSpark: sys.NetDownHist, NetUpSpark: sys.NetUpHist, CPUSpark: sys.CPUHist, MemSpark: sys.MemHist}
	if c := sys.System.CPU; c.Cores > 0 {
		v.CPUPct = int(c.Percent + 0.5)
		v.CPU = fmt.Sprintf("%d%% · %s", v.CPUPct, i18n.N(ctx, c.Cores, "%d core", "%d cores"))
		if c.Load1 > 0 {
			v.Load = fmt.Sprintf("%.2f · %.2f · %.2f", c.Load1, c.Load5, c.Load15)
		}
	}
	if m := sys.System.Memory; m.Total > 0 {
		v.MemUsed, v.MemTotal, v.MemPct = size(m.Used), size(m.Total), int(m.Used*100/m.Total)
	}
	if n := sys.System.Net; n.Measured {
		v.NetDown, v.NetUp = speed(n.Down), speed(n.Up)
	}
	dirs := []struct{ label, dir string }{{i18n.T(ctx, "Disk with the data folder"), sys.DataDir}}
	if sys.DiskCache != "" {
		dirs = append(dirs, struct{ label, dir string }{i18n.T(ctx, "Disk with the disk cache"), sys.DiskCache})
	}
	for _, d := range dirs {
		if disk, ok := sys.System.Disks[d.dir]; ok && disk.Total > 0 {
			v.Disks = append(v.Disks, views.DashDisk{
				Label: d.label, Free: size(disk.Free), Total: size(disk.Total),
				UsedPct: int(100 - disk.Free*100/disk.Total),
			})
		}
	}
	return v
}

// dashApp is the app card: Moviestracker, TorrServer and its GStreamer,
// what they use together, and TorrServer's caches.
func (s *Server) dashApp(ctx context.Context, sys systemState, echoSnap live.Snapshot, plays []playback.Session) views.DashApp {
	proc := func(p stats.Process) (string, string) {
		return size(p.RSS), fmt.Sprintf("%.0f%%", p.CPU)
	}
	v := views.DashApp{RAMCache: size(sys.CacheSize), DiskCache: sys.DiskCache}
	if sys.DiskCache != "" {
		v.DiskCacheUsed = size(sys.System.Folders[sys.DiskCache])
	}
	if g := sys.System.Go; g.Version != "" {
		v.Go, v.Goroutines, v.Heap = g.Version, fmt.Sprint(g.Goroutines), size(g.HeapInUse)
		v.GC = i18n.Tf(ctx, "%d · last %s", g.GCs, g.LastPause.Round(time.Microsecond))
		sent, skipped := patchesSent.Load(), patchesSkipped.Load()
		v.LiveStreams = fmt.Sprint(s.live.Subscribers())
		v.Patches = fmt.Sprintf("%d · %d", sent, skipped)
	}
	together := sys.System.Self
	if e := sys.System.Engine; e != nil {
		together.RSS += e.RSS
		together.CPU += e.CPU
		v.Memory, v.CPU = proc(together)
	}

	self := views.DashProc{Name: "Moviestracker", Detail: s.version + " · " + i18n.Tf(ctx, "up %s", humanDuration(ctx, time.Since(s.startedAt))), Known: true, Online: true}
	self.Memory, self.CPU = proc(sys.System.Self)

	echo, _ := echoSnap.Value.(torrserver.EchoInfo)
	known := echoSnap.Version > 0
	engineRow := views.DashProc{Name: "TorrServer", Known: known, Online: echoSnap.Err == nil && echo.Version != ""}
	switch {
	case !known:
		engineRow.Detail = i18n.T(ctx, "checking…")
	case engineRow.Online:
		label := []rune(i18n.T(ctx, s.engineLabel())) // "Run by Moviestracker": lowercase only its first letter
		engineRow.Detail = echo.Version + " · " + strings.ToLower(string(label[:1])) + string(label[1:])
	default:
		engineRow.Detail = i18n.T(ctx, "not answering")
	}
	if sys.System.Engine != nil {
		engineRow.Memory, engineRow.CPU = proc(*sys.System.Engine)
	} else if sys.EngineElsewhere {
		engineRow.Note = i18n.T(ctx, "runs on another machine: not measured here")
	}

	gst := views.DashProc{Name: "GStreamer", Nested: true, Known: known && engineRow.Online, Online: echo.GSTAvailable}
	switch {
	case !gst.Known:
	case echo.GSTAvailable:
		streams, outputs := 0, []string{}
		for _, p := range plays {
			if p.Kind != playback.HLS {
				continue
			}
			streams++
			if out, ok := s.hlsOutputs.load(hlsOutputKey{p.Hash, p.File, p.Audio}); ok {
				if o := strings.TrimSpace(codecName(out.VideoCodec) + " " + resolution(out.Width, out.Height)); o != "" && !slices.Contains(outputs, o) {
					outputs = append(outputs, o)
				}
			}
		}
		detail := []string{echo.GSTVersion, i18n.T(ctx, "idle")}
		if streams > 0 {
			detail[1] = i18n.N(ctx, streams, "%d HLS stream", "%d HLS streams")
		}
		detail = append(detail, outputs...)
		if echo.HDRTonemap {
			detail = append(detail, i18n.T(ctx, "HDR tone mapping"))
		}
		gst.Detail = strings.Join(detail, " · ")
		gst.Note = i18n.T(ctx, "part of TorrServer: its CPU and memory are counted there")
	default:
		gst.Detail = i18n.T(ctx, "not installed")
		gst.Note = i18n.T(ctx, "MKV files play in VLC, on TVs and in other players, not in the browser")
	}
	v.Procs = []views.DashProc{self, engineRow, gst}
	return v
}

func dashSources(ctx context.Context, health map[string]sources.ServiceHealth) []views.DashService {
	out := make([]views.DashService, 0, 3)
	for _, name := range []string{"TMDB", "JacRed", "IMDb"} {
		h, called := health[name]
		row := views.DashService{Name: name, Known: called, OK: h.OK, Error: h.Error}
		if called {
			row.Latency = h.Latency.Round(time.Millisecond).String()
			row.Ago = ago(ctx, time.Since(h.LastCall))
			row.Calls = i18n.N(ctx, h.Calls, "%d call in the last hour", "%d calls in the last hour")
			switch h.Failures {
			case 0:
				row.Calls += " · " + i18n.T(ctx, "no failures")
			default:
				row.Calls += " · " + i18n.N(ctx, h.Failures, "%d failure", "%d failures")
			}
		}
		out = append(out, row)
	}
	return slices.Clip(out)
}

func speed(bytesPerSecond float64) string {
	if bytesPerSecond <= 0 {
		return "0 B/s"
	}
	return size(int64(bytesPerSecond)) + "/s"
}

// size shows bytes the way people read them, about three significant
// digits: "812 KB", "4.59 MB", "12.3 GB".
func size(n int64) string {
	v, unit := float64(n), "B"
	for _, u := range []string{"KB", "MB", "GB", "TB"} {
		if v < 1024 {
			break
		}
		v, unit = v/1024, u
	}
	switch {
	case unit == "B":
		return fmt.Sprintf("%d B", n)
	case v < 10:
		return fmt.Sprintf("%.2f %s", v, unit)
	case v < 100:
		return fmt.Sprintf("%.1f %s", v, unit)
	}
	return fmt.Sprintf("%.0f %s", v, unit)
}

// humanDuration says how long, the way people do: "under a minute",
// "42 min", "1 h 5 min".
func humanDuration(ctx context.Context, d time.Duration) string {
	switch {
	case d < time.Minute:
		return i18n.T(ctx, "under a minute")
	case d < time.Hour:
		return i18n.Tf(ctx, "%d min", int(d.Minutes()))
	}
	h, m := int(d.Hours()), int(d.Minutes())%60
	if m == 0 {
		return i18n.Tf(ctx, "%d h", h)
	}
	return i18n.Tf(ctx, "%d h %d min", h, m)
}

// ago says how long ago: "just now", "3 min ago", "2 h ago".
func ago(ctx context.Context, d time.Duration) string {
	switch {
	case d < time.Minute:
		return i18n.T(ctx, "just now")
	case d < time.Hour:
		return i18n.Tf(ctx, "%d min ago", int(d.Minutes()))
	}
	return i18n.Tf(ctx, "%d h ago", int(d.Hours()))
}

// localDevice is a device's name (see deviceName) in ctx's language.
func localDevice(ctx context.Context, name string) string {
	if browser, system, ok := strings.Cut(name, " on "); ok {
		return i18n.Tf(ctx, "%s on %s", browser, system)
	}
	return i18n.T(ctx, name)
}

// formatDuration shows a duration as h:mm:ss or m:ss.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h, m, sec := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%d:%02d", m, sec)
}

// codecName names an RFC 6381 video codec string (hvc1.2.4…, avc1.64…).
func codecName(codec string) string {
	switch {
	case strings.HasPrefix(codec, "hvc1"), strings.HasPrefix(codec, "hev1"):
		return "HEVC"
	case strings.HasPrefix(codec, "avc"):
		return "H.264"
	case strings.HasPrefix(codec, "av01"):
		return "AV1"
	case strings.HasPrefix(codec, "vp09"):
		return "VP9"
	}
	return codec
}

func resolution(w, h int) string {
	if w == 0 || h == 0 {
		return ""
	}
	return fmt.Sprintf("%d×%d", w, h)
}

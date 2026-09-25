// Package stats computes what the dashboard shows: totals of the torrent
// list, short histories for sparklines, and this machine's memory, CPU and
// disks.
package stats

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"

	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

// History keeps the latest samples of a value, for a sparkline.
type History struct {
	values []float64
	size   int
}

// NewHistory keeps size samples.
func NewHistory(size int) *History {
	return &History{size: size}
}

// Add appends a sample, dropping the oldest beyond the size.
func (h *History) Add(v float64) {
	h.values = append(h.values, v)
	if len(h.values) > h.size {
		h.values = h.values[len(h.values)-h.size:]
	}
}

// Values returns the samples, oldest first.
func (h *History) Values() []float64 {
	return append([]float64(nil), h.values...)
}

// Max is the largest sample (0 when empty).
func (h *History) Max() float64 {
	var m float64
	for _, v := range h.values {
		m = max(m, v)
	}
	return m
}

// Totals sums the torrent list.
type Totals struct {
	Torrents, Active int
	Download, Upload float64 // bytes per second
	Seeders, Peers   int
	Loaded           int64 // bytes downloaded by active torrents
	// Downloaded and Uploaded are the data active torrents moved since
	// TorrServer connected them.
	Downloaded, Uploaded int64
}

// TotalsOf adds up the torrents; only those TorrServer has connected (not
// merely stored in its database) count as active.
func TotalsOf(list []torrserver.Torrent) Totals {
	t := Totals{Torrents: len(list)}
	for _, tr := range list {
		if tr.Stat < 1 || tr.Stat > 3 {
			continue
		}
		t.Active++
		t.Download += tr.Download
		t.Upload += tr.Upload
		t.Seeders += tr.Connected
		t.Peers += tr.ActivePeers
		t.Loaded += tr.LoadedSize
		t.Downloaded += tr.Downloaded
		t.Uploaded += tr.Uploaded
	}
	return t
}

// Process is the memory and CPU of one process.
type Process struct {
	RSS int64   // resident memory in bytes
	CPU float64 // percent of one core since the previous sample
}

// Memory is the machine's memory.
type Memory struct {
	Total, Used int64
}

// Disk is the space of the disk holding a folder.
type Disk struct {
	Total, Free int64
}

// CPU is the machine's processor use.
type CPU struct {
	Percent              float64 // all cores, since the previous sample
	Cores                int
	Load1, Load5, Load15 float64 // load averages; zero where the system has none
}

// Net is the machine's network traffic, loopback excluded.
type Net struct {
	Measured bool    // false on the first sample: nothing to compare with yet
	Down, Up float64 // bytes per second received and sent since the previous sample
}

// GoRuntime is Moviestracker's own Go runtime.
type GoRuntime struct {
	Version    string // "go1.27.1"
	Goroutines int
	HeapInUse  int64         // bytes in use by the heap
	GCs        uint32        // garbage collections since start
	LastPause  time.Duration // of the latest collection
}

// System is one sample of the machine.
type System struct {
	Go      GoRuntime
	CPU     CPU
	Net     Net
	Self    Process
	Engine  *Process // nil unless Watch names the engine's PID
	Memory  Memory
	Disks   map[string]Disk  // by folder
	Folders map[string]int64 // bytes used by each folder
}

// Watch says what to sample besides this process and the machine's memory.
type Watch struct {
	EnginePID int32    // the managed TorrServer; 0 for none
	Disks     []string // folders whose disk's space to report
	Folders   []string // folders whose size to measure (a walk: keep few)
}

// Sampler samples the machine; it remembers processes so CPU use is measured
// between samples. It is safe for concurrent use.
type Sampler struct {
	mu    sync.Mutex
	procs map[int32]*process.Process
	// The network counters of the previous sample.
	netAt            time.Time
	netRecv, netSent uint64
}

// NewSampler returns a sampler.
func NewSampler() *Sampler {
	return &Sampler{procs: map[int32]*process.Process{}}
}

// Sample reads the machine now. Figures it cannot read stay zero.
func (s *Sampler) Sample(ctx context.Context, w Watch) System {
	sys := System{Disks: map[string]Disk{}, Folders: map[string]int64{}}
	sys.Self = s.process(ctx, int32(os.Getpid())) // #nosec G115 -- PIDs fit in int32
	sys.Go = goRuntime()
	if w.EnginePID > 0 {
		engine := s.process(ctx, w.EnginePID)
		sys.Engine = &engine
	}
	if pct, err := cpu.PercentWithContext(ctx, 0, false); err == nil && len(pct) == 1 {
		sys.CPU.Percent = pct[0]
	}
	if n, err := cpu.CountsWithContext(ctx, true); err == nil {
		sys.CPU.Cores = n
	}
	if avg, err := load.AvgWithContext(ctx); err == nil {
		sys.CPU.Load1, sys.CPU.Load5, sys.CPU.Load15 = avg.Load1, avg.Load5, avg.Load15
	}
	sys.Net = s.network(ctx)
	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		sys.Memory = Memory{Total: int64(vm.Total), Used: int64(vm.Used)} // #nosec G115 -- memory sizes fit in int64
	}
	for _, dir := range w.Disks {
		if u, err := disk.UsageWithContext(ctx, dir); err == nil {
			sys.Disks[dir] = Disk{Total: int64(u.Total), Free: int64(u.Free)} // #nosec G115 -- disk sizes fit in int64
		}
	}
	for _, dir := range w.Folders {
		sys.Folders[dir] = folderSize(ctx, dir)
	}
	return sys
}

func (s *Sampler) process(ctx context.Context, pid int32) Process {
	s.mu.Lock()
	p := s.procs[pid]
	if p == nil {
		var err error
		if p, err = process.NewProcessWithContext(ctx, pid); err != nil {
			s.mu.Unlock()
			return Process{}
		}
		s.procs[pid] = p
	}
	s.mu.Unlock()
	var out Process
	if info, err := p.MemoryInfoWithContext(ctx); err == nil {
		out.RSS = int64(info.RSS) // #nosec G115 -- memory sizes fit in int64
	}
	if cpu, err := p.PercentWithContext(ctx, 0); err == nil {
		out.CPU = cpu
	}
	return out
}

// folderSize adds up the files under dir, stopping when ctx ends.
func folderSize(ctx context.Context, dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable parts are skipped
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// ErrNotListening is no process of ours listening on a port.
var ErrNotListening = errors.New("no process listens on that port")

// ListeningPID finds the process listening on a TCP port of this machine,
// such as a TorrServer the user runs themselves. It is slow (the system's
// socket table), so callers keep the answer for a while.
func ListeningPID(ctx context.Context, port int) (int32, error) {
	conns, err := gnet.ConnectionsWithContext(ctx, "tcp")
	if err != nil {
		return 0, err
	}
	for _, c := range conns {
		if c.Status == "LISTEN" && int(c.Laddr.Port) == port && c.Pid > 0 {
			return c.Pid, nil
		}
	}
	return 0, ErrNotListening
}

// network turns the interfaces' byte counters into speeds since the previous
// sample. Loopback is left out: it is this machine talking to itself (the
// stream proxy to TorrServer), not traffic on the network.
func (s *Sampler) network(ctx context.Context) Net {
	counters, err := gnet.IOCountersWithContext(ctx, true)
	if err != nil {
		return Net{}
	}
	var recv, sent uint64
	for _, c := range counters {
		if strings.HasPrefix(c.Name, "lo") {
			continue
		}
		recv += c.BytesRecv
		sent += c.BytesSent
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	prevAt, prevRecv, prevSent := s.netAt, s.netRecv, s.netSent
	s.netAt, s.netRecv, s.netSent = now, recv, sent
	secs := now.Sub(prevAt).Seconds()
	if prevAt.IsZero() || secs <= 0 || recv < prevRecv || sent < prevSent {
		return Net{} // first sample, or counters reset
	}
	return Net{Measured: true, Down: float64(recv-prevRecv) / secs, Up: float64(sent-prevSent) / secs}
}

// goRuntime reads the Go runtime. ReadMemStats stops the world briefly,
// which is fine at the dashboard's few-second interval.
func goRuntime() GoRuntime {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	g := GoRuntime{
		Version: runtime.Version(), Goroutines: runtime.NumGoroutine(),
		HeapInUse: int64(m.HeapInuse), GCs: m.NumGC, // #nosec G115 -- heap sizes fit in int64
	}
	if m.NumGC > 0 {
		g.LastPause = time.Duration(m.PauseNs[(m.NumGC+255)%256]) // #nosec G115 -- pauses fit in int64
	}
	return g
}

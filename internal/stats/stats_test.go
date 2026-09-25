package stats_test

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/stats"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

func TestAHistoryKeepsTheLatestSamplesInOrder(t *testing.T) {
	h := stats.NewHistory(3)
	if got := h.Values(); len(got) != 0 {
		t.Fatalf("empty history = %v", got)
	}
	for _, v := range []float64{1, 2, 3, 4, 5} {
		h.Add(v)
	}
	if got := h.Values(); !slices.Equal(got, []float64{3, 4, 5}) {
		t.Errorf("Values() = %v, want the last three oldest first", got)
	}
	if got := h.Max(); got != 5 {
		t.Errorf("Max() = %v, want 5", got)
	}
}

func TestTotalsAddUpTheTorrentList(t *testing.T) {
	list := []torrserver.Torrent{
		{Stat: 3, Download: 4 << 20, Upload: 1 << 20, Connected: 5, ActivePeers: 12, LoadedSize: 700 << 20, Downloaded: 800 << 20, Uploaded: 90 << 20},
		{Stat: 2, Download: 1 << 20, Connected: 1, ActivePeers: 3, LoadedSize: 50 << 20, Downloaded: 60 << 20, Uploaded: 10 << 20},
		{Stat: 5, LoadedSize: 900 << 20}, // dropped: in the database only
	}
	got := stats.TotalsOf(list)
	want := stats.Totals{Torrents: 3, Active: 2, Download: 5 << 20, Upload: 1 << 20, Seeders: 6, Peers: 15, Loaded: 750 << 20,
		Downloaded: 860 << 20, Uploaded: 100 << 20}
	if got != want {
		t.Errorf("TotalsOf() = %+v\nwant %+v", got, want)
	}
}

func TestTheSamplerReadsThisMachine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "piece"), make([]byte, 64<<10), 0o600); err != nil {
		t.Fatal(err)
	}
	sampler := stats.NewSampler()
	sys := sampler.Sample(context.Background(), stats.Watch{Disks: []string{dir}, Folders: []string{dir}})
	if sys.Self.RSS <= 0 {
		t.Errorf("own memory = %d, want > 0", sys.Self.RSS)
	}
	if sys.Memory.Total <= 0 || sys.Memory.Used <= 0 {
		t.Errorf("host memory = %+v", sys.Memory)
	}
	if d := sys.Disks[dir]; d.Total <= 0 || d.Free <= 0 {
		t.Errorf("disk of %s = %+v", dir, d)
	}
	if got := sys.Folders[dir]; got < 64<<10 {
		t.Errorf("folder size = %d, want at least the 64 KB file", got)
	}
	if sys.Engine != nil {
		t.Errorf("engine stats without an engine PID: %+v", sys.Engine)
	}

	withEngine := sampler.Sample(context.Background(), stats.Watch{EnginePID: int32(os.Getpid())}) // #nosec G115 -- PIDs fit in int32
	if withEngine.Engine == nil || withEngine.Engine.RSS <= 0 {
		t.Errorf("engine process stats = %+v, want the process's memory", withEngine.Engine)
	}
}

func TestTheSamplerReadsTheMachinesCPU(t *testing.T) {
	sampler := stats.NewSampler()
	sampler.Sample(context.Background(), stats.Watch{}) // CPU use is measured between samples
	time.Sleep(200 * time.Millisecond)
	cpu := sampler.Sample(context.Background(), stats.Watch{}).CPU
	if cpu.Cores < 1 || cpu.Percent < 0 || cpu.Percent > 100 {
		t.Errorf("machine CPU = %+v, want cores ≥ 1 and use within 0–100%%", cpu)
	}
	if cpu.Load1 < 0 {
		t.Errorf("load average = %v", cpu.Load1)
	}
}

func TestTheProcessListeningOnAPortIsFound(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port
	pid, err := stats.ListeningPID(context.Background(), port)
	if err != nil || pid != int32(os.Getpid()) { // #nosec G115 -- PIDs fit in int32
		t.Errorf("ListeningPID(%d) = %d, %v; want this test's PID %d", port, pid, err, os.Getpid())
	}
	if pid, err := stats.ListeningPID(context.Background(), 1); err == nil {
		t.Errorf("ListeningPID(1) = %d, want an error: nothing of ours listens there", pid)
	}
}

func TestTheSamplerMeasuresNetworkSpeedBetweenSamples(t *testing.T) {
	sampler := stats.NewSampler()
	if first := sampler.Sample(context.Background(), stats.Watch{}).Net; first.Measured {
		t.Errorf("the first sample has nothing to compare with, yet measured %+v", first)
	}
	// Some traffic on this machine between the samples.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer func() { _ = ln.Close() }()
	go func() {
		if c, err := ln.Accept(); err == nil {
			_, _ = io.Copy(io.Discard, c)
		}
	}()
	if c, err := net.Dial("tcp", ln.Addr().String()); err == nil {
		_, _ = c.Write(make([]byte, 1<<20))
		_ = c.Close()
	}
	time.Sleep(200 * time.Millisecond)
	second := sampler.Sample(context.Background(), stats.Watch{}).Net
	if !second.Measured || second.Down < 0 || second.Up < 0 {
		t.Errorf("network = %+v, want a measured, non-negative speed", second)
	}
}

func TestTheSamplerReadsTheGoRuntime(t *testing.T) {
	sys := stats.NewSampler().Sample(context.Background(), stats.Watch{})
	if g := sys.Go; !strings.HasPrefix(g.Version, "go") || g.Goroutines <= 0 || g.HeapInUse <= 0 {
		t.Errorf("Go runtime = %+v", g)
	}
}

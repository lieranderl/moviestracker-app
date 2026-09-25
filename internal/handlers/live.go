package handlers

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/live"
	"github.com/lieranderl/moviestracker-app/internal/sources"
	"github.com/lieranderl/moviestracker-app/internal/stats"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"

	"github.com/a-h/templ"
	"github.com/starfederation/datastar-go/datastar"
)

// Live topics. A player topic is "player:<hash>", or "player:<hash>:gst"
// when the player plays GStreamer HLS and its pipeline must be kept alive.
const (
	topicEngine   = "engine"
	topicTorrents = "torrents"
	topicPlays    = "plays"   // what plays through the proxy
	topicSystem   = "system"  // this machine and the engine's storage
	topicSources  = "sources" // how TMDB, JacRed and IMDb last answered
	playerPrefix  = "player:"
)

// sparkSamples is how many list polls a sparkline shows (2 minutes at 2s).
const sparkSamples = 60

// torrentsState is the torrent list with its recent total speeds.
type torrentsState struct {
	List     []torrserver.Torrent
	Down, Up []float64 // total download/upload speed per poll, oldest first
}

// playerState is what the player's stats row shows.
type playerState struct {
	Stats     *torrserver.Torrent
	CacheSize int64
}

// playerTopic names the live topic of a torrent being played.
func playerTopic(hash string, gst bool) string {
	topic := playerPrefix + hash
	if gst {
		topic += ":gst"
	}
	return topic
}

// liveTopic resolves live topics to their pollers. Each reads the TorrServer
// in use at the time of the poll, so switching engines needs no restart.
func (s *Server) liveTopic(every LivePolling) live.Source {
	return func(topic string) (live.Poller, bool) {
		switch topic {
		case topicEngine:
			return live.Poller{Interval: every.Engine, Fetch: func(ctx context.Context) (any, error) {
				return s.torrServer.Client().Echo(ctx)
			}}, true
		case topicTorrents:
			down, up := stats.NewHistory(sparkSamples), stats.NewHistory(sparkSamples)
			return live.Poller{Interval: every.Torrents, Fetch: func(ctx context.Context) (any, error) {
				list, err := s.torrServer.Client().ListTorrents(ctx)
				if err != nil {
					return nil, err
				}
				totals := stats.TotalsOf(list)
				down.Add(totals.Download)
				up.Add(totals.Upload)
				return torrentsState{List: list, Down: down.Values(), Up: up.Values()}, nil
			}}, true
		case topicPlays:
			return live.Poller{Interval: every.Player, Fetch: func(context.Context) (any, error) {
				return s.plays.Active(), nil
			}}, true
		case topicSystem:
			return live.Poller{Interval: every.Engine, Fetch: s.systemFetch()}, true
		case topicGStreamer:
			return live.Poller{Interval: every.Player, Fetch: func(context.Context) (any, error) {
				return s.gstStatus(), nil
			}}, true
		case topicSources:
			return live.Poller{Interval: every.Torrents, Fetch: func(context.Context) (any, error) {
				if s.connector.Health == nil {
					return map[string]sources.ServiceHealth{}, nil
				}
				return s.connector.Health.Snapshot(), nil
			}}, true
		}
		rest, ok := strings.CutPrefix(topic, playerPrefix)
		if !ok {
			return live.Poller{}, false
		}
		hash, gst := strings.CutSuffix(rest, ":gst")
		if !isHexHash(hash) {
			return live.Poller{}, false
		}
		return live.Poller{Interval: every.Player, Fetch: func(ctx context.Context) (any, error) {
			client := s.torrServer.Client()
			if gst {
				_ = client.Heartbeat(ctx, hash) // keeps TorrServer's pipeline for this stream alive
			}
			stats, err := client.TorrentStats(ctx, hash)
			if err != nil {
				return nil, err
			}
			cacheSize, _ := client.CacheSize(ctx)
			return playerState{Stats: stats, CacheSize: cacheSize}, nil
		}}, true
	}
}

// liveValue returns a topic's value as T, or T's zero value when there is
// none yet or the last poll failed.
func liveValue[T any](sub *live.Subscription, topic string) T {
	v, _ := sub.Snapshot(topic).Value.(T)
	return v
}

// probeRetry is how long the player stream waits before asking TorrServer
// again for media info it could not give yet (the file is still buffering).
const probeRetry = 5 * time.Second

// streamPlayerStats keeps the player's stats row, and with gst=1 its media
// info, current until the player closes.
func (s *Server) streamPlayerStats(w http.ResponseWriter, r *http.Request, hash string) {
	if !isHexHash(hash) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if !s.acquireSSE() {
		w.Header().Set("Retry-After", "2")
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}
	defer s.releaseSSE()
	q := r.URL.Query()
	gst := q.Get("gst") == "1"
	index, audio, ok := parseHLSTrack(q)
	if !ok {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	sse := datastar.NewSSE(w, r)
	ctx, cancel := s.streamContext(r)
	defer cancel()
	topic := playerTopic(hash, gst)
	sub := s.live.Subscribe(topic)
	defer sub.Close()

	var (
		lastStats, lastInfo string
		probe               *torrserver.ProbeResult
		probedAt            time.Time
	)
	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.C:
		}
		state := liveValue[playerState](sub, topic)
		if err := patchIfChanged(ctx, sse, views.TorrPlayerStats(state.Stats, state.CacheSize), &lastStats); err != nil {
			logSSEError(r, "patch player stats", err)
			return
		}
		if !gst {
			continue
		}
		if probe == nil && time.Since(probedAt) > probeRetry {
			probedAt = time.Now()
			probeCtx, probeCancel := context.WithTimeout(ctx, 10*time.Second)
			probe, _ = s.torrServer.Client().Probe(probeCtx, hash, index)
			probeCancel()
		}
		var output *torrserver.HLSOutput
		if out, ok := s.hlsOutputs.load(hlsOutputKey{hash, index, audio}); ok {
			output = &out
		}
		if err := patchIfChanged(ctx, sse, views.TorrMediaInfo(probe, audio, output), &lastInfo); err != nil {
			logSSEError(r, "patch media info", err)
			return
		}
	}
}

// patchIfChanged renders c and patches it only when the HTML differs from
// *last, so an unchanged view is not morphed again.
func patchIfChanged(ctx context.Context, sse *datastar.ServerSentEventGenerator, c templ.Component, last *string) error {
	var buf strings.Builder
	if err := c.Render(ctx, &buf); err != nil {
		return err
	}
	if html := buf.String(); html != *last {
		*last = html
		patchesSent.Add(1)
		return sse.PatchElements(html)
	}
	patchesSkipped.Add(1)
	return nil
}

// patchesSent and patchesSkipped count live patches since start: sent, or
// not sent because nothing changed. The dashboard shows them.
var patchesSent, patchesSkipped atomic.Int64

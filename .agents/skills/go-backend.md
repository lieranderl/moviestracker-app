# Skill: Go Backend, Templ & 3-Tier Architecture for Datastar

## Purpose & Scope
Architectural principles, static analysis standards, and implementation patterns for the Go runtime, `github.com/starfederation/datastar-go/datastar`, `github.com/a-h/templ`, and enterprise 3-Tier web architecture.

---

## 1. Native Go HTTP Routing & Datastar SDK

Modern Go (Go 1.22+) provides method routing directly in standard library `http.ServeMux`:

```go
mux := http.NewServeMux()
mux.HandleFunc("GET /", handleHome)
mux.HandleFunc("POST /api/login", handleLogin)
mux.HandleFunc("GET /api/live-stats", handleLiveStats)
```

### Datastar Go SDK (`github.com/starfederation/datastar-go/datastar`)

Initialize the Server-Sent Event generator:
```go
func handleStream(w http.ResponseWriter, r *http.Request) {
    sse := datastar.NewSSE(w, r)

    // Stream a templ component directly into the SSE stream:
    _ = sse.PatchElementTempl(views.MyComponent("hello"))

    // Update frontend reactive signals:
    _ = sse.MarshalAndPatchSignals(map[string]any{
        "count": 42,
    })

    // Browser navigation / redirect via SSE:
    _ = sse.Redirect("/dashboard")
}
```

### Reading Incoming Signals
```go
type MySignals struct {
    Query string `json:"query"`
    Count int    `json:"count"`
}

var signals MySignals
if err := datastar.ReadSignals(r, &signals); err != nil {
    // handle error
}
```

---

## 2. High-Performance Concurrency, Safety & Memory Patterns

### A. Lock-Free Atomic Caching for Read Paths
For metrics, telemetry, or hot read paths, avoid mutex contention by loading via `sync/atomic.Pointer[T]`:
```go
func getMetrics() *systemStats {
    if s := statsAtomic.Load(); s != nil && time.Since(s.CachedAt) < time.Second {
        return s // Lock-free hot path
    }

    metricsMu.Lock()
    defer metricsMu.Unlock()

    if s := statsAtomic.Load(); s != nil && time.Since(s.CachedAt) < time.Second {
        return s
    }

    fresh := computeMetrics()
    statsAtomic.Store(fresh)
    return fresh
}
```

### B. Persistent SSE Streaming Lifecycle
Stream updates over a single connection rather than client-polling:
```go
func (s *Server) handleLiveStats(w http.ResponseWriter, r *http.Request) {
    sse := datastar.NewSSE(w, r)
    // Send immediate initial state
    _ = sse.PatchElementTempl(views.LiveStatsFragment(...))

    if r.URL.Query().Get("stream") != "true" {
        return
    }

    ticker := time.NewTicker(2 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-r.Context().Done():
            return // Client closed tab/aborted fetch -> exit cleanly, zero goroutine leak
        case <-ticker.C:
            _ = sse.PatchElementTempl(views.LiveStatsFragment(...))
        }
    }
}
```

### C. Zero Forced Stop-The-World (STW) Pauses
- **Never invoke `runtime.GC()`** inside request or telemetry paths. In Go, `runtime.GC()` halts execution across all goroutines.
- Allow Go's concurrent collector to run on schedule based on `GOGC=100`.

### D. Bounded Readers & Memory Protection
Always guard against unbounded memory consumption when reading HTTP bodies:
```go
bodyBytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20)) // 1 MB limit
```
Use `bytes.NewReader(bodyBytes)` instead of `bytes.NewBuffer(bodyBytes)` for zero-allocation readers.

### E. Security & Constant-Time Verification
Protect authentication endpoints against timing side-channel attacks:
```go
if subtle.ConstantTimeCompare([]byte(password), []byte(storedHash)) != 1 {
    return ErrInvalidCredentials
}
```

### F. Panic Recovery & Server Timeouts
Wrap the server handler stack with panic recovery:
```go
func RecoveryMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        defer func() {
            if rec := recover(); rec != nil {
                log.Printf("[PANIC] %s %s: %v\n%s", r.Method, r.URL.Path, rec, debug.Stack())
                http.Error(w, "Internal Server Error", http.StatusInternalServerError)
            }
        }()
        next.ServeHTTP(w, r)
    })
}
```
Configure standard server timeouts:
```go
httpServer := &http.Server{
    Addr:              ":" + port,
    Handler:           handler,
    ReadHeaderTimeout: 5 * time.Second,
    IdleTimeout:       120 * time.Second,
    MaxHeaderBytes:    1 << 20,
}
```

### G. Modern Go 1.24+ Benchmarking (`for b.Loop()`)
Always modernize benchmarks using `for b.Loop()`:
```go
func BenchmarkEndpoint(b *testing.B) {
    server := NewServer()
    req := httptest.NewRequest(http.MethodGet, "/api/resource", nil)
    rec := httptest.NewRecorder()
    b.ReportAllocs()

    for b.Loop() {
        server.ServeHTTP(rec, req)
    }
}
```
`b.Loop()` automatically resets benchmark timer on iteration 0, properly handles benchmark iterations, and eliminates manual `b.ResetTimer()`.

### H. Static Analysis, Linting & Error Invariants
All agents must enforce:
1. **`make lint`**: Always run `go vet ./...` and `staticcheck ./...` before finishing tasks.
2. **Zero Diagnostics Policy**: Unused imports, unused variables, formatting deviations (`gofmt`), and linter warnings must be resolved immediately.
3. **Explicit Error Handling**: Check every error explicitly. If intentionally ignored (e.g. SSE close on disconnected socket), prefix with `_ =`.

---

## 3. Session Management & Cookies

Always use `HttpOnly`, `SameSite=Lax`, and `Path=/` with 24-byte random hex tokens generated via `crypto/rand`:

```go
http.SetCookie(w, &http.Cookie{
    Name:     "datastar_session",
    Value:    token,
    Path:     "/",
    MaxAge:   7 * 24 * 60 * 60,
    HttpOnly: true,
    SameSite: http.SameSiteLaxMode,
    Secure:   false, // Set to true in production over TLS
})
```

Implement bounded session eviction (background `time.Ticker` prune) to prevent unbounded memory growth.

---

## 4. Clean 3-Tier Architecture Invariants

- **Tier 1: Presentation (`internal/handlers/`, `internal/views/`)**:
  - HTTP handlers, SSE generators, session cookies, Templ rendering.
  - No database queries directly in handlers.
- **Tier 2: Application / Domain (`internal/auth/`, `internal/domain/`)**:
  - Business rules, domain models, service interfaces, validation.
  - Independent of HTTP or SSE.
- **Tier 3: Persistence (`internal/repository/`)**:
  - Database queries (PostgreSQL, SQLite via `sqlc` or `pgx`), connection pooling, transaction management.

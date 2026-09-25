# Go Engineering & Idioms Guide

This document defines coding conventions, safety standards, and performance patterns for Go development in Moviestracker.

## 1. Concurrency & Context Lifecycle
- **Context Propagation**:
  - Always pass `context.Context` through API boundaries as the first argument.
  - In HTTP handlers, use `r.Context()`. Never construct detached contexts (`context.Background()`) for operations bound to request lifecycle.
- **SSE Stream Goroutine Safety**:
  - Every streaming ticker loop must monitor `r.Context().Done()`:
    ```go
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-r.Context().Done():
            return
        case <-ticker.C:
            if err := sendPatch(); err != nil {
                return
            }
        }
    }
    ```
  - Always clean up tickers and release acquired stream slots in a `defer` statement.

## 2. Defensive Security & Cryptography
- **Constant-Time Verification**:
  - Prevent side-channel timing attacks on credentials by using `crypto/subtle.ConstantTimeCompare`:
    ```go
    if subtle.ConstantTimeCompare([]byte(givenPass), []byte(expectedPass)) != 1 {
        return nil, ErrInvalidCredentials
    }
    ```
- **Cryptographic Randomness**:
  - Use `crypto/rand` for session tokens and security identifiers. Never use `math/rand`.
- **Panic Recovery**:
  - Top-level `RecoveryMiddleware` intercepts unhandled panics, logs stack traces via `runtime/debug.Stack()`, and sends HTTP 500 without process termination.
- **Content-Security-Policy (CSP)**:
  - Strict security headers (`X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `Permissions-Policy`).
  - CSP allows `'self'` and `'unsafe-eval'` for Datastar's lightweight client expression evaluator.

## 3. Memory & Allocation Invariants
- **Bounded Request Consumption**:
  - Wrap request bodies with `http.MaxBytesReader` to eliminate denial-of-service memory exhaustion.
- **Zero Allocations on Static Reads**:
  - Use `bytes.NewReader(data)` over `bytes.NewBuffer(data)` when reading pre-existing byte slices.
- **Lock-Free Reads**:
  - Store hot read caches in `sync/atomic.Pointer[T]`. Writers update atomically via `Store()`; readers read lock-free via `Load()`.

## 4. Modern Benchmarks (Go 1.24+)
- **Use `for b.Loop()`**:
  - All benchmarks must use the modern loop syntax:
    ```go
    func BenchmarkTelemetryCache(b *testing.B) {
        cache := NewTelemetryCache()
        for b.Loop() {
            _ = cache.GetStats()
        }
    }
    ```
  - `b.Loop()` automatically resets timers on iteration 0, eliminates manual `b.ResetTimer()`, and guarantees clean iteration scaling.

## 5. Diagnostic & Static Analysis Standards
- Run `make lint` on every change (`go vet`, `staticcheck`, `gofmt`).
- Never ignore compiler warnings, unhandled errors, or staticcheck findings.
- Use explicit error wrapping (`fmt.Errorf("...: %w", err)`).

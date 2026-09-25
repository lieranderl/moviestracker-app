# Testing & Test-Driven Development (TDD) Guide

This document defines testing conventions, the test-driven development loop, seam selection, and memory leak verification in Moviestracker.

## 1. The Red-Green-Refactor Loop
- **Red**: Write one failing test first at an agreed public seam. Verify that it fails for the expected reason.
- **Green**: Write only enough production code to make that single test pass. Resist anticipating subsequent requirements.
- **Vertical Slicing**: Implement one vertical slice at a time (one seam, one test, minimal code, repeat). Avoid horizontal slicing (writing all tests upfront, then all code).
- **Refactoring**: Keep implementation and structural refactoring distinct. Refactor once tests are green to improve clarity without altering observed behavior.

## 2. Pre-Agreed Test Seams
- A **seam** is the public boundary where behavior is observed without inspecting internals.
- Stop and confirm candidate seams before writing test code:
  - **HTTP Seam (`httptest.NewServer` / `httptest.ResponseRecorder`)**: Tests full routing, middleware, headers, error statuses (e.g. 429 Retry-After), and SSE streaming end-to-end.
  - **Domain Service Seams (`auth.SessionManager`, `tmdb.Client`)**: Tests thread-safe state management, capacity bounding, coalescing, and expiration through exported methods (`CreateSession`, `GetUser`, `GetCatalog`).
- **Public Seam Rule**: Tests MUST assert observable public behavior and MUST NOT inspect private maps, unexported struct fields, or internal mutexes. If internal restructuring breaks the test, the test was placed at the wrong seam.

## 3. Mock Boundaries & Clock Seam
- **External Boundaries Only**: Mocks and fakes are reserved strictly for external system boundaries:
  - **System Wall Clock**: Injectable via `WithClock(func() time.Time)` options in packages that manage TTLs (`internal/auth`, `internal/tmdb`). Use this seam for deterministic expiration tests without `time.Sleep` or private state tampering.
  - **Cryptographic Entropy**: Injectable `io.Reader` in `auth.NewSessionManager` for verifying entropy failure handling.
  - **External HTTP APIs**: Providers like `tmdb.CatalogProvider` for simulating upstream media catalog responses and timeouts.
- **Never Mock Internal Domain Modules**: Always use real in-memory implementations for domain objects (`auth.NewSessionManager()`).

## 4. Capability-Driven Assertions
- **Name Capabilities, Not Internals**:
  - Preferred: `TestLogin_ValidCredentials_CreatesSessionAndSetsCookie`
  - Avoid: `TestLogin_CallsAuthenticateAndStore`
- **Literal Assertions**:
  - Assert on explicit expected literals derived directly from specifications, never re-computing values using the same logic as the production code.

## 5. Verification Commands & Protocols
- **Run Unit & Integration Tests**:
  ```bash
  go test -v ./...
  ```
- **Run Concurrency Race Detector**:
  ```bash
  go test -race ./...
  ```
- **Run Full Quality Gate**:
  ```bash
  make ci
  ```
- **Memory Leak & Goroutine Verification**:
  - Concurrency and SSE tests must assert that goroutines terminate cleanly after context cancellation and that active stream counters decrement to zero.

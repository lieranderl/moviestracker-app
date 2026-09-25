# AGENTS.md

A fullstack reactive web application built with Go (1.27), Templ, Datastar (v1.0), and DaisyUI (v5), streaming server-driven state over Server-Sent Events with no client application framework.

## Core UI & Frontend Principles (Strict Priority)

### 1. Datastar First (Zero-JS Preference)
- **Datastar is PRIORITY**: Always use Datastar declarative signals and attributes (`data-signals`, `data-on:*`, `data-on-interval`, `data-class`, `data-attr:*`, `data-bind`, `data-text`, etc.) or SSE server patches.
- **Custom JS is Strictly a Last Resort**: Write custom JavaScript ONLY if a capability fundamentally cannot be achieved with Datastar (e.g. hls.js playback in `frontend/player.js`, clipboard access, or YouTube `postMessage` player control).
- **Forbidden**: Never write imperative vanilla JS (`document.querySelector`, manual `classList.add`/`remove`, `setInterval`, `setTimeout`, custom event listeners) for UI state, transitions, or timers that Datastar can handle declaratively.

### 2. DaisyUI First (Semantic Token Preference)
- **DaisyUI is PRIORITY**: Always use DaisyUI components (`carousel`, `hero`, `card`, `badge`, `btn`, `alert`, `fieldset`, `modal`, `navbar`, `input`, etc.). Do NOT reinvent the wheel if DaisyUI provides a component.
- **Tailwind Only When Needed**: Use utility classes (flex/grid, spacing, z-index, sizing) only to arrange DaisyUI components.
- **Semantic Theme Tokens**: Always use semantic DaisyUI theme tokens (`primary`, `secondary`, `accent`, `neutral`, `base-100`, `base-200`, `base-300`, `base-content`).
- **Centralized Theme Exemption**: Literal hex colors are restricted strictly to the centralized theme definition in `frontend/app.css`.
- **Authored Utilities Exemption**: Small authored utilities are allowed strictly for behavior not cleanly expressible via DaisyUI/Tailwind (e.g. video crossfade transitions).
- **Icons**: Render Lucide icons server-side with `@Icon("name", "size-4 …")` (`internal/views/icon.templ`). `bun run icons` regenerates `icons_gen.go` from icon names used in templates. Never use client-side `data-lucide` placeholders.
- **Modals**: Use native `<dialog>` via the `modalDialog` helper (signal-driven `showModal()`/`close()`), never `div.modal` + `modal-open`.
- **No Inline Styles**: Never use inline `style="..."` attributes; they violate the strict CSP `style-src 'self'`.

---

## The Work Loop
Every task follows the four-step loop:
1. **Plan**: Align on the approach and agreed test seams before writing code.
2. **Execute**: Implement via vertical slices following TDD.
3. **Test**: Run unit tests, race detector, and linters.
4. **Commit**: Create focused, verified commits.

## Plan Mode
- Make the plan extremely concise. Sacrifice grammar for the sake of concision.
- At the end of each plan, give me a list of unresolved questions to answer, if any.

## Test-Driven Development (TDD)
- **Red-Green loop**: Write one failing test at a public seam, write only enough code to pass it, then move to the next behavior.
- **Vertical slices**: One seam, one test, minimal implementation, repeat. Never write a bulk batch of tests before implementation.
- **Pre-agreed seams**: Name the candidate public boundaries to test against and confirm them before creating test files.
- **Public Seam Rule**: Tests assert observable public behavior only. Never reach into private unexported fields, maps, or mutexes.
- **Mock boundaries**: Mocks/fakes are exclusively for external system boundaries (external APIs, entropy sources). Wall clock is injected via `WithClock` options in `internal/auth` and `internal/tmdb`. Never mock internal domain packages.
- **Capabilities over internals**: Test names reflect capabilities ("user can sign in with valid credentials"), not internal function names. Assert on literal values traceable to specs.
- For in-depth rules or test-first workflows, invoke the `/tdd` skill ([.agents/skills/tdd/SKILL.md](.agents/skills/tdd/SKILL.md)).

## Essential Commands
- `make dev`: Run dev server with Air hot-reloading (auto-generates Templ and recompiles Go)
- `make test`: Run unit, integration, and memory leak tests (`go test -race ./...`)
- `make lint`: Run static analysis (`go vet`, `staticcheck`, `golangci-lint`, `gofmt`)
- `make templ`: Compile `.templ` templates into Go
- `bun run assets`: Compile Tailwind CSS v4 and bundle frontend scripts
- `make build`: Compile static production binary to `bin/server`
- `make ci`: Run full verification pipeline (format, lint, templ, assets, tests, security, build)
- `make docker-build`: Build non-root production container
- `make docker-smoke`: Run read-only container smoke test asserting `/healthz` returns `ok`

## Progressive Disclosure
For detailed domain specifications and conventions, consult the targeted guide:
- Architecture & Lifecycles: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
- Go Conventions & Safety: [docs/GO.md](docs/GO.md)
- Datastar & Hypermedia Patterns: [docs/DATASTAR.md](docs/DATASTAR.md)
- Testing & Benchmarking: [docs/TESTING.md](docs/TESTING.md)
- Settings, environment variables & container: [docs/CONFIGURATION.md](docs/CONFIGURATION.md)
- Branches, CI, releases & secrets: [docs/MAINTAINING.md](docs/MAINTAINING.md)
- Specialized Agent Skills: [.agents/skills/](.agents/skills/) (DaisyUI, Datastar, Go performance, Concurrency, TDD)

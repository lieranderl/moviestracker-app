# Contributing to Moviestracker

Thanks for helping! Bug reports, ideas and pull requests are all welcome.
Please be kind: see the [code of conduct](CODE_OF_CONDUCT.md).

- **Bugs and ideas:** [open an issue](https://github.com/lieranderl/moviestracker-app/issues/new/choose).
- **Security problems:** report them privately, see [SECURITY.md](SECURITY.md).
- **Bigger changes:** open an issue first, so we agree on the approach before
  you spend time on it.

## Development setup

You need macOS or Linux (the Windows installer is built on Windows, with Git
Bash and Inno Setup: `make winapp`; CI builds and tests it on every pull
request) with:

- Go (the version in `go.mod`), and Bun (the version in `package.json`)
- Air for hot reload (`go install github.com/air-verse/air@latest`), optional
- Docker or Apple's `container` for the container checks, optional
- Xcode Command Line Tools to build the Mac app, on macOS

```bash
git clone https://github.com/lieranderl/moviestracker-app.git
cd moviestracker-app
bun install --frozen-lockfile
make torrserver   # the pinned TorrServer, checked against its SHA-256
make dev          # http://localhost:8095, data in .devdata/, hot reload
```

The first visit asks you to create the administrator account. Search works
once you add a free [TMDB key](https://www.themoviedb.org/settings/api) in
Settings → Sources, or put `TMDB_API_KEY=…` in a `.env` file (git-ignored).

## How we work

Every change follows the same loop: **plan, test first, implement, verify.**

1. **Tests first.** Write one failing test at a public seam, then just enough
   code to pass it; repeat. Tests describe what users can do
   (`TestAViewerCannotRemoveATorrent`), not internal function names.
   Mocks are only for external systems (TMDB, JacRed, TorrServer, the clock).
   See [docs/TESTING.md](docs/TESTING.md).
2. **Datastar and DaisyUI first.** UI state lives in Datastar attributes and
   server-sent patches; custom JavaScript is a last resort. Use DaisyUI
   components and theme tokens; no inline styles (the CSP forbids them).
   See [AGENTS.md](AGENTS.md) and [docs/DATASTAR.md](docs/DATASTAR.md).
3. **Go conventions** are in [docs/GO.md](docs/GO.md); the architecture is in
   [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).
4. **Run the full gate** before pushing:

   ```bash
   make ci   # generated files, lint, race tests, govulncheck, gosec, build
   ```

Generated files (`*_templ.go`, `static/app.css`, `static/*.js`,
`internal/views/icons_gen.go`) are committed; `make ci` fails if they are out
of date, so run `make templ` and `bun run assets` after changing templates or
styles.

Using an AI coding agent? `AGENTS.md` (also `CLAUDE.md` and `GEMINI.md`) and
`.agents/skills/` hold the project's rules for it. You are responsible for
every line you submit, as if you wrote it yourself.

## Branches and pull requests

`main` is always releasable and protected: changes reach it only through pull
requests that pass CI.

1. Fork the repository (or branch from `main` if you are a maintainer), and
   name the branch after the change: `fix/fetch-files-removes-torrent`,
   `feat/torrent-title-links`.
2. Keep a pull request to one change. Rebase on `main` rather than merging it
   in.
3. Title the pull request as a [Conventional Commit](https://www.conventionalcommits.org):
   `feat(dashboard): close the GStreamer message`, `fix: …`, `docs: …`.
   Pull requests are squash-merged, so the title becomes the commit on `main`
   and the line in the release notes. Add `!` for a breaking change
   (`feat!: …`).
4. Fill in the template: what changes for users, and how you tested it
   (with screenshots, light and dark, for visible changes).
5. CI must pass (one required check, **CI passed**, sums up every job), the
   title check and dependency review too, and conversations must be resolved.
   A maintainer reviews pull requests from contributors before merging.
   Workflows from first-time contributors run once a maintainer approves them.

Never commit keys, passwords, IP addresses or personal data, including in
tests and screenshots. CI scans the whole history for secrets.

Maintainers: branches, CI, secrets and releases are described in
[docs/MAINTAINING.md](docs/MAINTAINING.md).

## Licensing of contributions

Moviestracker is licensed under the [GNU Affero General Public License
v3.0](LICENSE) (AGPL-3.0-only). By submitting a contribution you agree that it
is licensed under the same license ("inbound = outbound", as in section D.6 of
GitHub's Terms of Service), and that you have the right to submit it. There is
no contributor license agreement.

New dependencies must be compatible with the AGPL-3.0: MIT, BSD, ISC, Apache-2.0,
MPL-2.0, LGPL, GPL-3.0 and AGPL-3.0 are fine; GPL-2.0-only, SSPL, BUSL and
"non-commercial" licenses are not.

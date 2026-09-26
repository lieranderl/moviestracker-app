# Maintaining Moviestracker

How the repository, CI and releases are set up, for maintainers.

## Branches

Trunk-based: `main` is always releasable, and everything reaches it through a
short-lived branch and a pull request, maintainers included:

```bash
git switch -c fix/short-name origin/main
# … commits, make ci …
git push -u origin HEAD
gh pr create --fill --title "fix(scope): what changes for users"
gh pr merge --auto --squash      # merges itself once the checks pass
```

`scripts/github-setup.sh` applies the settings below; run it again after
changing it (it updates what exists).

- **Ruleset `main`** (nobody bypasses it, admins included):
  - changes only through pull requests, squash-merged, so each pull request
    is one commit on `main`, signed by GitHub, titled as a Conventional
    Commit; conversations must be resolved;
  - required checks, on a branch up to date with `main`: **CI passed**
    (`ci.yml`'s last job, green only when every CI job is), **Title follows
    Conventional Commits** and **Dependency review** (`pr.yml`), and no new
    CodeQL errors or high-severity alerts;
  - no deletion, force-push or merge commits (linear history). Commits on
    branches need no signature: the squash merge makes a new commit that
    GitHub signs.
  - No approval is required while there is one maintainer: with more, raise
    `required_approving_review_count` to 1 and turn on code owner review.
  - In an emergency an admin can switch the ruleset off in Settings → Rules,
    push, and switch it on again; prefer a quick pull request.
- **Merging:** squash only, the pull request title and description become the
  commit; branches are deleted after merge; auto-merge and "Update branch" are
  on.
- **Labels:** `pr.yml` labels pull requests from their title (`feat` →
  `feature`, `fix` → `fix`, `!` → `breaking-change`), which groups the release
  notes (`.github/release.yml`). Add `skip-changelog` to leave one out.
- **Dependabot** opens grouped updates weekly, for versions at least seven
  days old; minor and patch updates merge themselves once CI passes, major
  ones wait for a person.
- **Jobs and required checks:** add or rename CI jobs freely, and list new
  ones in the `needs` of **CI passed**. Only a new workflow's checks need
  adding to the ruleset.

## Workflows

| Workflow | When | What |
| --- | --- | --- |
| `ci.yml` | pull requests, pushes to `main`, called by releases | `make ci` (generated files, lint, race tests, govulncheck, gosec, build) and the TorrServer API contract; gitleaks over the whole history; macOS tests and a real DMG build; Windows tests, a real installer build, and its install, upgrade, quit, uninstall and purge; the Docker image end to end with `compose.yaml` (`scripts/ci/docker-e2e.sh`: TorrServer and GStreamer, setup, persistence, shutdown, licences) on amd64 and on native arm64 runners; zizmor over the workflows; **CI passed** when all of them are green |
| `pr.yml` | pull requests | Conventional Commit titles; dependency review (vulnerable or AGPL-incompatible dependencies); labels from the title; auto-merge for Dependabot's minor and patch updates |
| `codeql.yml` | pull requests, `main`, weekly | CodeQL for Go, JavaScript and the workflows |
| `release.yml` | `v*` tags | Checks the tag is on `main`, runs CI, then in parallel the Docker image for linux/amd64 and linux/arm64 (pushed to `ghcr.io/lieranderl/moviestracker` as `<version>`, `MAJOR.MINOR` and `latest`, or only `<version>` for a pre-release, with SBOM and signed provenance), the DMG and the Windows installer; checksums, signed build provenance, and a **draft** release |

Every action is pinned to a commit SHA with its version in a comment;
Dependabot updates the pins, Go modules, Bun tools and the Docker base images
weekly. Workflows get a read-only token unless a job needs more, and zizmor
(`make lint` runs it too, with [uv](https://docs.astral.sh/uv/)) checks them
for template injection, over-broad permissions and cache poisoning. Release
builds use no caches.

## Secrets

| Secret | Used by | What |
| --- | --- | --- |
| `MT_SHARED_TMDB_KEY` | `release.yml` | The read-only TMDB token built into releases (the Docker image gets it as a build secret, so the image history does not show it). Without it, releases work but users must add their own key. |

It lives in the `release` environment, which only `v*` tags can use, so
pull requests and branches never see it. Set it with
`scripts/github-setup.sh` (from the git-ignored `.tmdb-shared-key`) or
`gh secret set MT_SHARED_TMDB_KEY --env release`. To rotate it,
create a new read-only token for the Moviestracker TMDB account, update the
secret, release, then revoke the old token.

## Releasing

Versions follow [Semantic Versioning](https://semver.org): `vMAJOR.MINOR.PATCH`,
and `v1.2.0-rc.1` for pre-releases. The squash-merged Conventional Commit
titles tell you which part to bump: `feat` → minor, `fix` → patch, `!` →
major (minor while the version is `0.x`).

1. Make sure `main` is green and has what you want to ship.
2. Tag the commit on `main` and push the tag (only admins can create `v*`
   tags, and nobody can move or delete one):

   ```bash
   git fetch origin
   git tag -a v0.3.0 -m "Moviestracker v0.3.0" origin/main
   git push origin v0.3.0
   ```

3. The release workflow checks that the tag is on `main`, runs CI, pushes the
   Docker image `ghcr.io/lieranderl/moviestracker:<version>` (amd64, arm64),
   builds the DMG and the Windows installer, and drafts a GitHub release with
   the files, `checksums.txt` and notes generated from the merged pull
   requests.
4. Check the draft: install the DMG on a Mac (and the installer on Windows),
   read the notes. Then publish. Releases are immutable: once published,
   neither the files nor the tag can change.

The Docker image is out as soon as the workflow runs, draft or not:
`latest` and `MAJOR.MINOR` point at it (pre-releases get only their own
version), so `docker compose pull` gets it at once.

A release that fails or turns out wrong is not repaired in place: fix it on
`main` through a pull request and release the next patch version. Anyone can
check a download or the image came from this workflow:

```bash
gh attestation verify Moviestracker-v0.3.0.dmg -R lieranderl/moviestracker-app
gh attestation verify oci://ghcr.io/lieranderl/moviestracker:0.3.0 -R lieranderl/moviestracker-app
```

The image's package on GitHub (`ghcr.io/lieranderl/moviestracker`) may start
private after the first release: make it public (Package settings → Change
visibility), or `docker pull` asks for a login.

To build locally instead: `make dmg VERSION=v0.3.0` on a Mac,
`make winapp VERSION=v0.3.0` on Windows, and `make docker-build VERSION=v0.3.0`
for the image (`make docker-smoke` tests it).

### Signing the Mac app

The app is signed ad hoc, so macOS asks users to confirm the first launch in
System Settings. With an Apple Developer ID, `scripts/macapp.sh` should sign
with `codesign --options runtime --sign "Developer ID Application: …"`, and
the release workflow should notarize the DMG with `xcrun notarytool` and
staple it, using the certificate and an App Store Connect API key stored as
secrets.

### Signing the Windows installer

The installer and programs are unsigned, so SmartScreen asks users to confirm
("More info" → "Run anyway") until a download builds reputation. With a code
signing certificate (or Azure Trusted Signing), `scripts/winapp.sh` should
sign `Moviestracker.exe`, `moviestracker-server.exe` and the installer with
`signtool sign /fd sha256 /tr <timestamp server>`; Inno Setup's `SignTool`
directive signs the uninstaller too.

## Dependencies

- Go modules and tools are pinned in `go.mod`; `go tool` runs the tools.
- TorrServer is pinned by version and SHA-256 in `scripts/torrserver.lock`
  (the Docker image too).
- The Docker base images are pinned by tag and digest in the `Dockerfile`;
  GStreamer in the image is Debian 13's.
- GStreamer for the Mac app is pinned in `macos/install-gstreamer.sh`; on
  Windows it comes inside the pinned TorrServer.
- Inno Setup, which builds the Windows installer in CI, is pinned by version
  and SHA-256 in `scripts/ci/install-inno-setup.ps1`.
- Frontend tools are pinned in `package.json` and `bun.lock`.

Updating TorrServer or GStreamer means updating the pin and its checksums
together, then checking playback on a Mac.

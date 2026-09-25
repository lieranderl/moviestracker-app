# Maintaining Moviestracker

How the repository, CI and releases are set up, for maintainers.

## Branches

Trunk-based: `main` is always releasable, and everything else is a
short-lived branch merged by pull request.

- **Rulesets** (applied by `scripts/github-setup.sh`):
  - `main` cannot be deleted or force-pushed, and keeps a linear history.
  - It changes only through pull requests: one approval from a code owner,
    conversations resolved, all required checks green on an up-to-date
    branch. Repository admins may merge their own pull requests without an
    approval (the ruleset's pull-request bypass), still with CI green.
  - `v*` tags cannot be moved or deleted.
- **Merging:** squash only; the pull request title (a Conventional Commit)
  becomes the commit on `main`. Branches are deleted after merge.
- **Required checks:** the five CI jobs and the pull request title check.
  Renaming a job means updating `scripts/github-setup.sh` and running it.

GitHub enforces rulesets on public repositories, or on private ones with a
paid plan. While the repository is private on the free plan, the rules are
saved but not enforced: keep to them by habit, and run the script again
after making it public.

## Workflows

| Workflow | When | What |
| --- | --- | --- |
| `ci.yml` | pull requests, pushes to `main`, called by releases | `make ci` (generated files, lint, race tests, govulncheck, gosec, build) and the TorrServer API contract; gitleaks over the whole history; macOS tests and a real DMG build; the Linux install, upgrade, uninstall and purge on a real systemd machine; the container image's smoke test |
| `pr.yml` | pull requests | Conventional Commit titles; dependency review (public repository) |
| `codeql.yml` | pull requests, `main`, weekly | CodeQL for Go, JavaScript and the workflows (public repository) |
| `release.yml` | `v*` tags | CI, then Linux archives and the DMG in parallel, checksums, build provenance (public repository), and a **draft** release |

Every action is pinned to a commit SHA with its version in a comment;
Dependabot updates the pins, Go modules, Bun tools and the Docker base images
weekly. Workflows get a read-only token unless a job needs more.

macOS runners cost ten times the minutes of Linux ones on private
repositories: the macOS job is the one to drop from pull requests if the
free minutes run short.

## Secrets

| Secret | Used by | What |
| --- | --- | --- |
| `MT_SHARED_TMDB_KEY` | `release.yml` | The read-only TMDB token built into releases. Without it, releases work but users must add their own key. |

Set it with `scripts/github-setup.sh` (from the git-ignored
`.tmdb-shared-key`) or `gh secret set MT_SHARED_TMDB_KEY`. To rotate it,
create a new read-only token for the Moviestracker TMDB account, update the
secret, release, then revoke the old token.

## Releasing

Versions follow [Semantic Versioning](https://semver.org): `vMAJOR.MINOR.PATCH`,
and `v1.2.0-rc.1` for pre-releases.

1. Make sure `main` is green and has what you want to ship.
2. Tag it and push the tag:

   ```bash
   git switch main && git pull
   git tag -a v0.3.0 -m "Moviestracker v0.3.0"
   git push origin v0.3.0
   ```

3. The release workflow runs CI, builds the Linux archives (amd64, arm64) and
   the DMG, and drafts a GitHub release with the files, `checksums.txt` and
   notes generated from the merged pull requests.
4. Check the draft: install the DMG on a Mac, read the notes. Then publish.

To build locally instead: `make release VERSION=v0.3.0` (Linux archives, and
the DMG on a Mac) or `make dmg VERSION=v0.3.0`.

### Signing the Mac app

The app is signed ad hoc, so macOS asks users to confirm the first launch in
System Settings. With an Apple Developer ID, `scripts/macapp.sh` should sign
with `codesign --options runtime --sign "Developer ID Application: …"`, and
the release workflow should notarize the DMG with `xcrun notarytool` and
staple it, using the certificate and an App Store Connect API key stored as
secrets.

## Dependencies

- Go modules and tools are pinned in `go.mod`; `go tool` runs the tools.
- TorrServer is pinned by version and SHA-256 in `scripts/torrserver.lock`.
- GStreamer for the Mac app is pinned in `macos/install-gstreamer.sh`.
- Frontend tools are pinned in `package.json` and `bun.lock`.

Updating TorrServer or GStreamer means updating the pin and its checksums
together, then checking playback on a Mac.

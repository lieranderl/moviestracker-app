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
| `ci.yml` | pull requests, pushes to `main`, called by releases | `make ci` (generated files, lint, race tests, govulncheck, gosec, build) and the TorrServer API contract; gitleaks over the whole history; macOS tests and a real DMG build; Windows tests, a real installer build, and its install, upgrade, quit, uninstall and purge; the Docker image end to end with `compose.yaml` (`scripts/ci/docker-e2e.sh`: TorrServer and GStreamer, setup, persistence, shutdown, licences) and its linux/arm64 build |
| `pr.yml` | pull requests | Conventional Commit titles; dependency review (public repository) |
| `codeql.yml` | pull requests, `main`, weekly | CodeQL for Go, JavaScript and the workflows (public repository) |
| `release.yml` | `v*` tags | CI, then in parallel the Docker image for linux/amd64 and linux/arm64 (pushed to `ghcr.io/lieranderl/moviestracker:<version>` with SBOM and provenance), the DMG and the Windows installer; checksums, build provenance (public repository), and a **draft** release |
| `docker-latest.yml` | a release is published | Points the image's `latest` and `MAJOR.MINOR` tags at the published version (not for pre-releases) |

Every action is pinned to a commit SHA with its version in a comment;
Dependabot updates the pins, Go modules, Bun tools and the Docker base images
weekly. Workflows get a read-only token unless a job needs more.

macOS runners cost ten times the minutes of Linux ones on private
repositories: the macOS job is the one to drop from pull requests if the
free minutes run short.

## Secrets

| Secret | Used by | What |
| --- | --- | --- |
| `MT_SHARED_TMDB_KEY` | `release.yml` | The read-only TMDB token built into releases (the Docker image gets it as a build secret, so the image history does not show it). Without it, releases work but users must add their own key. |

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

3. The release workflow runs CI, pushes the Docker image
   `ghcr.io/lieranderl/moviestracker:<version>` (amd64, arm64), builds the
   DMG and the Windows installer, and drafts a GitHub release with the files, `checksums.txt` and
   notes generated from the merged pull requests.
4. Check the draft: install the DMG on a Mac (and the installer on Windows),
   read the notes. Then publish.
5. Publishing the release (not a pre-release) moves the image's `latest`
   tag to it.

The image's package on GitHub (`ghcr.io/lieranderl/moviestracker`) starts
private, like the repository: when the repository goes public, make the
package public too (Package settings → Change visibility), or `docker pull`
asks for a login.

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

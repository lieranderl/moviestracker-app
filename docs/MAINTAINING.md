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
| `codeql.yml` | pull requests, `main`, weekly | CodeQL for Go, JavaScript and the workflows; results in templ's generated `*_templ.go` are dropped before upload |
| `release.yml` | `v*` tags | Checks the tag is on `main`, runs CI, then in parallel the Docker image for linux/amd64 and linux/arm64 (pushed to `ghcr.io/lieranderl/moviestracker:<version>` with SBOM and signed provenance), the DMG and the Windows installer; checksums, signed build provenance, and a **draft** release |
| `pins.yml` | Mondays, or by hand | `scripts/update-pins.sh` moves the TorrServer and GStreamer pins to upstream's latest stable releases once they are a week old, and opens (or refreshes) a pull request from the branch `deps/pins` as the pins app; it waits for a person (see [Dependencies](#dependencies)) |
| `docker-latest.yml` | a release is published | Points the image's `latest` and `MAJOR.MINOR` tags at the published version (not for pre-releases) |
| `web.yml` | pushes to `main` that touch the web app, or by hand | Builds `Dockerfile.web`, deploys it to Cloud Run as a revision without traffic, checks its `/readyz` (Cloud Run answers `/healthz` itself), then moves all traffic to it (see [Web app on Cloud Run](#web-app-on-cloud-run)) |

Every action is pinned to a commit SHA with its version in a comment;
Dependabot updates the pins, Go modules, Bun tools and the Docker base images
weekly. Workflows get a read-only token unless a job needs more, and zizmor
(`make lint` runs it too, with [uv](https://docs.astral.sh/uv/)) checks them
for template injection, over-broad permissions and cache poisoning. Release
builds use no caches.

## Web app on Cloud Run

The cloud web app (`cmd/web`, `Dockerfile.web`) runs as the Cloud Run service
`moviestracker-web` in the Google Cloud project `moviestracker-f07e2`
(`europe-west1`). It has no TorrServer or GStreamer: each visitor's browser
talks to their own TorrServer. It deploys on its own, from `main`
(`web.yml`), independently of the local app's `v*` releases; CI's **Web app
image and user store** job checks on every pull request that the image builds
and serves, and that the user store works with Firestore's emulator.

- **Signing in to Google Cloud:** keyless. The Workload Identity pool
  `moviestracker-web` (provider `github`) accepts only GitHub's token for
  `web.yml` on `refs/heads/main` of this repository (by repository ID), and
  maps it to `movies-web-deployer@`, which may deploy Cloud Run revisions
  (`roles/run.developer`), push to the Artifact Registry repository
  `moviestracker` and act as the runtime account. No keys are stored in
  GitHub.
- **Runtime account:** `movies-web@` (Firestore `moviestracker` database,
  read and write; the secrets `GOOGLE_SECRET` and `WEB_SESSION_KEY`).
- **Sign-in:** Google, with the OAuth client the Qwik app also uses; its
  redirect URIs list `…/auth/google/callback` for localhost:8080, the
  service's URL and later moviestracker.net. The service's settings:
  `MT_WEB_BASE_URL` (its URL), `MT_WEB_GOOGLE_CLIENT_ID`, and from Secret
  Manager `MT_WEB_GOOGLE_CLIENT_SECRET` (`GOOGLE_SECRET`) and
  `MT_WEB_SESSION_KEY` (`WEB_SESSION_KEY`, signs session cookies: a new
  version signs everyone out). Locally they come from `.env.web`
  (`.env.web.example`).
- **Catalog:** TMDB with the key in `MT_WEB_TMDB_KEY` (secret `TMDB_API_KEY`),
  IMDb ratings from `MT_WEB_IMDB_URL` (default: the Moviestracker rating
  service). The pages are the local app's (`handlers.Catalog`), with the web
  app's navigation (`views.Site`).
- **Release rows:** the home page's first rows (and their browse pages) are
  the backend's feeds of releases found on trackers, in the same database:
  `latesttorrentsmovies`, `hdr10movies`, `dvmovies` (`internal/releases`,
  read-only; each page kept five minutes).
- **Favourites:** a button on movie and TV pages (`/api/favorites`, loaded
  with the page) and a Favourites page (`/favorites`); titles and posters
  are taken from TMDB when a title is added.
- **Title sources:** movie and TV pages search JacRed through the backend
  with `MT_WEB_JACRED_KEY` (`JACRED_APIKEY`, pinned secret version 1). The
  runtime account has access to this secret only. Search reuses the local
  app's season, quality/HDR, tracker, voice and sort controls. The browser
  sends a chosen release straight to the user's selected TorrServer with
  the TMDB title, poster, category and `data` metadata:
  `{"tmdb":{"id":438631,"type":"movie"}}`. No key or TorrServer login
  is included in the rendered source results.
- **TorrServer:** each user's TorrServer addresses are kept in Firestore
  (`users/{uid}/torrservers`); their logins stay in the browser
  (localStorage). `static/torrserver.js` (from `frontend/torrserver.js`)
  reaches the TorrServer from the browser; the server never does.
  The TorrServer tab adds one magnet/HTTP(S) link or `.torrent` file (up to
  4 MB), with an optional title and poster URL. Uploads go straight to
  TorrServer's `/torrent/upload`; neither the file nor its login is sent to
  Cloud Run. After an add, the browser relays the updated list for rendering.
  Files play directly from `/stream?link=…&index=…&play`, or through hls.js
  from `/gst/{hash}/master.m3u8`. The browser relays file/probe metadata and
  live stats to signed-in rendering endpoints; audio tracks, subtitles,
  playlist/Next, VLC/IINA and copied links use the same player helpers as
  the local app. Datastar sends a heartbeat every two seconds and reports
  heartbeat failures. Close, page exit or stream change stops this viewer's
  heartbeat. TorrServer expires idle tasks: `/gst/remove` removes the shared
  hash-wide task and would interrupt another tab/device. TorrServer also
  shares one file/audio selection per hash; choosing another replaces that
  task upstream. Unknown cache capacity omits buffer percentage.
  Tested against MatriX.145 with `--httpauth`: media routes accept streams
  without a login header; API reads use the browser's saved Basic login.
  Safari and an HTTPS cloud page reaching HTTP localhost remain release checks.
- **Users' data:** preferences and favourites in the Firestore database
  `moviestracker` (`users/{uid}`, `users/{uid}/favorites/{kind}-{id}`;
  `internal/store`), beside the catalog collections the backend writes. The
  service's `MT_WEB_FIRESTORE_PROJECT` and `MT_WEB_FIRESTORE_DATABASE` name
  it; locally `make firestore` starts the emulator and `make test-store`
  checks the store against it.
- **Service settings** (made once, `web.yml` changes only the image): 1 CPU,
  512 MiB, 0–3 instances, 250 requests an instance, a 3600-second request
  timeout (Datastar reconnects its streams), public.
- **Rolling back:** `gcloud run services update-traffic moviestracker-web
  --region europe-west1 --to-revisions <revision>=100`.

## Secrets

| Secret | Used by | What |
| --- | --- | --- |
| `MT_SHARED_TMDB_KEY` | `release.yml` | The read-only TMDB token built into releases (the Docker image gets it as a build secret, so the image history does not show it). Without it, releases work but users must add their own key. |
| `JACRED_APIKEY` | `release.yml` | Moviestracker's jacred.su project key (unlimited searches), built into releases the same way and used for jacred.su only, when a user has no key of their own. Without it, users need a personal jacred.su key (100 searches a day). The project's terms ask for its referral link beside the results: `JacRedCredit` in `internal/views/footer.templ`. |

They live in the `release` environment, which only `v*` tags can use, so
pull requests and branches never see them. Set them with
`scripts/github-setup.sh` (from the git-ignored `.tmdb-shared-key` and
`.jacred-shared-key`) or `gh secret set MT_SHARED_TMDB_KEY --env release`
and `gh secret set JACRED_APIKEY --env release`. To rotate the JacRed key,
create a new one for the project at jacred.su, update the secret, release,
then revoke the old one. To rotate it,
create a new read-only token for the Moviestracker TMDB account, update the
secret, release, then revoke the old token.

The `pins` environment, which only `main` can use, holds the pins app that
opens the weekly pin updates: the variable `PINS_APP_CLIENT_ID` and the secret
`PINS_APP_PRIVATE_KEY`. Actions may not open pull requests in this
repository, and pull requests opened with `GITHUB_TOKEN` would not start CI,
so the app does it. To create it (once):

1. GitHub → Settings → Developer settings → GitHub Apps → **New GitHub App**:
   any unique name (for example `moviestracker-pins`), this repository as the
   homepage, **Webhook → Active** off; repository permissions **Contents** and
   **Pull requests**: *Read and write*; installable **Only on this account**.
2. On the app's page, copy the **Client ID** into `.pins-app-client-id` and
   **Generate a private key** into `.pins-app-key.pem` (both git-ignored).
3. **Install App** → only this repository.
4. Run `scripts/github-setup.sh`, which stores both in the `pins` environment,
   then delete the local key; `gh workflow run pins.yml` tries it at once.

To rotate the key, generate a new one on the app's page, set it with
`gh secret set PINS_APP_PRIVATE_KEY --env pins < new.pem`, and delete the old
one there.

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
5. Publishing the release (not a pre-release) moves the image's `latest`
   tag to it.

Installed apps find new releases through GitHub's `releases/latest` (published, not drafts or pre-releases) and offer the file named `Moviestracker-<version>.dmg` or `Moviestracker-Setup-<version>-x64.exe`: keep those names, or the apps link only to the release page.

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

`pins.yml` opens a pull request every week one of them has a stable release
at least a week old (`scripts/update-pins.sh`, which you can also run
yourself: it takes the SHA-256 GitHub publishes for each TorrServer file and
the `.sha256sum` beside GStreamer's installer, and skips GStreamer's odd,
development minor versions). CI builds and tests the apps and the image with
it; before merging, play an MKV (HLS) and an MP4 (Direct) in the Mac app. The
next release carries the new versions to everyone; nothing updates them in
installed apps on its own.

Waiting upstream: TorrServer copies AAC Main audio into HLS unchanged, which
browsers refuse (the player explains it). The fix is
[YouROK/TorrServer#879](https://github.com/YouROK/TorrServer/pull/879); once a
release has it, move `scripts/torrserver.lock` to that release.

### Cloud TorrServer settings

The TorrServer tab offers torrent engine, streaming, storage, network, other-device and GStreamer settings. The browser reads settings; Cloud Run renders the form and validates only the known editable fields from `settings_spec.go`. Unrelated fields, including credentials and service keys, are not relayed to Cloud Run.

Before saving, the browser reads fresh settings and merges the validated changes, preserving unknown fields because TorrServer replaces the whole object. Startup options and engine restart commands are excluded. Saving engine settings causes TorrServer's own reconnect; GStreamer changes apply to new streams. Closing the panel or changing the selected server/section cancels pending browser requests. An already accepted save cannot be undone by closing the panel.

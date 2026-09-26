#!/usr/bin/env bash
# Single-quoted scripts below run in the container and expand there.
# shellcheck disable=SC2016

# Builds the image and runs it with compose.yaml as a Linux server or NAS
# would, then checks it end to end: health, the bundled TorrServer on
# loopback with working GStreamer, non-root processes, only port 8095
# published, first-run setup with the code from the log, persistence across a
# restart, graceful shutdown, an external TorrServer, and the licences.
#
#   scripts/ci/docker-e2e.sh           (make docker-smoke)
#
# It needs Docker with Compose and port 8095 free, and removes what it made.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
image="${IMAGE:-moviestracker:e2e}"
key="e2e-shared-tmdb-key-$$"
base=http://127.0.0.1:8095
work="$(mktemp -d)"
project=moviestracker-e2e
compose=(docker compose --project-name "$project" -f "$root/compose.yaml" -f "$work/override.yaml")
cat > "$work/override.yaml" <<EOF
services:
  moviestracker:
    image: $image
    container_name: $project
EOF
cleanup() {
  "${compose[@]}" down --volumes >/dev/null 2>&1 || true
  docker rm -f "$project-external" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

step() { printf '\n==> %s\n' "$*"; }
fail() {
  printf 'FAIL: %s\n' "$*" >&2
  docker logs --tail 80 "$project" >&2 2>&1 || true
  exit 1
}
in_container() { docker exec "$project" "$@"; }
health() { docker inspect -f '{{.State.Health.Status}}' "$project" 2>/dev/null || true; }
wait_healthy() {
  for _ in $(seq 1 90); do
    [ "$(health)" = healthy ] && return 0
    sleep 1
  done
  fail "the container did not become healthy (health: $(health))"
}
location() { curl -sS -o /dev/null -w '%{redirect_url}' "$base$1"; }
# engine_get asks the bundled TorrServer, which only the container reaches.
engine_get() {
  local password port
  password="$(in_container cat /data/engine/accs.db | sed -E 's/.*"moviestracker":"([0-9a-f]+)".*/\1/')"
  port="$(engine_port "$project")"
  docker run --rm --network "container:$project" curlimages/curl:8.22.0 -fsS -u "moviestracker:$password" "http://127.0.0.1:$port$1"
}
# commands lists the command lines of the processes in container $1.
commands() {
  docker exec "$1" bash -c 'for p in /proc/[0-9]*; do tr "\0" " " <"$p/cmdline" 2>/dev/null; echo; done'
}
# engine_port is the API port of the bundled TorrServer in container $1.
engine_port() { commands "$1" | sed -n 's|^/usr/local/bin/torrserver .*--port \([0-9]*\).*|\1|p' | head -n 1; }

step "Build the image (the shared TMDB key as a build secret)"
if [ -z "${IMAGE:-}" ]; then
  MT_SHARED_TMDB_KEY="$key" docker build --secret id=tmdb_key,env=MT_SHARED_TMDB_KEY \
    --build-arg VERSION=v0.0.0-e2e -t "$image" "$root"
  docker run --rm --entrypoint grep "$image" -q "$key" /usr/local/bin/moviestracker ||
    fail "the binary lacks the shared TMDB key it was built with"
  [ "$(docker run --rm "$image" --version)" = "moviestracker v0.0.0-e2e" ] || fail "the image does not know its version"
  docker history --no-trunc "$image" | grep -q "$key" && fail "the image history holds the shared TMDB key"
fi

step "Licences travel with the image"
for f in LICENSE NOTICE TorrServer-LICENSE TorrServer-SOURCE.txt GSTREAMER.txt; do
  docker run --rm --entrypoint test "$image" -s "/usr/share/doc/moviestracker/$f" || fail "no /usr/share/doc/moviestracker/$f"
done
for pkg in libgstreamer1.0-0 gstreamer1.0-plugins-good gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly gstreamer1.0-libav; do
  docker run --rm --entrypoint test "$image" -s "/usr/share/doc/$pkg/copyright" || fail "no copyright file for $pkg"
done
docker run --rm --entrypoint grep "$image" -q "GNU AFFERO GENERAL PUBLIC LICENSE" /usr/share/doc/moviestracker/LICENSE ||
  fail "the image's LICENSE is not the AGPL"

step "Start with compose.yaml"
"${compose[@]}" up -d
wait_healthy
[ "$(in_container moviestracker --health && echo ok)" = ok ] || fail "moviestracker --health fails"
published="$(docker port "$project" | sed 's/ ->.*//' | sort -u | tr '\n' ' ')"
[ "$published" = "8095/tcp " ] || fail "published ports: $published (want only 8095/tcp)"

step "Moviestracker runs the bundled TorrServer on loopback, all as uid 1000"
port="$(engine_port "$project")"
[ -n "$port" ] || fail "TorrServer is not running"
# /proc/net/tcp: local address:port in hex, state 0A is LISTEN.
hexport="$(printf '%04X' "$port")"
listen="$(in_container cat /proc/net/tcp /proc/net/tcp6 | awk -v p=":$hexport" '$4 == "0A" && $2 ~ p"$" {print $2}')"
[ "$listen" = "0100007F:$hexport" ] || fail "TorrServer's API listens on ${listen:-nothing}, not only 127.0.0.1"
uids="$(in_container bash -c 'for p in /proc/[0-9]*; do awk "/^Uid:/{print \$2}" "$p/status"; done' | sort -u | tr '\n' ' ')"
[ "$uids" = "1000 " ] || fail "processes run as uid(s) $uids, not only 1000"
in_container bash -c 'tr "\0" " " </proc/1/cmdline' | grep -q '^/usr/bin/tini' || fail "PID 1 is not tini"

step "TorrServer's GStreamer works, and converts MKV with AC3 for the browser"
echo_json="$(engine_get /gst/echo)"
echo "$echo_json"
echo "$echo_json" | grep -q '"gstreamer":{"found":true,"available":true,"works":true' || fail "TorrServer has no working GStreamer"
echo "$echo_json" | grep -q '"gst_discoverer":{"found":true,"available":true,"works":true' || fail "gst-discoverer does not work"
in_container bash -c '
  set -e
  gst-launch-1.0 -q videotestsrc num-buffers=60 ! video/x-raw,width=320,height=240,framerate=30/1 ! x264enc tune=zerolatency ! h264parse ! \
    matroskamux name=m ! filesink location=/tmp/e2e.mkv audiotestsrc num-buffers=60 ! audioconvert ! avenc_ac3 ! ac3parse ! m.
  timeout 60 gst-launch-1.0 -q filesrc location=/tmp/e2e.mkv ! matroskademux name=d \
    d.video_0 ! queue ! h264parse ! avdec_h264 ! videoconvert ! x264enc tune=zerolatency ! fakesink sync=false \
    d.audio_0 ! queue ! ac3parse ! avdec_ac3 ! audioconvert ! audioresample ! avenc_aac ! fakesink sync=false
  for e in avdec_h265 avdec_dca avdec_eac3 avdec_truehd; do gst-inspect-1.0 --exists "$e"; done
  rm -f /tmp/e2e.mkv' || fail "GStreamer cannot convert MKV with AC3 to H.264 and AAC"

step "First-run setup from another device, with the code from the log"
[ "$(location /movies)" = "$base/setup" ] || fail "a fresh container does not send visitors to setup"
code="$(docker logs "$project" 2>&1 | sed -n 's/.*setup_code=\([A-Z0-9-]*\).*/\1/p' | tail -n 1)"
[ -n "$code" ] || fail "the log shows no setup code"
password="$(openssl rand -hex 16)"
curl -fsS -X POST "$base/api/setup" -H 'Content-Type: application/json' -H 'Datastar-Request: true' \
  --data "{\"accepted\":true,\"username\":\"e2e\",\"password\":\"$password\",\"setupCode\":\"$code\"}" >/dev/null
[ "$(location /setup)" = "$base/login" ] || fail "setup did not create the administrator"
engine_password="$(in_container cat /data/engine/accs.db)"

step "Stopping is graceful and quick"
started=$(date +%s)
"${compose[@]}" stop
took=$(( $(date +%s) - started ))
[ "$took" -lt 25 ] || fail "stopping took ${took}s"
[ "$(docker inspect -f '{{.State.ExitCode}}' "$project")" = 0 ] || fail "exit code $(docker inspect -f '{{.State.ExitCode}}' "$project")"
docker logs "$project" 2>&1 | grep -q "server stopped cleanly" || fail "the server did not stop cleanly"

step "Accounts, settings and TorrServer's state persist in the volume"
"${compose[@]}" up -d
wait_healthy
[ "$(location /setup)" = "$base/login" ] || fail "the restart lost the administrator"
[ "$(in_container cat /data/engine/accs.db)" = "$engine_password" ] || fail "TorrServer's credentials changed"
in_container test -s /data/engine/config.db || fail "TorrServer's database is not in the volume"
"${compose[@]}" down
"${compose[@]}" up -d
wait_healthy
[ "$(location /setup)" = "$base/login" ] || fail "down and up again lost the administrator"
"${compose[@]}" down --volumes

step "An external TorrServer replaces the bundled one"
docker run -d --name "$project-external" -e TORRSERVER_URL=http://192.0.2.1:8090 "$image" >/dev/null
for _ in $(seq 1 30); do
  docker logs "$project-external" 2>&1 | grep -q "Moviestracker running" && break
  sleep 1
done
docker logs "$project-external" 2>&1 | grep -q 'torrserver=http://192.0.2.1:8090' || fail "TORRSERVER_URL was not used"
[ -z "$(engine_port "$project-external")" ] || fail "the bundled TorrServer runs although TORRSERVER_URL is set"
docker rm -f "$project-external" >/dev/null

step "A data folder it cannot write is explained"
if docker run --rm --tmpfs /data:uid=0,mode=0755 "$image" >"$work/denied.log" 2>&1; then
  fail "it started with a data folder it cannot write"
fi
grep -q "chown -R 1000:1000 /data" "$work/denied.log" || { cat "$work/denied.log"; fail "the error does not say how to fix the folder"; }

printf '\nThe Docker image works: health, TorrServer with GStreamer, setup, persistence, shutdown, licences.\n'

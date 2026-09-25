#!/usr/bin/env bash
# Installs a Linux release archive on this machine as a user would, then checks
# the whole life cycle against the real systemd: install, first-run setup,
# upgrade in place, uninstall (keeping settings), reinstall, and purge.
#
#   scripts/ci/linux-install-e2e.sh dist/moviestracker_<version>_linux_amd64.tar.gz
#
# It needs root (sudo) and systemd, and leaves nothing behind when it passes.
# Run it on a throwaway machine: CI, or a VM, not your media server.
set -euo pipefail

archive="$(cd "$(dirname "${1:?usage: linux-install-e2e.sh <archive.tar.gz>}")" && pwd)/$(basename "$1")"
base=http://127.0.0.1:8095
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

step() { printf '\n==> %s\n' "$*"; }
fail() {
  printf 'FAIL: %s\n' "$*" >&2
  sudo journalctl -u moviestracker --no-pager -n 80 >&2 || true
  exit 1
}

# wait_for_health waits until the service answers /healthz.
wait_for_health() {
  for _ in $(seq 1 60); do
    [ "$(curl -fsS "$base/healthz" 2>/dev/null)" = ok ] && return 0
    sleep 1
  done
  fail "Moviestracker did not answer $base/healthz"
}

# location prints where a GET of $1 redirects.
location() { curl -sS -o /dev/null -w '%{redirect_url}' "$base$1"; }

tar -xzf "$archive" -C "$work"
cd "$work"/moviestracker_*_linux_*

step "Install"
sudo ./install.sh --without-gstreamer | tee "$work/install.log"
grep -q "Moviestracker is running" "$work/install.log" || fail "the installer did not say where to open it"
grep -q "Enter this setup code: [A-Z0-9]\{4\}-[A-Z0-9]\{4\}" "$work/install.log" || fail "the installer did not show the setup code"
wait_for_health
systemctl is-active --quiet moviestracker || fail "the service is not active"
systemctl is-enabled --quiet moviestracker || fail "the service does not start at boot"
[ "$(stat -c %U /var/lib/moviestracker)" = moviestracker ] || fail "the data folder is not owned by the service user"
pgrep -u moviestracker -x torrserver >/dev/null || fail "TorrServer is not running under the service user"
sudo ss -ltnp | grep torrserver | grep -qv '127.0.0.1' && fail "TorrServer listens beyond loopback"
[ "$(location /movies)" = "$base/setup" ] || fail "a fresh install does not send visitors to setup"

step "First-run setup from this machine"
password="$(openssl rand -hex 16)"
curl -fsS -X POST "$base/api/setup" -H 'Content-Type: application/json' -H 'Datastar-Request: true' \
  --data "{\"accepted\":true,\"username\":\"ci\",\"password\":\"$password\"}" >/dev/null
[ "$(location /setup)" = "$base/login" ] || fail "setup did not create the administrator"

step "Upgrade in place keeps the accounts"
sudo ./install.sh --without-gstreamer >/dev/null
wait_for_health
[ "$(location /setup)" = "$base/login" ] || fail "the upgrade lost the administrator"

step "Uninstall keeps settings, removes the programs and TorrServer"
sudo /usr/local/lib/moviestracker/uninstall.sh
! systemctl is-active --quiet moviestracker || fail "the service still runs"
for gone in /usr/local/bin/moviestracker /usr/local/lib/moviestracker /etc/systemd/system/moviestracker.service /var/lib/moviestracker/engine; do
  [ ! -e "$gone" ] || fail "$gone is left behind"
done
[ -d /var/lib/moviestracker ] && [ -f /etc/moviestracker/moviestracker.env ] || fail "the settings were not kept"
pgrep -x torrserver >/dev/null && fail "TorrServer still runs"

step "Reinstall picks the settings up again"
sudo ./install.sh --without-gstreamer >/dev/null
wait_for_health
[ "$(location /setup)" = "$base/login" ] || fail "the reinstall lost the administrator"

step "Purge removes everything"
sudo /usr/local/lib/moviestracker/uninstall.sh --purge
for gone in /usr/local/bin/moviestracker /usr/local/lib/moviestracker /var/lib/moviestracker /etc/moviestracker /etc/systemd/system/moviestracker.service; do
  [ ! -e "$gone" ] || fail "$gone is left behind after --purge"
done
id moviestracker >/dev/null 2>&1 && fail "the moviestracker user is left behind"
curl -fsS "$base/healthz" >/dev/null 2>&1 && fail "something still answers on 8095"

printf '\nThe Linux install, upgrade, uninstall and purge all work.\n'

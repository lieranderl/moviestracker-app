#!/bin/sh
# Installs Moviestracker (and the TorrServer it runs) on Linux from this folder:
#
#   sudo ./install.sh     a system service (systemd), data in /var/lib/moviestracker
#
# It stays installed as /usr/local/lib/moviestracker/uninstall.sh, which removes it.
# On macOS, install Moviestracker.app from the Moviestracker DMG instead.
#
# Options:
#   --uninstall     remove the programs and the service; keep settings and data
#   --purge         with --uninstall: also remove settings and data
#   --no-service    install files only; do not create a user or start a service
#   --prefix DIR    install under DIR instead of /
#   --with-gstreamer     install GStreamer for browser playback of MKV files
#   --without-gstreamer  do not install GStreamer and do not ask
#
# Without either GStreamer option the installer asks, when there is someone to ask.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
# The system and version this archive is for; scripts/release.sh fills them in.
archive_target=
archive_version=
action=install
case "${0##*/}" in uninstall.sh) action=uninstall ;; esac
purge=no
service=yes
prefix=
gstreamer=ask

usage() {
	awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$0"
}

while [ $# -gt 0 ]; do
	case "$1" in
	--uninstall) action=uninstall ;;
	--purge) purge=yes ;;
	--no-service) service=no ;;
	--with-gstreamer) gstreamer=yes ;;
	--without-gstreamer) gstreamer=no ;;
	--prefix)
		[ $# -ge 2 ] || { echo "--prefix needs a folder" >&2; exit 2; }
		prefix=$2
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "unknown option: $1" >&2
		usage >&2
		exit 2
		;;
	esac
	shift
done

say() { printf '%s\n' "$*"; }

case "$(uname -s)" in
Linux) ;;
Darwin)
	echo "On macOS, install Moviestracker.app: open the Moviestracker DMG and drag the app to Applications." >&2
	exit 1
	;;
*)
	echo "Moviestracker installs on Linux (this script) and macOS (the Moviestracker DMG)." >&2
	exit 1
	;;
esac

# machine_target prints this machine as os/arch, as release archives name it.
machine_target() {
	case "$(uname -m)" in
	x86_64 | amd64) echo linux/amd64 ;;
	arm64 | aarch64) echo linux/arm64 ;;
	*) echo "linux/$(uname -m)" ;;
	esac
}

describe() {
	case "$1" in
	linux/amd64) echo "x86-64 (Intel or AMD) computers" ;;
	linux/arm64) echo "ARM64 computers (Raspberry Pi 4/5, ARM servers)" ;;
	*) echo "$1" ;;
	esac
}

if [ "$action" = install ] && [ -n "$archive_target" ]; then
	machine=$(machine_target)
	if [ "$machine" != "$archive_target" ]; then
		echo "This archive is for $(describe "$archive_target"), but this machine is not one of them." >&2
		echo "Download moviestracker_${archive_version}_${machine%/*}_${machine#*/}.tar.gz instead." >&2
		exit 1
	fi
fi

if [ "$action" = install ] && { [ ! -f "$here/moviestracker" ] || [ ! -f "$here/torrserver" ]; }; then
	cat >&2 <<'MISSING'
moviestracker and torrserver are not next to install.sh: run it from an
unpacked release archive. To build one from the source folder:

  make release VERSION=v0.1.0
  tar xzf dist/moviestracker_v0.1.0_linux_<arch>.tar.gz
  cd moviestracker_v0.1.0_linux_<arch> && sudo ./install.sh
MISSING
	exit 1
fi

# ------------------------------------------------------------ GStreamer

# gst_version prints the installed GStreamer version, or nothing.
gst_version() {
	command -v gst-inspect-1.0 >/dev/null 2>&1 || return 0
	GST_REGISTRY_UPDATE=no gst-inspect-1.0 --version 2>/dev/null | awk '/^GStreamer / { print $2; exit }'
}

# gst_new_enough tells whether version $1 is 1.22 or newer, which TorrServer needs.
gst_new_enough() {
	printf '%s\n' "$1" | awk -F. '{ exit !($1 > 1 || ($1 == 1 && $2 >= 22)) }'
}

# gst_command prints the command that installs GStreamer here, or nothing.
gst_command() {
	sudo=
	[ "$(id -u)" -eq 0 ] || sudo="sudo "
	if command -v apt-get >/dev/null 2>&1; then
		echo "${sudo}apt-get install -y gstreamer1.0-tools gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly gstreamer1.0-libav"
	elif command -v dnf >/dev/null 2>&1; then
		echo "${sudo}dnf install -y gstreamer1 gstreamer1-plugins-base gstreamer1-plugins-good gstreamer1-plugins-bad-free gstreamer1-plugins-ugly-free gstreamer1-plugin-libav"
	elif command -v pacman >/dev/null 2>&1; then
		echo "${sudo}pacman -S --needed --noconfirm gstreamer gst-plugins-base gst-plugins-good gst-plugins-bad gst-plugins-ugly gst-libav"
	fi
	return 0
}

# gst_remove_command prints the command that removes the GStreamer packages
# gst_command adds. It asks the package manager's usual question, so the
# person sees anything else that would go with them.
gst_remove_command() {
	gst_command | sed -e 's/ install -y / remove /' -e 's/ -S --needed --noconfirm / -R /'
}

gst_requirements() {
	say "  Browser playback needs GStreamer 1.22 or newer: Debian 12+, Ubuntu 24.04+,"
	say "  Raspberry Pi OS 12 (Bookworm)+, Fedora 38+ or Arch Linux."
}

# gstreamer_step explains GStreamer and installs it when asked to.
gstreamer_step() {
	v=$(gst_version)
	if [ -n "$v" ] && gst_new_enough "$v"; then
		say "GStreamer $v found: MKV files play in the browser."
		return 0
	fi
	[ "$gstreamer" = no ] && return 0
	cmd=$(gst_command)

	say ""
	if [ -n "$v" ]; then
		say "GStreamer $v is installed, but browser playback needs 1.22 or newer."
	else
		say "GStreamer (optional, recommended)"
	fi
	say "  Browsers cannot play MKV files, the usual format of movie torrents, nor their AC3 or DTS"
	say "  audio. With GStreamer, TorrServer turns them into a stream every browser plays, converting"
	say "  only what it must. Without it, MKV files still play in VLC, on TVs and in players like"
	say "  Infuse through Moviestracker's stream links."
	if [ -z "$cmd" ]; then
		gst_requirements
		return 0
	fi

	if [ "$gstreamer" = ask ]; then
		if [ ! -t 0 ]; then
			say "  To add it (a few hundred MB):  $cmd"
			return 0
		fi
		printf '  Install it now with "%s" (a few hundred MB)? [Y/n] ' "$cmd"
		read -r answer || answer=n
		case "$answer" in
		"" | [Yy]*) ;;
		*)
			say "  Skipped. To add it later:  $cmd"
			return 0
			;;
		esac
	fi

	case "$cmd" in
	sudo\ apt-get*) sudo apt-get update || true ;;
	apt-get*) apt-get update || true ;;
	esac
	# shellcheck disable=SC2086 # the command is split into its words on purpose
	if ! $cmd; then
		say "GStreamer could not be installed; Moviestracker works without it. To try again:  $cmd"
		return 0
	fi
	v=$(gst_version)
	# Remembered, so the uninstaller can offer to remove what it added.
	gst_remove_command >"$lib/gstreamer-added"
	if [ -n "$v" ] && gst_new_enough "$v"; then
		say "GStreamer $v is installed: MKV files play in the browser."
	else
		say "This system's GStreamer (${v:-unknown version}) is too old for browser playback."
		gst_requirements
	fi
}

# remove_gstreamer offers to remove the GStreamer packages the installer
# added ($1 removes them). Other programs may use them, so it asks.
remove_gstreamer() {
	say ""
	say "The installer added GStreamer for Moviestracker. Other programs may use it too."
	if [ ! -t 0 ]; then
		say "To remove it:  $1"
		return 0
	fi
	printf 'Remove it too? %s [Y/n] ' "($1)"
	read -r answer || answer=n
	case "$answer" in
	"" | [Yy]*)
		# shellcheck disable=SC2086 # the command is split into its words on purpose
		$1 || say "GStreamer was not removed. To try again:  $1"
		;;
	*) say "GStreamer is kept. To remove it later:  $1" ;;
	esac
}

# ---------------------------------------------------------------- Linux

linux() {
	root=${prefix%/}
	bin=$root/usr/local/bin/moviestracker
	lib=$root/usr/local/lib/moviestracker
	doc=$root/usr/local/share/doc/moviestracker
	etc=$root/etc/moviestracker
	env=$etc/moviestracker.env
	data=$root/var/lib/moviestracker
	unit=$root/etc/systemd/system/moviestracker.service

	if [ "$service" = yes ] && [ "$(id -u)" -ne 0 ]; then
		echo "Run it with sudo: it installs a system service." >&2
		exit 1
	fi

	if [ "$action" = uninstall ]; then
		if [ "$service" = yes ]; then
			systemctl disable --now moviestracker.service 2>/dev/null || true
		fi
		gst_added=
		[ -f "$lib/gstreamer-added" ] && gst_added=$(cat "$lib/gstreamer-added")
		rm -f "$bin" "$unit"
		rm -rf "$lib" "$doc" # the running uninstall.sh too: sh has it open
		# TorrServer goes with Moviestracker: its program (in $lib), torrent
		# list, settings and logs.
		rm -rf "$data/engine"

		[ "$service" = yes ] && systemctl daemon-reload
		if [ "$purge" = yes ]; then
			rm -rf "$data" "$etc"
			[ "$service" = yes ] && userdel moviestracker 2>/dev/null || true
			say "Moviestracker, its settings and its data are removed."
		else
			say "Moviestracker and TorrServer are removed; Moviestracker's accounts and settings in $data and $etc are kept."
		fi
		if [ -n "$gst_added" ]; then
			remove_gstreamer "$gst_added"
		fi
		return
	fi

	if [ "$service" = yes ] && systemctl is-active --quiet moviestracker.service; then
		say "Stopping the running Moviestracker to upgrade it…"
		systemctl stop moviestracker.service
	fi
	install -d -m 0755 "$(dirname "$bin")" "$lib" "$doc" "$(dirname "$unit")"
	install -m 0755 "$here/moviestracker" "$bin"
	install -m 0755 "$here/torrserver" "$lib/torrserver"
	install -m 0755 "$here/install.sh" "$lib/uninstall.sh"
	install -m 0644 "$here/LICENSE" "$here/NOTICE" "$doc/"
	cp -R "$here/licenses" "$doc/"
	install -d -m 0750 "$data"
	install -d -m 0755 "$etc"
	if [ ! -f "$env" ]; then # an admin's edits survive upgrades
		cat >"$env" <<'ENV'
# Moviestracker service settings; restart the service after a change:
#   sudo systemctl restart moviestracker
MT_LISTEN=:8095
MT_DATA_DIR=/var/lib/moviestracker
MT_TORRSERVER_BIN=/usr/local/lib/moviestracker/torrserver
# Extra hostnames to answer to, e.g. MT_HOSTNAMES=media.home
#MT_HOSTNAMES=
# Behind HTTPS (a reverse proxy): secure cookies
#MT_SECURE_COOKIES=true
ENV
		chmod 0640 "$env"
	fi
	cat >"$unit" <<'UNIT'
[Unit]
Description=Moviestracker (with its TorrServer)
Documentation=file:///usr/local/share/doc/moviestracker
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=moviestracker
Group=moviestracker
EnvironmentFile=/etc/moviestracker/moviestracker.env
ExecStart=/usr/local/bin/moviestracker
Restart=on-failure
RestartSec=5
# TorrServer is a child process: stop Moviestracker first, then give both time.
KillMode=mixed
TimeoutStopSec=20

# Hardening: only the data folder is writable. To use a disk cache folder
# elsewhere, add it with: sudo systemctl edit moviestracker
#   [Service]
#   ReadWritePaths=/srv/torrent-cache
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true
ReadWritePaths=/var/lib/moviestracker

[Install]
WantedBy=multi-user.target
UNIT
	chmod 0644 "$unit"

	gstreamer_step
	if [ "$service" = yes ]; then
		if ! id moviestracker >/dev/null 2>&1; then
			useradd --system --home-dir /var/lib/moviestracker --no-create-home --shell /usr/sbin/nologin moviestracker
		fi
		chown -R moviestracker:moviestracker "$data"
		chgrp moviestracker "$env"
		systemctl daemon-reload
		systemctl enable --now moviestracker.service
		host=$(hostname -I 2>/dev/null | awk '{print $1}')
		say "Moviestracker is running: open http://${host:-localhost}:8095 (on this machine: http://localhost:8095)."
		# Another device needs the setup code the service printed at start.
		code=
		for _ in 1 2 3 4 5 6 7 8 9 10; do
			code=$(journalctl -u moviestracker.service -n 200 -o cat 2>/dev/null | sed -n 's/.*setup_code=\([A-Z0-9-]*\).*/\1/p' | tail -n 1)
			[ -n "$code" ] && break
			sleep 1
		done
		if [ -n "$code" ]; then
			say "Setting up from another device? Enter this setup code: $code"
		else
			say "Setting up from another device? Find its setup code with: sudo journalctl -u moviestracker | grep setup_code"
		fi
	else
		say "Files installed under ${root:-/}; no service started (--no-service)."
	fi
	say "To remove it:  sudo /usr/local/lib/moviestracker/uninstall.sh   (add --purge to delete settings and data)"
}

linux

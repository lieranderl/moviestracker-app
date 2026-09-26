Moviestracker
=============

A personal movie and series catalog that runs on your own computer or home
server, with TorrServer as its streaming engine.

Install (Linux)
---------------
  sudo ./install.sh

It runs as a system service (systemd). On a Mac, use the Moviestracker DMG
instead: it holds Moviestracker.app, a menu bar app.

Then open http://localhost:8095 (or http://<this machine>:8095 from a TV or
phone on your network), create the administrator account, and follow the
setup: a free TMDB key, JacRed for torrent search, and where TorrServer runs
(Moviestracker runs the one in this archive for you).

GStreamer (optional, recommended): browsers cannot play MKV files, the usual
format of movie torrents, nor their AC3 or DTS audio. With GStreamer 1.22 or
newer, TorrServer turns them into a stream every browser plays. Without it,
MKV files still play in VLC, on TVs and in players like Infuse. install.sh
offers to install it with apt, dnf or pacman;
--with-gstreamer or --without-gstreamer answer for you. Systems that have
1.22+: Debian 12+, Ubuntu 24.04+, Raspberry Pi OS 12+,
Fedora 38+, Arch Linux. On Fedora, HEVC video and some audio formats also
need the RPM Fusion repositories.

Upgrade: run install.sh from a newer archive. Settings and data are kept.
Remove:  sudo /usr/local/lib/moviestracker/uninstall.sh
         (add --purge to delete settings and data)

What is inside
--------------
  moviestracker  the app (AGPL-3.0, see LICENSE and NOTICE)
  torrserver     TorrServer by YouROK, unmodified (GPL-3.0, see licenses/)
  install.sh     the installer

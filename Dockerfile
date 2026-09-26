# syntax=docker/dockerfile:1
# Moviestracker for Linux, NAS and home servers (linux/amd64, linux/arm64):
# Moviestracker runs the bundled TorrServer (GStreamer build) itself, on
# loopback inside the container, so only port 8095 is exposed.
#
#   docker build -t moviestracker:local .
#   docker compose up -d          (compose.yaml)
#
# Release builds pass --build-arg VERSION=v1.2.3 and the shared TMDB key as a
# build secret (never an argument, which the image history would keep):
#   --secret id=tmdb_key,env=MT_SHARED_TMDB_KEY

# The build stages run on the build machine and cross-compile for the target.
FROM --platform=$BUILDPLATFORM oven/bun:1.4.2@sha256:9114c058aeae42162ee16dd5084b95fe9473970bb6bcb5b232ab1630f0546895 AS assets
WORKDIR /src
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile
COPY frontend ./frontend
COPY internal ./internal
COPY static ./static
RUN bun run assets

FROM --platform=$BUILDPLATFORM golang:1.27.1-bookworm@sha256:69a7b9788769bec032d238959b61854e9ae87f57be9029ec04e9885fabf99195 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=assets /src/static/app.css /src/static/player.js /src/static/theme.js /src/static/
COPY --from=assets /src/internal/views/icons_gen.go /src/internal/views/icons_gen.go
ARG TARGETOS TARGETARCH
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=secret,id=tmdb_key,required=false \
    key="" && if [ -f /run/secrets/tmdb_key ]; then key="$(tr -d '[:space:]' </run/secrets/tmdb_key)"; fi && \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" go build -trimpath \
      -ldflags "-s -w -X main.version=$VERSION -X main.sharedTMDBKey=$key" \
      -o /out/bin/moviestracker ./cmd/server
# TorrServer's GStreamer build for the target CPU, pinned and checked against
# scripts/torrserver.lock, with its licence and source link.
RUN TS_TARGET="$TARGETOS/$TARGETARCH" scripts/fetch-torrserver.sh /out/bin/torrserver && \
    scripts/fetch-torrserver.sh --license /out/doc/TorrServer-LICENSE && \
    ts_version="$(awk '$1 == "version" {print $2}' scripts/torrserver.lock)" && \
    printf '%s\n' \
      "/usr/local/bin/torrserver is TorrServer $ts_version by YouROK, unmodified, licensed" \
      "under the GNU General Public License v3 (see TorrServer-LICENSE). Its complete" \
      "source code is available at:" "" \
      "  https://github.com/YouROK/TorrServer/tree/$ts_version" "" \
      "Moviestracker runs it as a separate program and talks to it over HTTP." \
      > /out/doc/TorrServer-SOURCE.txt && \
    install -m 0644 LICENSE NOTICE packaging/docker/GSTREAMER.txt /out/doc/

FROM debian:trixie-slim@sha256:a99cfc517144bc59b1978475ec53b46ecabec7e43635402ee5b77cc54cd1b20a
# GStreamer as TorrServer's README lists it (Debian 13: GStreamer 1.26), for
# MKV, AC3/DTS and HEVC in the browser; tini forwards signals and reaps.
# Each package keeps its licence in /usr/share/doc/<package>/copyright.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      ca-certificates tini \
      libgstreamer1.0-0 libgstreamer-plugins-base1.0-0 \
      gstreamer1.0-plugins-base gstreamer1.0-plugins-base-apps \
      gstreamer1.0-plugins-good gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly \
      gstreamer1.0-libav gstreamer1.0-tools && \
    rm -rf /var/lib/apt/lists/* && \
    groupadd --gid 1000 moviestracker && \
    useradd --uid 1000 --gid moviestracker --home-dir /data --no-create-home \
      --shell /usr/sbin/nologin moviestracker && \
    install -d -o moviestracker -g moviestracker -m 0700 /data
COPY --from=build /out/bin/ /usr/local/bin/
COPY --from=build /out/doc/ /usr/share/doc/moviestracker/

ARG VERSION=dev
LABEL org.opencontainers.image.title="Moviestracker" \
      org.opencontainers.image.description="Your own movie and TV catalog, with TorrServer streaming" \
      org.opencontainers.image.source="https://github.com/lieranderl/moviestracker-app" \
      org.opencontainers.image.licenses="AGPL-3.0-only AND GPL-3.0-only" \
      org.opencontainers.image.version=$VERSION

# /data keeps accounts, settings and TorrServer's database, settings and log
# (/data/engine). TorrServer listens on 127.0.0.1 only. MT_LAN_ADDRESS=off:
# the container's own address is not one TVs can reach (set the host's).
ENV MT_LISTEN=:8095 MT_DATA_DIR=/data MT_LAN_ADDRESS=off HOME=/data
VOLUME /data
# 8090: TorrServer for other apps, when switched on in Settings → Other apps.
EXPOSE 8095 8090
USER 1000:1000
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --start-interval=2s --retries=3 \
  CMD ["/usr/local/bin/moviestracker", "--health"]
# Stopping takes up to about 20 seconds (open pages, then TorrServer):
# `docker stop -t 30`, stop_grace_period in compose.yaml.
STOPSIGNAL SIGTERM
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/moviestracker"]

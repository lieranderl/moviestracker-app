FROM oven/bun:1.4.2 AS assets
WORKDIR /src
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile
COPY frontend ./frontend
COPY internal ./internal
COPY static ./static
RUN bun run assets

FROM golang:1.27.1-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=assets /src/static/app.css /src/static/app.css
COPY --from=assets /src/static/player.js /src/static/player.js
COPY --from=assets /src/static/theme.js /src/static/theme.js
COPY --from=assets /src/internal/views/icons_gen.go /src/internal/views/icons_gen.go
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
ARG SOURCE_URL=local
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown
LABEL org.opencontainers.image.source=$SOURCE_URL \
      org.opencontainers.image.revision=$VCS_REF \
      org.opencontainers.image.created=$BUILD_DATE
COPY --from=build --chown=nonroot:nonroot /out/server /server
# Accounts, sessions and sources live in /data: mount a volume there.
ENV MT_LISTEN=:8095 MT_DATA_DIR=/data
VOLUME /data
EXPOSE 8095
USER nonroot:nonroot
ENTRYPOINT ["/server"]

#!/usr/bin/env bash
# Points the runner's Docker daemon at Google's Docker Hub mirror, so the
# image jobs' Docker Hub pulls (base images, test containers) do not hit Docker
# Hub's limit on anonymous pulls, which GitHub's shared runners exhaust. The
# images stay pinned by digest, so the mirror cannot swap them; a pull the
# mirror cannot serve falls back to Docker Hub.
#
#   scripts/ci/docker-hub-mirror.sh   (CI only: rewrites /etc/docker/daemon.json)
set -euo pipefail

config=/etc/docker/daemon.json
current=$(sudo cat "$config" 2>/dev/null || true)
[ -n "$current" ] || current='{}'
jq '."registry-mirrors" = ["https://mirror.gcr.io"]' <<<"$current" | sudo tee "$config" >/dev/null
sudo systemctl restart docker
docker info --format '{{.RegistryConfig.Mirrors}}' | grep -q mirror.gcr.io

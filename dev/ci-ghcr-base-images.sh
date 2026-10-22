#!/usr/bin/env bash
# CI only. Loads the container-test base images from their GHCR mirrors and tags them
# with the Public ECR names the test Dockerfiles use, so `docker build` finds them
# locally and never pulls from Public ECR, which throttles GitHub's shared runners.
# Local runs are unchanged: the Dockerfiles still pull from Public ECR.
#
# Refreshing a mirror (an admin with packages: write on ghcr.io/malbeclabs/ci-base):
#   docker buildx imagetools create --tag ghcr.io/malbeclabs/ci-base/<name>:<tag> public.ecr.aws/<source>:<tag>
# then update the digest below. imagetools keeps the source digest; docker pull/push does not.
set -euo pipefail

: "${GHCR_TOKEN:?GHCR_TOKEN must hold a token with packages: read}"
echo "$GHCR_TOKEN" | docker login ghcr.io -u "${GITHUB_ACTOR:-ci}" --password-stdin

images=(
  "ghcr.io/malbeclabs/ci-base/golang@sha256:9ce231b28998b32e116927c61aa5c406183061d4f3f2d61b292c88e7f6bdf7ca public.ecr.aws/docker/library/golang:1.26.10-alpine"
  "ghcr.io/malbeclabs/ci-base/ubuntu@sha256:0890b02df9b3515958f1e181d15001b7015cee0e0b0eaad7f6dc6796b1de3171 public.ecr.aws/lts/ubuntu:22.04"
)
for pair in "${images[@]}"; do
  read -r mirror name <<<"$pair"
  docker pull --platform linux/amd64 "$mirror"
  docker tag "$mirror" "$name"
done
docker logout ghcr.io

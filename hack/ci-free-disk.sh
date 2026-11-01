#!/usr/bin/env bash
# Frees disk on a GitHub-hosted runner before a job builds the images and
# runs kind on it: the toolchains the runner image comes with and Changeport
# never uses, and the Docker images it preloads. Runners of one image have
# had as little as 14 GiB for Docker, too little for the images and a kind
# cluster, so kubeadm timed out.
# Usage: hack/ci-free-disk.sh (on a GitHub-hosted runner only)
set -euo pipefail

[[ ${GITHUB_ACTIONS:-} == true ]] || { echo "hack/ci-free-disk.sh deletes system directories; it runs on GitHub's runners only" >&2; exit 1; }
df -h / | tail -1 | awk '{ print "Free before: " $4 }'
sudo rm -rf \
  /usr/share/dotnet \
  /usr/local/lib/android \
  /opt/ghc \
  /usr/local/.ghcup \
  /opt/hostedtoolcache/CodeQL \
  /usr/local/share/boost \
  /usr/share/swift
docker image prune --all --force >/dev/null
df -h / | tail -1 | awk '{ print "Free after: " $4 }'

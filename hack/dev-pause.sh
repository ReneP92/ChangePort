#!/usr/bin/env bash
# Pauses the local setup, so that it takes no CPU while you do something
# else, or resumes it. Pausing freezes every container of the kind cluster,
# its load balancers and helpers in place, as docker pause does: it keeps
# their memory, and resumes in seconds with everything as it was, whereas
# stopping the nodes would start every pod again and lose the load
# balancers' addresses. Docker Desktop's Resource Saver only stops its VM
# once no container runs, so it frees nothing while the cluster runs.
# Usage: hack/dev-pause.sh pause|resume
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

action=${1:?usage: hack/dev-pause.sh pause|resume}

# Every container of the local setup that is running (state) or paused.
containers() {
  {
    docker ps -q --filter "status=$1" --filter "label=io.x-k8s.kind.cluster=$CLUSTER_NAME"
    docker ps -q --filter "status=$1" --filter "label=io.x-k8s.cloud-provider-kind.cluster=$CLUSTER_NAME"
    docker ps -q --filter "status=$1" --filter "name=^${CLOUD_PROVIDER_KIND_CONTAINER}$" --filter "name=^${HOST_GATEWAY_CONTAINER}$"
  } | sort -u
}

case $action in
  pause)
    running=$(containers running)
    [[ -n $running ]] || die "nothing of the local setup is running"
    # shellcheck disable=SC2086 # one ID per word
    docker pause $running >/dev/null
    log "Paused the local setup: $(wc -w <<<"$running" | tr -d ' ') containers. just dev-resume resumes it."
    ;;
  resume)
    paused=$(containers paused)
    [[ -n $paused ]] || die "nothing of the local setup is paused"
    # shellcheck disable=SC2086 # one ID per word
    docker unpause $paused >/dev/null
    ensure_kubectl
    # The nodes renew their leases first; until then they read as not ready.
    kubectl wait --for=condition=Ready nodes --all --timeout=2m >/dev/null
    log "Resumed the local setup: $(wc -w <<<"$paused" | tr -d ' ') containers"
    ;;
  *)
    die "usage: hack/dev-pause.sh pause|resume"
    ;;
esac

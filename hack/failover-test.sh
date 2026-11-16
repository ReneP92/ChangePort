#!/usr/bin/env bash
# Tests that a three-zone customer cluster keeps its clients working through
# the disruptions production clusters see, each under load: an idempotent
# acks=all producer and a consumer run throughout, and every message must
# arrive exactly once.
# - Zone outage: the worker of one broker's zone freezes (docker pause) for a
#   minute; clients carry on with the other two zones, and the cluster
#   recovers with all partitions in sync.
# - Node drain: a broker's eviction is denied by Strimzi Drain Cleaner, which
#   marks the pod for the Strimzi operator to restart instead.
# Usage: hack/failover-test.sh [customer-id] (default ha). The customer gets
# the dev-ha profile and is deprovisioned afterwards; KEEP=1 keeps it.
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

customer=${1:-ha}
namespace="customer-$customer"
work=$(mktemp -d)
paused=""
cleanup() {
  [[ -z "$paused" ]] || docker unpause "$paused" >/dev/null 2>&1 || true
  docker rm -f "$customer-producer" "$customer-consumer" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

ensure_kubectl
log "Provisioning customer $customer with the dev-ha profile"
provisioner provision -customer "$customer" -profile dev-ha -wait -timeout 15m
prepare_kafka_client "$customer" "$work"
bootstrap=$(sed -n 's/^bootstrap.servers=//p' "$work/client.properties")
# The clients write their results here; they run as the image's own user.
mkdir -m 777 "$work/out"
KAFKA_CLIENT+=(-v "$work/out:/out")
# shellcheck source=load.sh
source "$(dirname "${BASH_SOURCE[0]}")/load.sh"

log "Zone outage: freezing the zone of broker $customer-dual-role-1 under load"
start_load zone-outage
node=$(broker_node "$customer-dual-role-1")
zone=$(kubectl get node "$node" -o jsonpath='{.metadata.labels.topology\.kubernetes\.io/zone}')
docker pause "$node" >/dev/null
paused=$node
before=$(consumed zone-outage)
sleep 60
during=$(consumed zone-outage)
[[ $during -gt $((before + 1000)) ]] ||
  die "clients stalled while $zone was down: $((during - before)) messages consumed in 60 seconds"
log "Clients kept working while $zone was down: $((during - before)) messages consumed in 60 seconds"
docker unpause "$node" >/dev/null
paused=""
stop_load zone-outage
wait_recovered
log "The cluster recovered from the outage of $zone"

log "Node drain: evicting broker $customer-dual-role-2 under load"
start_load node-drain
pod="$customer-dual-role-2"
uid=$(kubectl -n "$namespace" get pod "$pod" -o jsonpath='{.metadata.uid}')
if kubectl create --raw "/api/v1/namespaces/$namespace/pods/$pod/eviction" -f - >/dev/null 2>"$work/eviction.log" <<JSON; then
{"apiVersion": "policy/v1", "kind": "Eviction", "metadata": {"name": "$pod", "namespace": "$namespace"}}
JSON
  die "the eviction of $pod was allowed; Drain Cleaner should deny it"
fi
[[ $(kubectl -n "$namespace" get pod "$pod" -o jsonpath='{.metadata.annotations.strimzi\.io/manual-rolling-update}') == true ]] ||
  die "Drain Cleaner denied the eviction of $pod but did not mark it for a rolling restart: $(cat "$work/eviction.log")"
log "Drain Cleaner denied the eviction and marked $pod for the Strimzi operator"
for _ in $(seq 100); do
  current=$(kubectl -n "$namespace" get pod "$pod" -o jsonpath='{.metadata.uid}' 2>/dev/null || true)
  [[ -n "$current" && "$current" != "$uid" ]] && break
  sleep 3
done
[[ -n "$current" && "$current" != "$uid" ]] || die "the Strimzi operator did not restart $pod"
wait_recovered
stop_load node-drain
log "The Strimzi operator restarted $pod without losing a message"

if [[ ${KEEP:-0} != 1 ]]; then
  log "Deprovisioning customer $customer"
  provisioner deprovision -customer "$customer" -wait -timeout 5m
fi
log "Failover test passed"

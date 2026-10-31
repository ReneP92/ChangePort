#!/usr/bin/env bash
# Tests alerting end to end on kind (docs/runbook.md, Alerting): the Watchdog
# reaches its receiver, and freezing a broker of a three-broker customer
# (docker pause of its worker node) pages, through vmalert and Alertmanager,
# the alert receiver that stands in for PagerDuty on kind; once the broker is
# back, the page resolves and the customer's alerts are quiet. The alerting
# pipeline runs on the control-plane node on kind, so freezing a worker never
# freezes it.
# Usage: hack/alerting-test.sh [customer-id] (default ha). The customer gets
# the dev-ha profile if it does not exist yet, and is deprovisioned
# afterwards; KEEP=1 keeps it.
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

customer=${1:-ha}
namespace="customer-$customer"
paused=""
cleanup() {
  [[ -z "$paused" ]] || docker unpause "$paused" >/dev/null 2>&1 || true
}
trap cleanup EXIT

ensure_kubectl
kubectl -n metrics rollout status deployment/alert-receiver --timeout=2m >/dev/null ||
  die "the alert receiver does not run: $(kubectl -n metrics get pods -l app.kubernetes.io/name=alert-receiver --no-headers)"
if ! kubectl get customer "$customer" >/dev/null 2>&1; then
  log "Provisioning customer $customer with the dev-ha profile"
  provisioner provision -customer "$customer" -profile dev-ha -wait -timeout 15m
fi
[[ $(kubectl get customer "$customer" -o jsonpath='{.spec.profile}') == dev-ha ]] || die "customer $customer does not have the dev-ha profile"
kubectl wait customer "$customer" --for=condition=Ready --timeout=10m >/dev/null

# The alerts of the alert receiver's log, as JSON lines; none while it does
# not run.
received() { kubectl -n metrics logs deploy/alert-receiver 2>/dev/null || true; }
# Waits up to $1 seconds until the receiver got an alert whose line holds
# every other argument, such as '"status": "firing"'.
wait_received() {
  local timeout=$1 line
  shift
  for _ in $(seq $((timeout / 5))); do
    line=$(received | tail -n +"$from")
    for want in "$@"; do
      line=$(grep -F -- "$want" <<<"$line" || true)
    done
    [[ -n $line ]] && return
    sleep 5
  done
  die "the alert receiver got no alert with $*; it got:"$'\n'"$(received | tail -n +"$from" | tail -20)"
}

log "The Watchdog reaches its receiver, and the alerts of customer $customer are quiet"
from=1
wait_received 120 '"receiver": "watchdog"' '"alertname": "Watchdog"' '"env": "kind"'
check_alerts "$customer" "" ""

broker="$customer-dual-role-0"
node=$(kubectl -n "$namespace" get pod "$broker" -o jsonpath='{.spec.nodeName}')
log "Freezing the node of broker $broker, $node, until the page arrives"
from=$(($(received | wc -l) + 1))
docker pause "$node" >/dev/null
paused=$node
# The alert waits 5 minutes, and vmalert and Alertmanager a little more.
wait_received 600 '"receiver": "page"' '"status": "firing"' '"alertname": "KafkaBrokersDown"' "\"tenant\": \"$customer\""
log "KafkaBrokersDown paged for customer $customer"

log "The broker is back: the page resolves, and the alerts are quiet"
docker unpause "$node" >/dev/null
paused=""
wait_received 600 '"receiver": "page"' '"status": "resolved"' '"alertname": "KafkaBrokersDown"' "\"tenant\": \"$customer\""
kubectl -n "$namespace" wait pods -l "strimzi.io/cluster=$customer,strimzi.io/broker-role=true" --for=condition=Ready --timeout=10m >/dev/null
check_alerts "$customer" "" ""

if [[ ${KEEP:-0} != 1 ]]; then
  log "Deprovisioning customer $customer"
  provisioner deprovision -customer "$customer" -wait -timeout 5m
fi
log "Alerting test passed: freezing a broker of $customer paged, and the page resolved"

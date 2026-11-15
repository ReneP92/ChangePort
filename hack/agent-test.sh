#!/usr/bin/env bash
# Tests the Kafka agent on a customer's cluster on kind: of four topics
# created through the Admin API, as a customer would, the agent turns on
# tiered storage for the plain one within a few reconcile intervals, and for
# the one kept an hour, shorter than the day the brokers keep on disk, with
# an hour's local retention, which Kafka requires; it leaves the compacted
# one alone (Kafka cannot tier it) and keeps the customer's explicit
# remote.storage.enable=false; its metrics reach the metrics store with the
# tenant.
# Usage: hack/agent-test.sh [customer-id] (default demo)
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

customer=${1:-demo}
run_id="$(date +%s)-$RANDOM"
plain="agent-plain-$run_id"
hourly="agent-hourly-$run_id"
compacted="agent-compacted-$run_id"
opted_out="agent-opted-out-$run_id"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

ensure_kubectl
prepare_kafka_client "$customer" "$work"
bootstrap=$(sed -n 's/^bootstrap.servers=//p' "$work/client.properties")
kafka() { docker run "${KAFKA_CLIENT[@]}" --entrypoint "/opt/kafka/bin/$1" "$KAFKA_IMAGE" "${@:2}"; }
topics() { kafka kafka-topics.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties "$@"; }
# Prints a config of the topic if it is set on the topic itself.
# Usage: topic_config <topic> <config>
topic_config() {
  kafka kafka-configs.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties \
    --describe --entity-type topics --entity-name "$1" 2>/dev/null |
    awk -v key="$2" '{ split($1, kv, "=") } kv[1] == key { print kv[2] }'
}
remote_storage() { topic_config "$1" remote.storage.enable; }

log "Agent test for customer $customer: creating a plain, an hourly, a compacted and an opted-out topic"
topics --create --topic "$plain" --partitions 1 >/dev/null
topics --create --topic "$hourly" --partitions 1 --config retention.ms=3600000 >/dev/null
topics --create --topic "$compacted" --partitions 1 --config cleanup.policy=compact >/dev/null
topics --create --topic "$opted_out" --partitions 1 --config remote.storage.enable=false >/dev/null

for _ in $(seq 24); do
  [[ $(remote_storage "$plain") == true ]] && break
  sleep 5
done
[[ $(remote_storage "$plain") == true ]] || die "the agent did not turn on tiered storage for $plain within 2 minutes"
log "The agent turned on tiered storage for $plain"
for _ in $(seq 12); do
  [[ $(remote_storage "$hourly") == true ]] && break
  sleep 5
done
[[ $(remote_storage "$hourly") == true ]] || die "the agent did not turn on tiered storage for $hourly, kept an hour"
[[ $(topic_config "$hourly" local.retention.ms) == 3600000 ]] ||
  die "the agent tiered $hourly without an hour's local retention: $(topic_config "$hourly" local.retention.ms)"
log "The agent turned on tiered storage for $hourly, keeping an hour on the brokers"
[[ -z $(remote_storage "$compacted") ]] || die "the agent changed the compacted topic $compacted"
[[ $(remote_storage "$opted_out") == false ]] || die "the agent overrode the customer's remote.storage.enable=false on $opted_out"
log "The agent left the compacted topic and the customer's opt-out alone"

for _ in $(seq 24); do
  enabled=$(metrics_query "sum(changeport_agent_tiering_enabled_total{tenant=\"$customer\"})")
  [[ ${enabled%.*} -ge 1 ]] && break
  sleep 5
done
[[ ${enabled%.*} -ge 1 ]] || die "the metrics store has no tiering by the agent of customer $customer"
[[ -z $(metrics_query 'count(changeport_agent_topics{tenant=""})') ]] || die "agent metrics without a tenant"

topics --delete --topic "$plain,$hourly,$compacted,$opted_out" >/dev/null 2>&1 ||
  for topic in "$plain" "$hourly" "$compacted" "$opted_out"; do topics --delete --topic "$topic" >/dev/null; done
log "Agent test passed: topics tiered so far by the agent of customer $customer: $enabled"

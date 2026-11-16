#!/usr/bin/env bash
# Tests usage (docs/console.md): bytes produced to a topic and consumed from
# it once show up in the metrics as the console reads them
# (internal/metrics, UsageIn and UsageOut), each byte once. On a cluster of
# three brokers the followers fetch every byte again to replicate it, which
# must not count. Then the console's own usage queries (internal/metrics) run
# on counters imported into the metrics store with its samples missing
# around midnight, as while it restarts, which must count what each grew by
# rather than its whole value.
# Usage: hack/usage-test.sh [customer-id] (default demo)
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

customer=${1:-demo}
topic="usage-$(date +%s)-$RANDOM"
messages=2000
work=$(mktemp -d)
forward=""
cleanup() {
  [[ -z $forward ]] || kill "$forward" 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT

ensure_kubectl
prepare_kafka_client "$customer" "$work"
bootstrap=$(sed -n 's/^bootstrap.servers=//p' "$work/client.properties")
kafka() { docker run "${KAFKA_CLIENT[@]}" -i --entrypoint "/opt/kafka/bin/$1" "$KAFKA_IMAGE" "${@:2}"; }
replicas=$(kubectl -n "customer-$customer" get kafka "$customer" -o jsonpath='{.spec.kafka.config.default\.replication\.factor}')

log "Usage test for customer $customer, whose topics keep $replicas copies: $messages messages of 1,000 bytes to $topic"
kafka kafka-topics.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties --create \
  --topic "$topic" --partitions 1 >/dev/null
seq -f '%08g' 1 "$messages" | awk '{ printf "%s-%0991d\n", $0, 0 }' |
  kafka kafka-console-producer.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties --topic "$topic"
consumed=$(kafka kafka-console-consumer.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties \
  --topic "$topic" --group "$topic" --from-beginning --max-messages "$messages" --timeout-ms 60000 2>/dev/null | grep -c .)
[[ $consumed == "$messages" ]] || die "consumed $consumed of $messages messages"

# The console's usage queries, for this topic.
usage() {
  metrics_query "sum(increase_pure(kafka_server_brokertopicmetrics_bytes${1}persec_total{tenant=\"$customer\", topic=\"$topic\"}[30m]))"
}
payload=$((messages * 1000))
bytes_in="" bytes_out=""
# The brokers' metrics are scraped every 30 seconds.
for _ in $(seq 36); do
  bytes_in=$(usage in) bytes_out=$(usage out)
  [[ -n $bytes_in && -n $bytes_out ]] && ((${bytes_in%.*} >= payload && ${bytes_out%.*} >= payload)) && break
  sleep 5
done
bytes_in=${bytes_in%.*} bytes_out=${bytes_out%.*}
log "Usage shows ${bytes_in:-nothing} bytes in and ${bytes_out:-nothing} bytes out for $payload bytes of messages"
# Kafka's record batches add a few bytes per message; a second copy would
# double it.
for direction in in out; do
  value=$bytes_in
  [[ $direction == out ]] && value=$bytes_out
  [[ -n $value ]] || die "usage shows no bytes $direction for $topic"
  ((value >= payload && value <= payload * 11 / 10)) ||
    die "usage shows $value bytes $direction for $payload bytes produced and consumed once, want each byte once"
done

kafka kafka-consumer-groups.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties \
  --delete --group "$topic" >/dev/null 2>&1 || true
kafka kafka-topics.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties --delete --topic "$topic"

log "The console's usage queries on counters with the metrics store's samples missing around midnight"
kubectl -n metrics port-forward svc/victoria-metrics :8428 >"$work/forward" 2>&1 &
forward=$!
port=""
for _ in $(seq 30); do
  port=$(sed -nE 's/^Forwarding from 127\.0\.0\.1:([0-9]+) .*/\1/p' "$work/forward")
  [[ -n $port ]] && break
  sleep 1
done
[[ -n $port ]] || die "could not reach the metrics store: $(cat "$work/forward")"
CHANGEPORT_VICTORIA_METRICS_URL="http://127.0.0.1:$port" go -C "$ROOT" test -count=1 -run TestUsageAgainstVictoriaMetrics ./internal/metrics/ ||
  die "the console's usage queries counted a counter's gap wrongly"
log "Usage test passed for customer $customer: each byte counted once, of $replicas copies"

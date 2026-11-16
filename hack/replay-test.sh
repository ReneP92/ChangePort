#!/usr/bin/env bash
# Tests that replaying history from tiered storage leaves live consumers
# alone (docs/console.md): on a topic whose older messages are only in the
# object store, a consumer reading it from the start is held to the brokers'
# limit for reading tiered storage back
# (remote.log.manager.fetch.max.bytes.per.second), while a consumer of
# another topic keeps up with its producer throughout.
# Usage: hack/replay-test.sh [customer-id] (default demo)
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

customer=${1:-demo}
run="$(date +%s)-$RANDOM"
history="replay-history-$run"
live="replay-live-$run"
# About 40 MiB of history: well over 10 seconds at the dev profiles' limit.
messages=40000
live_rate=100
work=$(mktemp -d)
cleanup() {
  docker rm -f "replay-producer-$run" "replay-consumer-$run" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

ensure_kubectl
prepare_kafka_client "$customer" "$work"
bootstrap=$(sed -n 's/^bootstrap.servers=//p' "$work/client.properties")
mkdir -m 777 "$work/out"
KAFKA_CLIENT+=(-v "$work/out:/out")
kafka() { docker run "${KAFKA_CLIENT[@]}" -i --entrypoint "/opt/kafka/bin/$1" "$KAFKA_IMAGE" "${@:2}"; }
offset() {
  kafka kafka-get-offsets.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties \
    --topic "$history" --time "$1" 2>/dev/null | cut -d: -f3
}
lines() { grep -c . "$work/out/$1" 2>/dev/null || true; }

limit=$(kubectl -n "customer-$customer" get kafka "$customer" \
  -o jsonpath='{.spec.kafka.config.remote\.log\.manager\.fetch\.max\.bytes\.per\.second}')
[[ $limit -gt 0 ]] || die "customer $customer's brokers have no limit for reading tiered storage back"
log "Replay test for customer $customer: the brokers read tiered storage back at most $limit bytes a second"

log "Producing $messages messages of about 1 KiB to $history, and waiting for them to be tiered"
kafka kafka-topics.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties --create \
  --topic "$history" --partitions 1 --config remote.storage.enable=true --config segment.bytes=1048576 \
  --config local.retention.ms=1000 >/dev/null
seq -f '%08g' 1 "$messages" | awk '{ printf "%s-%01000d\n", $0, 0 }' |
  kafka kafka-console-producer.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties --topic "$history"
# Segments are copied every 30 seconds, and local copies deleted on the
# retention check, every 5 minutes.
local_start=""
for _ in $(seq 100); do
  local_start=$(offset earliest-local)
  [[ -n "$local_start" && $local_start -gt $((messages / 2)) ]] && break
  sleep 5
done
[[ -n "$local_start" && $local_start -gt $((messages / 2)) ]] ||
  die "the brokers still keep $history from offset ${local_start:-unknown} locally, want most of it only in the object store"
tiered_bytes=$((local_start * 1010))
log "Offsets 0 to $local_start, about $((tiered_bytes / 1048576)) MiB, are only in the object store"

log "Starting a live producer and consumer on $live, $live_rate messages a second"
kafka kafka-topics.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties --create \
  --topic "$live" --partitions 1 >/dev/null
docker run -d --name "replay-producer-$run" "${KAFKA_CLIENT[@]}" --entrypoint /opt/kafka/bin/kafka-producer-perf-test.sh \
  "$KAFKA_IMAGE" --topic "$live" --num-records $((live_rate * 900)) --throughput "$live_rate" --record-size 100 \
  --producer.config /config/client.properties --producer-props "bootstrap.servers=$bootstrap" >/dev/null
# shellcheck disable=SC2016 # expanded by the container's shell
docker run -d --name "replay-consumer-$run" "${KAFKA_CLIENT[@]}" -e TOPIC="$live" -e BOOTSTRAP="$bootstrap" \
  --entrypoint bash "$KAFKA_IMAGE" -c '
    exec /opt/kafka/bin/kafka-console-consumer.sh --bootstrap-server "$BOOTSTRAP" --topic "$TOPIC" \
      --command-config /config/client.properties --group "$TOPIC" >"/out/live" 2>"/out/live.log"' >/dev/null
for _ in $(seq 60); do
  [[ $(lines live) -gt $((live_rate * 5)) ]] && break
  sleep 1
done
[[ $(lines live) -gt $((live_rate * 5)) ]] || die "the live consumer did not start; it said:"$'\n'"$(tail -20 "$work/out/live.log")"

log "Replaying $history from the start"
live_before=$(lines live)
started=$(date +%s)
kafka kafka-console-consumer.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties \
  --topic "$history" --group "$history-replay" --from-beginning --max-messages "$messages" --timeout-ms 300000 \
  >"$work/out/replayed" 2>"$work/out/replay.log" || true
elapsed=$(($(date +%s) - started))
live_after=$(lines live)

replayed=$(lines replayed)
[[ $replayed == "$messages" ]] || die "the replay read $replayed of $messages messages; it said:"$'\n'"$(tail -20 "$work/out/replay.log")"
# At the limit the tiered part alone takes tiered_bytes / limit seconds;
# allow a quarter off for the limit's averaging window.
least=$((tiered_bytes * 3 / 4 / limit))
((elapsed >= least)) ||
  die "the replay read $((tiered_bytes / 1048576)) MiB from tiered storage in ${elapsed}s, faster than the limit allows (at least ${least}s)"
live_rate_seen=$(((live_after - live_before) / elapsed))
((live_rate_seen * 10 >= live_rate * 9)) ||
  die "the live consumer read $live_rate_seen messages a second during the replay, behind its producer's $live_rate"
log "The replay took ${elapsed}s, held to the limit (at least ${least}s); the live consumer kept up at $live_rate_seen messages a second"

# The consumer leaves its group as it stops, so the group can go.
docker stop "replay-consumer-$run" >/dev/null 2>&1 || true
docker rm -f "replay-producer-$run" "replay-consumer-$run" >/dev/null 2>&1 || true
kafka kafka-consumer-groups.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties \
  --delete --group "$history-replay" >/dev/null 2>&1 || true
kafka kafka-consumer-groups.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties \
  --delete --group "$live" >/dev/null 2>&1 || true
kafka kafka-topics.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties --delete --topic "$history"
kafka kafka-topics.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties --delete --topic "$live"
log "Replay test passed for customer $customer"

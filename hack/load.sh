# shellcheck shell=bash
# The scripts that source this set customer, namespace, work and bootstrap.
# shellcheck disable=SC2154
# Load and recovery checks for the tests that disrupt a three-broker
# customer's cluster (hack/failover-test.sh, hack/chaos-test.sh): a producer
# and a consumer under load, which must see every message arrive exactly
# once, and waiting until the cluster has recovered. Source it after lib.sh
# with customer, namespace, work (with an out directory the clients write
# to, mounted as /out in KAFKA_CLIENT) and bootstrap set.

kafka() { docker run "${KAFKA_CLIENT[@]}" --entrypoint "/opt/kafka/bin/$1" "$KAFKA_IMAGE" "${@:2}"; }
consumed() { grep -c . "$work/out/$1-consumed" 2>/dev/null || true; }

# Creates topic and starts the load: a producer sending 1, 2, 3, ... until
# the file stop-<topic> appears, and a consumer reading them back. The
# console producer only retries 3 times by default; the producer here retries
# until delivery.timeout.ms like any Kafka client with default settings.
start_load() {
  local topic=$1
  kafka kafka-topics.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties \
    --create --topic "$topic" --partitions 3 >/dev/null
  # shellcheck disable=SC2016 # expanded by the container's shell
  docker run -d --name "$customer-producer" "${KAFKA_CLIENT[@]}" -e TOPIC="$topic" -e BOOTSTRAP="$bootstrap" \
    --entrypoint bash "$KAFKA_IMAGE" -c '
      { i=0
        while [ ! -e "/out/stop-$TOPIC" ]; do i=$((i + 1)); echo "$i"; sleep 0.02; done
        echo "$i" >"/out/$TOPIC-produced"
      } | /opt/kafka/bin/kafka-console-producer.sh --bootstrap-server "$BOOTSTRAP" --topic "$TOPIC" \
        --command-config /config/client.properties --producer-property acks=all \
        --producer-property enable.idempotence=true --producer-property retries=2147483647 \
        2>"/out/$TOPIC-producer.log"
      echo $? >"/out/$TOPIC-producer-exit"' >/dev/null
  # The consumer replaces the shell, so that docker stop's SIGTERM reaches
  # it, and it closes, committing what it consumed, as stop_load wants.
  # shellcheck disable=SC2016 # expanded by the container's shell
  docker run -d --name "$customer-consumer" "${KAFKA_CLIENT[@]}" -e TOPIC="$topic" -e BOOTSTRAP="$bootstrap" \
    --entrypoint bash "$KAFKA_IMAGE" -c '
      exec /opt/kafka/bin/kafka-console-consumer.sh --bootstrap-server "$BOOTSTRAP" --topic "$TOPIC" \
        --command-config /config/client.properties --group "$TOPIC-check" --from-beginning \
        >"/out/$TOPIC-consumed" 2>"/out/$TOPIC-consumer.log"' >/dev/null
  for _ in $(seq 60); do
    [[ $(consumed "$topic") -gt 100 ]] && return
    sleep 1
  done
  die "the load on $topic did not start; the producer and the consumer said:"$'\n'"$(tail -20 "$work/out/$topic-producer.log" "$work/out/$topic-consumer.log" 2>&1)"
}

# The consumer's group on topic, with the offsets it committed.
group_offsets() {
  kafka kafka-consumer-groups.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties \
    --describe --group "$1-check" 2>&1
}

# Whether the group described on stdin committed all of topic.
# Columns: group, topic, partition, current offset, log end offset, lag.
committed_all() {
  awk -v group="$1-check" '$1 == group { rows++; if ($6 != "0") behind++ } END { exit !(rows && !behind) }'
}

# Stops the producer, waits until the consumer has read everything produced,
# and checks that every message from 1 to the last arrived exactly once. The
# consumer must leave its group with nothing to consume: with its last
# messages uncommitted, ConsumerGroupLagHigh would rightly fire for the group
# 10 minutes later, while the alerting test wants quiet alerts. So it stops
# only once its group has committed everything, which its auto-commit does
# every 5 seconds: a disruption can leave it reading from the partitions'
# new leaders while its group is still moving to a new coordinator, and its
# last commit, as it closes, is refused until the group has.
stop_load() {
  local topic=$1 produced exit_code
  touch "$work/out/stop-$topic"
  for _ in $(seq 120); do
    [[ -s "$work/out/$topic-producer-exit" ]] && break
    sleep 1
  done
  exit_code=$(cat "$work/out/$topic-producer-exit" 2>/dev/null) || die "the producer on $topic did not finish"
  produced=$(cat "$work/out/$topic-produced")
  if [[ $exit_code != 0 ]] || grep -E "ERROR|Error when sending" "$work/out/$topic-producer.log" >/dev/null; then
    cat "$work/out/$topic-producer.log" >&2
    die "the producer on $topic failed"
  fi
  for _ in $(seq 120); do
    [[ $(consumed "$topic") -ge $produced ]] && break
    sleep 1
  done
  for _ in $(seq 60); do
    group_offsets "$topic" | committed_all "$topic" && break
    sleep 2
  done
  # The producer has exited and removed itself by now.
  docker stop "$customer-consumer" >/dev/null 2>&1 ||
    die "the consumer on $topic stopped early; it said:"$'\n'"$(tail -20 "$work/out/$topic-consumer.log")"
  docker rm -f "$customer-producer" "$customer-consumer" >/dev/null 2>&1 || true
  local lines unique
  lines=$(consumed "$topic")
  unique=$(sort -un "$work/out/$topic-consumed" | grep -c .)
  if [[ $lines != "$produced" || $unique != "$produced" ]] ||
    [[ $(sort -n "$work/out/$topic-consumed" | head -1) != 1 || $(sort -n "$work/out/$topic-consumed" | tail -1) != "$produced" ]]; then
    tail -20 "$work/out/$topic-consumer.log" >&2
    die "on $topic, $produced messages were produced, but $lines were consumed ($unique distinct)"
  fi
  local offsets
  offsets=$(group_offsets "$topic") || die "could not describe consumer group $topic-check:"$'\n'"$offsets"
  committed_all "$topic" <<<"$offsets" ||
    die "consumer group $topic-check stopped without committing all it consumed on $topic:"$'\n'"$offsets"
  log "All $produced messages on $topic arrived exactly once"
}

# Waits until the Kafka cluster is ready and every partition is in sync.
wait_recovered() {
  kubectl -n "$namespace" wait kafka "$customer" --for=condition=Ready --timeout=10m >/dev/null
  kubectl -n "$namespace" wait pods -l "strimzi.io/cluster=$customer,strimzi.io/broker-role=true" --for=condition=Ready --timeout=10m >/dev/null
  for _ in $(seq 60); do
    [[ -z $(kafka kafka-topics.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties \
      --describe --under-replicated-partitions 2>/dev/null) ]] && return
    sleep 5
  done
  die "partitions of customer $customer are still under-replicated"
}

broker_node() { kubectl -n "$namespace" get pod "$1" -o jsonpath='{.spec.nodeName}'; }

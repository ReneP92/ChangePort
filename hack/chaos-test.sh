#!/usr/bin/env bash
# Tests that a three-broker customer's cluster, and the platform around it,
# get through the disruptions production sees beyond a zone outage and a
# node drain (hack/failover-test.sh), each under load, with every message
# arriving exactly once (hack/load.sh):
# - the active KRaft controller crashes, and another takes over without
#   KafkaNoActiveController firing;
# - one broker loses the network to the others, while clients still reach
#   it: the controllers fence it and its partitions get new leaders, which
#   the console's health and KafkaBrokersDown must show, but it keeps the
#   leadership it knew, so clients that keep asking it can stall on those
#   partitions until their metadata is refreshed (up to
#   metadata.max.age.ms), which Kafka cannot help. The broker is cut off for
#   90 seconds, under the producers' delivery.timeout.ms of 2 minutes;
#   once it is back every message arrives exactly once (docs/runbook.md, A
#   broker is cut off from the others);
# - the Kafka agent is down while a topic is created, which it tiers once
#   it is back;
# - the provisioning controller restarts while it creates a customer and
#   while it deletes one, which both finish;
# - the Strimzi operator restarts during a rolling restart of the brokers,
#   which it finishes;
# - the object store of tiered storage is down, which raises
#   TieredStorageCopyErrors while clients work on, and the brokers catch up
#   once it is back.
# The alerts each should raise must match (vmalert's ALERTS series, as
# pending or firing, however long they must hold), and once each disruption
# of the customer's cluster is over its alerts are quiet again
# (check_alerts), as docs/runbook.md says (Chaos tests). A full volume is not
# tested here: kind's volumes enforce no size and report no stats, so it
# waits for the EKS dev environment.
# Usage: hack/chaos-test.sh [customer-id] (default chaos). The customer gets
# the dev-ha profile if it does not exist yet, and is deprovisioned
# afterwards; KEEP=1 keeps it.
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

customer=${1:-chaos}
namespace="customer-$customer"
run="$(date +%s)-$RANDOM"
work=$(mktemp -d)
# What to put back if the test stops halfway: network rules, the object
# store, the Kafka agent, the second customer, the topics of this run.
partitioned=""
partition_rules=()
store_down=""
agent_down=""
other=""
bootstrap=""
cleanup() {
  for rule in "${partition_rules[@]}"; do
    # shellcheck disable=SC2086 # the rule's words are iptables' arguments
    docker exec "$partitioned" iptables -w -D FORWARD $rule 2>/dev/null || true
  done
  [[ -z $store_down ]] || kubectl -n object-store scale deployment/seaweedfs --replicas=1 >/dev/null 2>&1 || true
  [[ -z $agent_down ]] || kubectl -n "$namespace" scale deployment/changeport-agent --replicas=1 >/dev/null 2>&1 || true
  docker rm -f "$customer-producer" "$customer-consumer" "chaos-tiered-$run" >/dev/null 2>&1 || true
  [[ -z $other ]] || provisioner deprovision -customer "$other" >/dev/null 2>&1 || true
  # A failed run's topics would leave the customer's alerts firing for the
  # next run.
  if [[ -n $bootstrap ]]; then
    kafka kafka-topics.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties \
      --delete --topic ".*-$run.*" >/dev/null 2>&1 || true
  fi
  rm -rf "$work"
}
trap cleanup EXIT

ensure_kubectl
if ! kubectl get customer "$customer" >/dev/null 2>&1; then
  log "Provisioning customer $customer with the dev-ha profile"
  provisioner provision -customer "$customer" -profile dev-ha -wait -timeout 15m
fi
[[ $(kubectl get customer "$customer" -o jsonpath='{.spec.profile}') == dev-ha ]] || die "customer $customer does not have the dev-ha profile"
kubectl wait customer "$customer" --for=condition=Ready --timeout=10m >/dev/null
prepare_kafka_client "$customer" "$work"
bootstrap=$(sed -n 's/^bootstrap.servers=//p' "$work/client.properties")
mkdir -m 777 "$work/out"
KAFKA_CLIENT+=(-v "$work/out:/out")
# shellcheck source=load.sh
source "$(dirname "${BASH_SOURCE[0]}")/load.sh"

# Whether vmalert had alert $1 pending or firing for the customer in the last
# $2 seconds: its rule matched, however long it must hold.
alert_matched() {
  [[ -n $(metrics_query "count(max_over_time(ALERTS{alertname=\"$1\", tenant=\"$customer\"}[${2}s]))" || true) ]]
}

# Waits up to $2 seconds until alert $1 matches for the customer.
wait_alert() {
  local alert=$1 timeout=$2
  for _ in $(seq $((timeout / 10))); do
    alert_matched "$alert" 30 && log "$alert matches for customer $customer" && return
    sleep 10
  done
  die "$alert did not match for customer $customer within $timeout seconds"
}

# Fails if alert $1 fired for the customer in the last $2 seconds.
never_fired() {
  local alert=$1 window=$2 fired
  fired=$(metrics_query "count(max_over_time(ALERTS{alertname=\"$alert\", alertstate=\"firing\", tenant=\"$customer\"}[${window}s]))" || true)
  [[ -z $fired ]] || die "$alert fired for customer $customer"
}

# Whether the console's cluster health queries say what $1 says, such as
# active_brokers=2,fenced_brokers=1 (internal/metrics).
console_says() {
  metrics_port_forward
  local ok=0
  CHANGEPORT_METRICS_URL="http://127.0.0.1:$METRICS_PORT" CHANGEPORT_METRICS_TENANT="$customer" CHANGEPORT_HEALTH_EXPECT="$1" \
    go -C "$ROOT" test -count=1 -run TestClusterHealthAgainstVictoriaMetrics ./internal/metrics/ >"$work/health" 2>&1 || ok=$?
  kill "$METRICS_FORWARD" 2>/dev/null || true
  return "$ok"
}

# The node ID of the active KRaft controller, or nothing while it cannot be
# asked, as during an election.
controller() {
  kafka kafka-metadata-quorum.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties \
    describe --status 2>/dev/null | sed -nE 's/^LeaderId:[[:space:]]+([0-9]+).*/\1/p' || true
}

# Clients keep working: more than $2 messages consumed of load $1 over $3 seconds.
keeps_working() {
  local load=$1 least=$2 seconds=$3 before after
  before=$(consumed "$load")
  sleep "$seconds"
  after=$(consumed "$load")
  ((after - before > least)) || die "clients stalled: $((after - before)) messages of $load consumed in $seconds seconds"
  log "Clients kept working: $((after - before)) messages consumed in $seconds seconds"
}

pod_uids() {
  kubectl -n "$namespace" get pods -l "strimzi.io/cluster=$customer,strimzi.io/broker-role=true" \
    -o jsonpath='{range .items[*]}{.metadata.uid}{"\n"}{end}' | sort
}

log "The active controller crashes under load"
start_load "controller-$run"
leader=$(controller)
[[ -n $leader ]] || die "could not find the active controller of customer $customer"
started=$(date +%s)
kubectl -n "$namespace" delete pod "$customer-dual-role-$leader" --grace-period=0 --force >/dev/null 2>&1
new=""
for _ in $(seq 30); do
  new=$(controller)
  [[ -n $new && $new != "$leader" ]] && break
  sleep 2
done
[[ -n $new && $new != "$leader" ]] || die "no other controller took over from node $leader"
log "Node $new took over from node $leader as the active controller"
keeps_working "controller-$run" 500 30
wait_recovered
stop_load "controller-$run"
never_fired KafkaNoActiveController $(($(date +%s) - started + 60))
check_alerts "$customer" "" ""

log "A broker loses the network to the other brokers under load"
start_load "partition-$run"
broker="$customer-dual-role-1"
partitioned=$(broker_node "$broker")
ip=$(kubectl -n "$namespace" get pod "$broker" -o jsonpath='{.status.podIP}')
for peer in $(kubectl -n "$namespace" get pods -l "strimzi.io/cluster=$customer,strimzi.io/broker-role=true" \
  -o jsonpath='{range .items[*]}{.status.podIP}{"\n"}{end}' | grep -vx "$ip"); do
  # Both directions pass the broker's node, between its pod and the network.
  partition_rules+=("-s $ip -d $peer -j DROP" "-s $peer -d $ip -j DROP")
done
for rule in "${partition_rules[@]}"; do
  # shellcheck disable=SC2086 # the rule's words are iptables' arguments
  docker exec "$partitioned" iptables -w -I FORWARD 1 $rule
done
cut_off=$(date +%s)
before=$(consumed "partition-$run")
# The controllers fence it once its heartbeats stop, and the console's
# health must say so from the active controller's counts, not the stale
# ones the cut-off broker reports.
told=""
while (($(date +%s) - cut_off < 80)); do
  if console_says active_brokers=2,fenced_brokers=1; then
    told=1
    break
  fi
  sleep 5
done
[[ -n $told ]] || die "the console did not count the cut-off broker as fenced: $(cat "$work/health")"
log "The console counts 2 active brokers and 1 fenced"
sleep $((cut_off + 90 - $(date +%s) > 0 ? cut_off + 90 - $(date +%s) : 0))
log "While the broker was cut off, clients consumed $(($(consumed "partition-$run") - before)) messages in $(($(date +%s) - cut_off)) seconds"
for rule in "${partition_rules[@]}"; do
  # shellcheck disable=SC2086 # the rule's words are iptables' arguments
  docker exec "$partitioned" iptables -w -D FORWARD $rule
done
partition_rules=()
partitioned=""
# vmalert evaluates every 30 seconds, after the metrics are scraped.
matched=""
for _ in $(seq 9); do
  alert_matched KafkaBrokersDown 180 && matched=1 && break
  sleep 10
done
[[ -n $matched ]] || die "KafkaBrokersDown did not match for customer $customer while its broker was cut off"
log "KafkaBrokersDown matched for customer $customer while its broker was cut off"
wait_recovered
stop_load "partition-$run"
check_alerts "$customer" "" ""
log "The broker caught up once the network was back"

log "The Kafka agent is down while a topic is created"
topic="agent-$run"
agent_down=1
kubectl -n "$namespace" scale deployment/changeport-agent --replicas=0 >/dev/null
kubectl -n "$namespace" wait pods -l app.kubernetes.io/name=changeport-agent --for=delete --timeout=2m >/dev/null
kafka kafka-topics.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties \
  --create --topic "$topic" --partitions 1 >/dev/null
kubectl -n "$namespace" scale deployment/changeport-agent --replicas=1 >/dev/null
agent_down=""
tiered=""
for _ in $(seq 30); do
  tiered=$(kafka kafka-configs.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties \
    --describe --entity-type topics --entity-name "$topic" 2>/dev/null | grep -o 'remote.storage.enable=true' || true)
  [[ -n $tiered ]] && break
  sleep 5
done
[[ -n $tiered ]] || die "the Kafka agent did not tier $topic once it was back"
kafka kafka-topics.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties --delete --topic "$topic"
check_alerts "$customer" "" ""
log "The Kafka agent tiered $topic once it was back"

log "The provisioning controller restarts while it creates a customer and while it deletes one"
restart_provisioner() {
  kubectl -n changeport delete pod -l app.kubernetes.io/name=changeport-provisioner --wait=false >/dev/null
  kubectl -n changeport rollout status deployment/changeport-provisioner --timeout=3m >/dev/null
}
other="chaos-$((RANDOM % 100000))"
provisioner provision -customer "$other" -profile dev
# Mid-create: once the template's Kafka cluster is applied, before it is ready.
creating=""
for _ in $(seq 60); do
  kubectl -n "customer-$other" get kafka "$other" >/dev/null 2>&1 && creating=1 && break
  sleep 2
done
[[ -n $creating ]] || die "the provisioning controller did not start creating customer $other"
restart_provisioner
kubectl wait customer "$other" --for=condition=Ready --timeout=15m >/dev/null ||
  die "customer $other did not get ready after the provisioning controller restarted"
log "Customer $other got ready"
provisioner deprovision -customer "$other"
# Mid-delete: once its namespace is being deleted, before it is gone.
deleting=""
for _ in $(seq 60); do
  [[ $(kubectl get namespace "customer-$other" -o jsonpath='{.status.phase}' 2>/dev/null || true) == Terminating ]] && deleting=1 && break
  sleep 2
done
[[ -n $deleting ]] || die "the provisioning controller did not start deleting customer $other"
restart_provisioner
kubectl wait customer "$other" --for=delete --timeout=10m >/dev/null 2>&1 ||
  die "customer $other was not deleted after the provisioning controller restarted"
log "Customer $other was deleted"
other=""

log "The Strimzi operator restarts during a rolling restart of the brokers, under load"
start_load "roll-$run"
before=$(pod_uids)
for pod in $(kubectl -n "$namespace" get pods -l "strimzi.io/cluster=$customer,strimzi.io/broker-role=true" -o name); do
  kubectl -n "$namespace" annotate "$pod" strimzi.io/manual-rolling-update=true >/dev/null
done
# The operator looks at the annotations on its next reconciliation.
for _ in $(seq 100); do
  [[ $(comm -12 <(echo "$before") <(pod_uids) | grep -c .) -lt 3 ]] && break
  sleep 3
done
[[ $(comm -12 <(echo "$before") <(pod_uids) | grep -c .) -lt 3 ]] || die "the Strimzi operator did not start the rolling restart"
kubectl -n strimzi delete pod -l name=strimzi-cluster-operator --wait=false >/dev/null
log "Restarted the Strimzi operator with the roll under way"
for _ in $(seq 200); do
  [[ -z $(comm -12 <(echo "$before") <(pod_uids)) ]] && break
  sleep 3
done
[[ -z $(comm -12 <(echo "$before") <(pod_uids)) ]] || die "the Strimzi operator did not finish restarting the brokers"
wait_recovered
stop_load "roll-$run"
check_alerts "$customer" "" ""
log "The Strimzi operator finished the rolling restart"

log "The object store of tiered storage is down, under load"
start_load "store-$run"
tiered_topic="store-$run-tiered"
kafka kafka-topics.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties --create \
  --topic "$tiered_topic" --partitions 1 --config remote.storage.enable=true --config segment.bytes=1048576 \
  --config local.retention.ms=1000 >/dev/null
store_down=1
kubectl -n object-store scale deployment/seaweedfs --replicas=0 >/dev/null
kubectl -n object-store wait pods -l app.kubernetes.io/name=seaweedfs --for=delete --timeout=2m >/dev/null
# Closed segments the brokers cannot copy, and must keep.
docker run --name "chaos-tiered-$run" "${KAFKA_CLIENT[@]}" --entrypoint /opt/kafka/bin/kafka-producer-perf-test.sh \
  "$KAFKA_IMAGE" --topic "$tiered_topic" --num-records 20000 --throughput 2000 --record-size 1000 \
  --producer.config /config/client.properties --producer-props "bootstrap.servers=$bootstrap" >/dev/null
wait_alert TieredStorageCopyErrors 300
keeps_working "store-$run" 500 30
kubectl -n object-store scale deployment/seaweedfs --replicas=1 >/dev/null
kubectl -n object-store rollout status deployment/seaweedfs --timeout=3m >/dev/null
store_down=""
lag=""
for _ in $(seq 60); do
  lag=$(metrics_query "sum(kafka_server_brokertopicmetrics_remotecopylagbytes{tenant=\"$customer\", topic=\"$tiered_topic\"})" || true)
  [[ ${lag%.*} == 0 ]] && break
  sleep 10
done
[[ ${lag%.*} == 0 ]] || die "the brokers did not catch up with tiered storage: ${lag:-no} bytes still to copy"
log "The brokers copied what they kept once the object store was back"
stop_load "store-$run"
kafka kafka-topics.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties --delete --topic "$tiered_topic"
# The copy errors' rate looks back 10 minutes.
check_alerts "$customer" "" "" 72

if [[ ${KEEP:-0} != 1 ]]; then
  log "Deprovisioning customer $customer"
  provisioner deprovision -customer "$customer" -wait -timeout 5m
  # Nothing of it is left to clean up.
  bootstrap=""
fi
log "Chaos test passed for customer $customer"

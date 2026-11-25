#!/usr/bin/env bash
# Tests the alerts of a Postgres CDC source that the customer tunes, and the
# one on a source that stops reading without failing (docs/alerts.md), end to
# end on kind, against the Postgres that stands in for a customer's
# database: the customer's thresholds, set as the console sets them, reach
# the metrics store within two minutes, and one turned off has none. The source's role
# then loses REPLICATION, which keeps its connector from reading the WAL
# while it retries without end, Connect calls it running and its phase stays
# Streaming: as the database is written to, its stall alerts fire, and so
# does its slot's share of max_slot_wal_keep_size, until the customer turns
# that one off. Given REPLICATION back, it reads again, and its alerts are
# quiet.
# Usage: hack/cdc-alerts-test.sh [customer-id] (default demo)
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

customer=${1:-demo}
namespace=customer-$customer
source_name="stall-$RANDOM"
id=$(random_string a-z0-9 6)
password=$(random_string A-Za-z0-9 32)
slot="changeport_${source_name//-/_}_$id"
role=$slot
tables=public.customers
console="system:serviceaccount:changeport:changeport-console"
work=$(mktemp -d)

ensure_kubectl
# Runs SQL on the stand-in database as its owner, as the customer runs ours.
pg() { kubectl -n source-postgres exec -i deploy/postgres -- psql -U postgres -d app -v ON_ERROR_STOP=1 -qtA "$@"; }
cdcsql() { go run ./cmd/cdcsql -source "$source_name" -id "$id" "$@"; }
source_field() { kubectl -n "$namespace" get postgressource "$source_name" -o jsonpath="{$1}" 2>/dev/null; }
slot_column() { pg -c "SELECT $1 FROM pg_replication_slots WHERE slot_name = '$slot'"; }
# Waits up to 10 minutes until the source reaches a phase.
wait_phase() {
  local phase
  for _ in $(seq 150); do
    phase=$(source_field .status.phase)
    [[ $phase == "$1" ]] && return
    sleep 4
  done
  die "source $source_name is in phase \"$phase\", not $1: $(source_field '.status.conditions[?(@.type=="Ready")].message')"
}
# Changes the customer's alert settings as the console does, as its service
# account: it replaces them, as read, with its change merged into their spec.
set_alerts() {
  kubectl -n "$namespace" get alertsettings alerts -o json | jq --argjson change "$1" '.spec *= $change' |
    kubectl replace --as "$console" -f - >/dev/null
}
# Waits up to two minutes until the customer's threshold of an alert is
# value, or, with value absent, until it has none, as the metrics store
# answers queries: within a minute, measured on kind, of a change.
expect_threshold() {
  local alert=$1 want=${2:-absent} got=""
  metrics_port_forward
  for _ in $(seq 24); do
    got=$(curl -s "http://127.0.0.1:$METRICS_PORT/api/v1/query" \
      --data-urlencode "query=changeport_alert_threshold{tenant=\"$customer\",alert=\"$alert\"}" |
      jq -r '.data.result[0].value[1] // "absent"')
    [[ $got == "$want" ]] && break
    sleep 5
  done
  kill "$METRICS_FORWARD" 2>/dev/null || true
  [[ $got == "$want" ]] || die "the threshold of $alert is $got two minutes after the change, not $want"
}
# Writes WAL the source's slot keeps, as the customer's applications would.
write_wal() { pg -c "SELECT pg_logical_emit_message(true, 'changeport-test', repeat('x', $1))" >/dev/null; }
cleanup() {
  # shellcheck disable=SC2181 # the exit status of the script, on EXIT
  (($? == 0)) || keep_source_diagnostics "$namespace" "$source_name"
  [[ -n ${WRITER:-} ]] && kill "$WRITER" 2>/dev/null
  # The stand-in database keeps a 1 GB limit, as the setup guide recommends.
  pg -c "ALTER SYSTEM SET max_slot_wal_keep_size = '1GB'" -c "SELECT pg_reload_conf()" >/dev/null 2>&1 || true
  pg -c "ALTER ROLE \"$role\" REPLICATION" >/dev/null 2>&1 || true
  [[ -z ${CREATED_SETTINGS:-} ]] || kubectl -n "$namespace" delete alertsettings alerts --ignore-not-found >/dev/null 2>&1 || true
  kubectl -n "$namespace" delete postgressource "$source_name" --ignore-not-found --timeout=3m >/dev/null 2>&1 || true
  cdcsql -step cleanup -tables "$tables" | pg >/dev/null 2>&1 || true
  pg -c "SELECT pg_drop_replication_slot('$slot') FROM pg_replication_slots WHERE slot_name = '$slot' AND NOT active" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

[[ -z $(kubectl -n "$namespace" get alertsettings alerts -o name 2>/dev/null) ]] ||
  die "customer $customer has alert settings already, which this test would replace"

log "Alerts test for customer $customer: the customer's source thresholds reach the rules"
kubectl create --as "$console" -f - >/dev/null <<SETTINGS
apiVersion: changeport.io/v1alpha1
kind: AlertSettings
metadata:
  name: alerts
  namespace: $namespace
spec: {}
SETTINGS
CREATED_SETTINGS=1
expect_threshold PostgresSourceBehind 900
expect_threshold PostgresSourceRetainedWALHigh 20000000000
expect_threshold PostgresSourceSlotFilling 0.5
set_alerts '{"sourceBehind":{"seconds":120},"sourceRetainedWAL":{"enabled":false},"sourceSlotFilling":{"percent":10}}'
expect_threshold PostgresSourceBehind 120
expect_threshold PostgresSourceRetainedWALHigh
expect_threshold PostgresSourceSlotFilling 0.1

log "Source $source_name streams, and its alerts are quiet"
cdcsql -step role -password "$password" | pg
cdcsql -step tables -tables "$tables" | pg
create_source_as_console "$namespace" "$source_name" "$id" "$password" "$tables" 3600000
wait_phase Streaming
check_alerts "$customer" "$source_name" ""

log "Its role loses REPLICATION: the connector cannot read, retries, and does not fail"
pg -c "ALTER ROLE \"$role\" NOREPLICATION" \
  -c "SELECT pg_terminate_backend(active_pid) FROM pg_replication_slots WHERE slot_name = '$slot' AND active_pid IS NOT NULL" >/dev/null
for _ in $(seq 30); do
  [[ $(slot_column active) == f ]] && break
  sleep 2
done
[[ $(slot_column active) == f ]] || die "the slot $slot is still active without REPLICATION"
# The database is written to meanwhile, as a customer's would be, a write
# that fails being no reason to stop.
(while true; do
  write_wal 20000 2>/dev/null || true
  sleep 15
done) &
WRITER=$!
# The stall rule wants 5 minutes without a heartbeat. A 64 MB limit lets a
# slot keep 64 to 80 MiB, as the database counts whole segments, so 12 MB
# passes the customer's 10% of it.
pg -c "ALTER SYSTEM SET max_slot_wal_keep_size = '64MB'" -c "SELECT pg_reload_conf()" >/dev/null
for _ in $(seq 12); do write_wal 1000000; done
check_alerts "$customer" "$source_name" PostgresSourceStalled,PostgresSourceConnectorStalled,PostgresSourceSlotFilling 48
[[ $(source_field .status.phase) == Streaming ]] ||
  die "the stalled source is in phase $(source_field .status.phase), where nothing else would have told"

log "The customer turns the slot's alert off, and it stops within two minutes"
set_alerts '{"sourceSlotFilling":{"enabled":false}}'
expect_threshold PostgresSourceSlotFilling
check_alerts "$customer" "$source_name" PostgresSourceStalled,PostgresSourceConnectorStalled 6

log "With REPLICATION back, the source reads again, and its alerts are quiet"
kill "$WRITER" 2>/dev/null
WRITER=""
pg -c "ALTER ROLE \"$role\" REPLICATION" >/dev/null
for _ in $(seq 60); do
  [[ $(slot_column active) == t ]] && break
  sleep 2
done
[[ $(slot_column active) == t ]] || die "the slot $slot is not active again with REPLICATION back"
check_alerts "$customer" "$source_name" "" 30
log "Alerts test passed: source $source_name's thresholds follow the customer's settings, and its stall alerted until it read again"

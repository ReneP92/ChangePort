#!/usr/bin/env bash
# Runs a Kafka command-line tool against a customer's cluster on kind with a
# given client.properties, such as one downloaded from the console, as the
# customer's own client would: from outside the cluster, through its load
# balancers. The file is at /config/client.properties for the tool, and
# standard input reaches it, for kafka-console-producer.sh. A tool ending in
# .java is a program of a single file in hack/, run with Kafka's clients.
# Usage: hack/kafka-with.sh <customer> <client.properties> <tool> [arguments]
# Example: hack/kafka-with.sh acme acme-client.properties kafka-topics.sh --list \\
#   --bootstrap-server bootstrap.acme.kafka.changeport.test:9094 --command-config /config/client.properties
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

customer=${1:?usage: hack/kafka-with.sh <customer> <client.properties> <tool> [arguments]}
properties=${2:?usage: hack/kafka-with.sh <customer> <client.properties> <tool> [arguments]}
tool=${3:?usage: hack/kafka-with.sh <customer> <client.properties> <tool> [arguments]}
[[ -f $properties ]] || die "$properties does not exist"

ensure_kubectl
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cp "$properties" "$work/client.properties"
kafka_client_container "$customer" "$work"
if [[ $tool == *.java ]]; then
  docker run "${KAFKA_CLIENT[@]}" -i -v "$ROOT/hack/$tool:/app/$tool:ro" --entrypoint java "$KAFKA_IMAGE" \
    -cp '/opt/kafka/libs/*' "/app/$tool" "${@:4}"
else
  docker run "${KAFKA_CLIENT[@]}" -i --entrypoint "/opt/kafka/bin/$tool" "$KAFKA_IMAGE" "${@:4}"
fi

#!/usr/bin/env bash
# Tests the examples of the customer connection guide (site/content/docs/connecting.md)
# against a customer's cluster on kind: each example, taken from the guide as
# it is written, produces a message and consumes it back as a consumer group,
# from outside the cluster through the load balancers, with the customer's
# credentials. The guide's examples trust the public certificate authorities
# that issue production certificates; on kind, the only change made to them
# is trusting the local CA.
# Usage: hack/connecting-guide-test.sh [customer-id] (default demo)
# shellcheck source=lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

customer=${1:-demo}
guide="$ROOT/site/content/docs/connecting.md"
run_id="$(date +%s)-$RANDOM"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# The images and client versions the examples run with.
JAVA_IMAGE=eclipse-temurin:21-jdk@sha256:23cc5734ff46e776f51ce66eb9ee08444fd47caabec0a95b7eb6e94be2d3696d
GO_IMAGE=golang:1.26.5@sha256:705e964a93a2fd2e75c7d59bb7d781b57e30f12293ffde5175c69229e18fb678
NODE_IMAGE=node:24-slim@sha256:55fb5f5a527ea2c5c4a95f963e48ef90aa38c7b346054fdc8ee9957a2b55d205
FRANZ_GO=v1.22.2
CONFLUENT_KAFKA_JAVASCRIPT=1.11.0

# Prints the first code block of the given language after a heading.
# Usage: example <heading> <language>
example() {
  awk -v heading="$1" -v fence="\`\`\`$2" '
    $0 == heading { found = 1; next }
    found && $0 == fence { inside = 1; next }
    inside && $0 == "```" { exit }
    inside { print }
  ' "$guide"
}

ensure_kubectl
prepare_kafka_client "$customer" "$work"
properties="$work/client.properties"
bootstrap=$(sed -n 's/^bootstrap.servers=//p' "$properties")
username=$(sed -nE 's/.*username="([^"]+)".*/\1/p' "$properties")
password=$(sed -nE 's/.*password="([^"]+)".*/\1/p' "$properties")
# The local CA, from the truststore lines of client.properties.
sed -n '/BEGIN CERTIFICATE/,/END CERTIFICATE/p' "$properties" |
  sed -E 's/^ssl\.truststore\.certificates=//; s/^ +//; s/ \\$//' >"$work/ca.crt"
grep -q "BEGIN CERTIFICATE" "$work/ca.crt" || die "no CA certificate in customer $customer's client.properties"

# Runs an example in its image: the container works on a copy of the
# example, so that what it installs and builds, owned by the container's
# user, stays out of this machine's directories.
# Usage: run <language> <image> <command>
run() {
  local language=$1 topic="guide-$1-$run_id" output
  docker run "${KAFKA_CLIENT[@]}" --entrypoint /opt/kafka/bin/kafka-topics.sh "$KAFKA_IMAGE" \
    --bootstrap-server "$bootstrap" --command-config /config/client.properties --create --topic "$topic" >/dev/null
  log "Running the guide's $language example"
  if ! output=$(docker run "${KAFKA_CLIENT[@]}" -e "KAFKA_BOOTSTRAP=$bootstrap" -e "KAFKA_USERNAME=$username" \
    -e "KAFKA_PASSWORD=$password" -e KAFKA_CLIENT_RACK=zone-a -e "TOPIC=$topic" -v "$work/$language:/example:ro" "$2" \
    sh -c "cp -R /example /app && cd /app && $3" 2>&1); then
    die "the guide's $language example failed:"$'\n'"$output"
  fi
  grep -q "consumed: hello from" <<<"$output" || die "the guide's $language example consumed nothing:"$'\n'"$output"
  # It committed what it processed, by itself rather than on a timer: its
  # group's offset is past the one message.
  output=$(docker run "${KAFKA_CLIENT[@]}" --entrypoint /opt/kafka/bin/kafka-consumer-groups.sh "$KAFKA_IMAGE" \
    --bootstrap-server "$bootstrap" --command-config /config/client.properties --describe --group "example-$language" 2>&1)
  awk -v topic="$topic" '$2 == topic && $4 == 1 && $6 == 0 { found = 1 } END { exit !found }' <<<"$output" ||
    die "the guide's $language example committed no offset after processing:"$'\n'"$output"
}

mkdir -p "$work/java" "$work/python" "$work/go" "$work/node"

# Java takes client.properties as it is, with its truststore lines on kind.
example "### Java" java >"$work/java/Example.java"
cp "$properties" "$work/java/client.properties"
container=$(docker create "$KAFKA_IMAGE")
docker cp -q "$container:/opt/kafka/libs/." "$work/java/libs/"
docker rm "$container" >/dev/null
run java "$JAVA_IMAGE" 'javac -cp "libs/*" Example.java && java -cp "libs/*:." Example'

example "### Python" python | sed 's|^topic = |config["ssl.ca.location"] = "/config/ca.crt"\ntopic = |' >"$work/python/example.py"
run python "$PYTHON_IMAGE" "pip install -q --root-user-action=ignore --disable-pip-version-check confluent-kafka==$CONFLUENT_KAFKA_PYTHON && python example.py"

example "### Go" go | sed 's|kgo.DialTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})|kgo.DialTLSConfig(\&tls.Config{MinVersion: tls.VersionTLS12, RootCAs: caPool()})|' >"$work/go/main.go"
cat >>"$work/go/main.go" <<'GO'

func caPool() *x509.CertPool {
	pem, err := os.ReadFile("/config/ca.crt")
	if err != nil {
		panic(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(pem)
	return pool
}
GO
sed -i.bak 's|^	"crypto/tls"$|	"crypto/tls"\n	"crypto/x509"|' "$work/go/main.go"
run go "$GO_IMAGE" "go mod init example 2>/dev/null && go get github.com/twmb/franz-go@$FRANZ_GO && go mod tidy && go run ."

example "### Node.js" js | sed 's|^  "sasl.password": process.env.KAFKA_PASSWORD,|&\n  "ssl.ca.location": "/config/ca.crt",|' >"$work/node/example.js"
run node "$NODE_IMAGE" "npm install --loglevel=error --no-audit --no-fund @confluentinc/kafka-javascript@$CONFLUENT_KAFKA_JAVASCRIPT >/dev/null && node example.js"

# The exactly-once example reads a message, writes it in capitals to
# another topic and commits its offset in the same transaction: the output
# reads back as committed, and the group's offset is past the input.
eos_in="guide-eos-in-$run_id" eos_out="guide-eos-out-$run_id"
kafka_tool() { docker run "${KAFKA_CLIENT[@]}" --entrypoint "/opt/kafka/bin/$1" "$KAFKA_IMAGE" "${@:2}"; }
for topic in "$eos_in" "$eos_out"; do
  kafka_tool kafka-topics.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties --create --topic "$topic" >/dev/null
done
echo "order-1:hello from exactly once" | docker run -i "${KAFKA_CLIENT[@]}" --entrypoint /opt/kafka/bin/kafka-console-producer.sh "$KAFKA_IMAGE" \
  --bootstrap-server "$bootstrap" --command-config /config/client.properties --topic "$eos_in" \
  --property parse.key=true --property key.separator=: >/dev/null
mkdir -p "$work/eos"
example "### Read, process, write in Java" java >"$work/eos/ExactlyOnce.java"
cp "$properties" "$work/eos/client.properties"
cp -R "$work/java/libs" "$work/eos/libs"
log "Running the guide's exactly-once example"
if ! output=$(docker run "${KAFKA_CLIENT[@]}" -e "TOPIC=$eos_in" -e "OUTPUT_TOPIC=$eos_out" -v "$work/eos:/example:ro" "$JAVA_IMAGE" \
  sh -c 'cp -R /example /app && cd /app && javac -cp "libs/*" ExactlyOnce.java && java -cp "libs/*:." ExactlyOnce' 2>&1); then
  die "the guide's exactly-once example failed:"$'\n'"$output"
fi
grep -q "exactly once: processed 1" <<<"$output" || die "the guide's exactly-once example processed nothing:"$'\n'"$output"
output=$(kafka_tool kafka-console-consumer.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties \
  --topic "$eos_out" --from-beginning --isolation-level read_committed --max-messages 1 --timeout-ms 30000 2>/dev/null)
[[ $output == "HELLO FROM EXACTLY ONCE" ]] || die "the exactly-once example's output reads back as '$output', not committed in capitals"
output=$(kafka_tool kafka-consumer-groups.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties \
  --describe --group example-exactly-once 2>&1)
awk -v topic="$eos_in" '$2 == topic && $4 == 1 && $6 == 0 { found = 1 } END { exit !found }' <<<"$output" ||
  die "the exactly-once example committed no offset with its transaction:"$'\n'"$output"
kafka_tool kafka-topics.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties --delete --topic "$eos_in" >/dev/null
kafka_tool kafka-topics.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties --delete --topic "$eos_out" >/dev/null
kafka_tool kafka-consumer-groups.sh --bootstrap-server "$bootstrap" --command-config /config/client.properties --delete --group example-exactly-once >/dev/null

# The command line example lists the topics with Kafka's own tools, from
# the directory that holds client.properties, as the guide says.
example "### Command line" bash >"$work/command-line.sh"
log "Running the guide's command line example"
output=$(docker run "${KAFKA_CLIENT[@]}" -e "KAFKA_BOOTSTRAP=$bootstrap" -v "$work/command-line.sh:/command-line.sh:ro" \
  -w /config --entrypoint sh "$KAFKA_IMAGE" -c 'PATH=/opt/kafka/bin:$PATH; . /command-line.sh' 2>&1) ||
  die "the guide's command line example failed:"$'\n'"$output"
grep -qx "guide-java-$run_id" <<<"$output" || die "the guide's command line example lists no topics:"$'\n'"$output"

# The guide's client.properties has the settings the console's has, in the
# same order; kind's adds only the local CA's truststore.
guide_keys=$(example "## Your connection details" properties | cut -d= -f1)
console_keys=$(grep -E '^[a-z.]+=' "$properties" | cut -d= -f1 | grep -v '^ssl\.truststore\.')
[[ $guide_keys == "$console_keys" ]] ||
  die "the guide's client.properties has $(tr '\n' ' ' <<<"$guide_keys")but the console's $(tr '\n' ' ' <<<"$console_keys")"

# The producers say acks=all and idempotence outright.
for language in java python go node; do
  grep -Eqs '"?acks"?[,:] *"all"|kgo\.RequiredAcks\(kgo\.AllISRAcks\(\)\)' "$work/$language"/* ||
    die "the guide's $language example leaves acks to the client's default"
  [[ $language == go ]] || grep -Eqs 'enable\.idempotence"[,:] *("true"|true|True)' "$work/$language"/* ||
    die "the guide's $language example leaves idempotence to the client's default"
done

# They committed by themselves rather than on a timer: consumers that commit
# on a timer also commit when they close, so the offsets alone cannot tell.
for language in java python go node; do
  grep -Eqs 'enable\.auto\.commit"[,:] *("false"|false|False)|kgo\.DisableAutoCommit\(\)' "$work/$language"/* ||
    die "the guide's $language example leaves automatic commits on"
done

# The examples leave a topic and a consumer group each behind.
for language in java python go node; do
  docker run "${KAFKA_CLIENT[@]}" --entrypoint /opt/kafka/bin/kafka-topics.sh "$KAFKA_IMAGE" \
    --bootstrap-server "$bootstrap" --command-config /config/client.properties --delete --topic "guide-$language-$run_id" >/dev/null
  docker run "${KAFKA_CLIENT[@]}" --entrypoint /opt/kafka/bin/kafka-consumer-groups.sh "$KAFKA_IMAGE" \
    --bootstrap-server "$bootstrap" --command-config /config/client.properties --delete --group "example-$language" >/dev/null
done
log "Connection guide test passed: the Java, Python, Go and Node.js examples produce and consume, and the exactly-once example reads, processes and writes, on customer $customer's cluster"

# Some of my producers don't show up in the console

The console found a topic that receives a good share of its data from
producers that send no telemetry. Their messages arrive as usual, but the
console cannot see who they are, so they are missing from the data flow
map, and the console cannot diagnose their errors.

## Why it happens

Kafka clients can report their own metrics to the cluster (KIP-714): how
much they send, to which topics, and what fails. The console builds its
picture of your producers from these reports. Producers that do not send
them still write to your topics, but the console only sees their bytes
arriving, without a name.

A producer sends no telemetry when:

- **its client is too old.** Kafka's Java client does so from 3.7, and
  librdkafka-based clients (such as the Python, Node.js and .NET clients
  built on it) from 2.5;
- **its client sends only part of it.** franz-go, the Go client of the
  [connection guide](../connecting.md#go), sends the metrics every client
  must, such as how long it is throttled, but not how much it sends to
  each topic, which the console names producers by. Its producers stay
  unseen, though the alert about throttled clients works for them;
- **its client does not support it at all.** Other clients send telemetry
  only if they implement KIP-714; check your client's documentation;
- **telemetry was turned off** with `enable.metrics.push=false`.

## What to do

1. **Find the unseen producers.** The data flow map, the **Data flow** card
   on the overview, shows what the topic receives from them as clients
   without telemetry. Compare it with the services you know write to the
   topic.
2. **Upgrade their clients** to Kafka's Java client 3.7 or newer, or
   librdkafka 2.5 or newer. They then send telemetry by themselves, with no
   setting to change.
3. **Or leave them as they are.** Older clients work fine. They just stay
   unseen: no name on the map, and no diagnoses of their errors or
   transactions.

## What the console misses without telemetry

- The producer on the data flow map, by client ID and Kafka user.
- Its failed and retried sends ([My producer's messages fail, or are sent twice](producer-errors.md)),
  and its name when a topic refuses its messages as too large
  ([My topic refuses messages because they are too large](oversized-messages.md)).
- Its aborted transactions
  ([My producer aborts transactions](transactions-aborted.md)).
- The `ClientThrottled` and `ClientRecordErrors` alerts
  ([alerts](../alerts.md)).

The brokers still count every message the topic receives, so the topic's
traffic, and the consumer groups' lag, stay complete.

## How the console tells

The console compares the bytes the brokers received for the topic with the
bytes its producers report sending. It notes the topic when more than
100 bytes a second, and more than a tenth of the topic's bytes, come from
producers without telemetry. Topics written by a Postgres source are left
out: Changeport's own connector writes them ([Postgres](../postgres.md)).

## In Kafka terms

This is **client metrics push**, from KIP-714, set on the client with
`enable.metrics.push` (on by default where supported). The cluster asks
clients for metrics through a **client metrics subscription**; Changeport
sets that up for you.

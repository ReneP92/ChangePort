# My producer's messages fail, or are sent twice

The console found a producer that, over the last 15 minutes, either failed
to send 1% or more of its messages, or had to send 10% or more of them
again. Failed messages never reach your topic unless your code handles the
error, and messages sent again arrive late.

## Why it happens

The console names one of three cases.

**"Records from ... fail before reaching Kafka."** The producer refused the
messages itself, before sending them. The usual causes:

- a message larger than the producer's `max.request.size` (1 MB by default);
- a topic that does not exist: your cluster does not create topics on first
  use;
- the producer's Kafka user lacking permission to write to the topic.

**"Records from ... fail to be delivered."** The messages were sent, but
failed on their way or were refused by the topic. The usual causes:

- brokers the producer cannot reach before `delivery.timeout.ms` runs out;
- too few in-sync copies of a partition for `acks=all`, while a broker is
  down;
- messages without a key sent to a compacted topic;
- messages larger than the topic's `max.message.bytes`.

**"... sends many records twice."** The messages arrive in the end, but
only after a retry. Retries follow timeouts, and brokers moving partitions
around, such as during a restart.

## What to do

1. **Read the producer's log.** Each failed send has an error that says
   why; the console can only name the usual causes.
2. **For messages that are too large,** keep large payloads in object
   storage and send a message that refers to them
   ([keep messages small](../connecting.md#keep-messages-small)).
3. **For a topic that does not exist,** create it in the console, or fix
   the topic name in the producer.
4. **For unreachable brokers,** check the network between the producer and
   the cluster ([if you cannot connect](../connecting.md#if-you-cannot-connect)),
   and the overview's **Health** card for a broker that is down.
5. **Keep the default retry settings.** Clients retry failed sends until
   `delivery.timeout.ms` has passed, which carries them through a broker
   restart ([keep the default retries](../connecting.md#keep-the-default-retries)).
   A producer with few retries fails sends that would have succeeded.
6. **If retries go on,** check the network, and `request.timeout.ms`.
   Occasional retries during a broker restart are normal.

## How the console tells

Producers report their own sends, errors and retries to the cluster
(KIP-714): Kafka's Java client 3.7 or newer, and librdkafka 2.5 or newer,
do this by themselves. The console looks at producers that tried to send
at least one message every 10 seconds, and flags one when:

- 1% or more of what it tried to send failed (a warning). It tells the two
  error cases apart by whether the producer reported the errors against a
  topic;
- or else 10% or more of its messages were sent again (a note).

Producers that send no telemetry are not checked
([Some of my producers don't show up in the console](producers-without-telemetry.md)). Errors
on a topic that refused messages for their size are counted under
[My topic refuses messages because they are too large](oversized-messages.md) instead. The
`ClientRecordErrors` alert notifies you of failing producers
([alerts](../alerts.md)).

## In Kafka terms

The producer metrics behind this are `record-error-total` and
`record-retry-total`. Settings involved: `delivery.timeout.ms`,
`request.timeout.ms`, `retries`, `acks`, `max.request.size`, and the
topic's `max.message.bytes` and `min.insync.replicas`. See
[delivery guarantees](../delivery-guarantees.md).

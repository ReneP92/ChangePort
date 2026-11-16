# My topic refuses messages because they are too large

The console found a topic that refused messages over the last 15 minutes
because they were larger than it accepts. A message refused this way is not
stored: unless its producer handles the error, the message is lost.

## Why it happens

Every topic has a largest message it accepts, 1 MB by default. Producers
send messages in batches, and the topic checks each batch. It refuses:

- **a single message larger than the limit,** such as a file, an image or
  a large JSON document sent as one message;
- **a batch of smaller messages that adds up to more than the limit,** when
  the producer's batches are larger than the topic accepts.

Java producers split a refused batch of smaller messages and send the parts
again, so those messages still arrive. A single message over the limit
cannot be split, and fails.

## What to do

1. **Find the producers.** The console names the producers whose records
   failed on the topic, if they send telemetry
   ([Some of my producers don't show up in the console](producers-without-telemetry.md)).
   Their logs show the error.
2. **Keep large payloads out of Kafka.** Store files and images in object
   storage such as S3, and send a message that refers to them
   ([keep messages small](../connecting.md#keep-messages-small)).
3. **Compress messages in the producer** with `compression.type=zstd`, if
   they are large by nature, such as big JSON documents. The topic checks
   the compressed size.
4. **Raise the topic's limit** only when messages truly need to be that
   large. Set the topic's `max.message.bytes` with Kafka's
   `kafka-configs.sh` tool; the console does not change it yet. The
   producer's `max.request.size` (1 MB by default) must then allow them
   too.
5. **Handle send errors in your producer,** so that a refused message is
   logged or retried elsewhere rather than silently lost.

## How the console tells

The brokers count the bytes each topic refuses for their size. The console
flags a topic that refused any over the last 15 minutes, with the limit and
the bytes refused. It names a producer when that producer reports errors on
the topic itself; producers without telemetry stay unnamed. Once the topic
refuses nothing for 15 minutes, the warning clears.

While a topic refuses messages for their size, producers' errors on it
count here rather than under
[My producer's messages fail, or are sent twice](producer-errors.md). The `TopicRequestErrors` and
`ClientRecordErrors` alerts may fire for the same refusals
([alerts](../alerts.md)).

## In Kafka terms

The topic's `max.message.bytes` limits a **record batch**, after
compression. The broker answers a larger one with `MESSAGE_TOO_LARGE`
(`RecordTooLargeException` in Java). On the producer, `max.request.size`
limits a request, and `batch.size` how large it lets a batch grow. See
Kafka's
[topic configs](https://kafka.apache.org/documentation/#topicconfigs) and
[topics](../topics.md).

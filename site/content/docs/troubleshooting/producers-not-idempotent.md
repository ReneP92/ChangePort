# My producers may write duplicate messages

The console noticed a busy topic whose producers are not idempotent. When
such a producer sends a message and the answer from Kafka is lost, it
sends the message again, and the topic may store it twice. Your consumers
then process it twice.

## Why it happens

Networks drop answers now and then, and brokers restart. A producer that
does not hear back retries its send. If the first attempt had in fact
arrived, the retry writes a duplicate.

An idempotent producer numbers its messages, so Kafka recognizes a retry
and keeps only one copy. Whether a producer is idempotent depends on its
client and settings:

- **Java clients from 3.2** are idempotent by default, unless another
  setting turns it off without telling you, such as `acks=1`.
- **librdkafka-based clients,** such as the Python and Node.js ones, are
  not idempotent unless you set `enable.idempotence=true`.
- **Older clients** are not idempotent by default.

## What to do

1. **Set `enable.idempotence=true` and `acks=all` on the topic's
   producers,** even where they are the default, so that a conflicting
   setting cannot silently turn idempotence off. The
   [connection guide's examples](../connecting.md#delivery-guarantees) do
   this in every language.
2. **Check for settings that conflict** with idempotence in Java, such as
   `acks=1` or `acks=0`, `retries=0`, or
   `max.in.flight.requests.per.connection` above 5.
3. **Restart the producers** with the new settings. The topic's
   **Producers** card then lists them, as Kafka keeps state for idempotent
   producers.
4. **Make processing safe to repeat** where duplicates would hurt, as
   idempotence covers one producer's retries only, not a message your
   application sends twice itself.

## How the console tells

Kafka keeps state only for idempotent and transactional producers; others
leave no trace on the brokers. So the console checks the other way round:
it notes a topic that received at least 1 message a second over the last
15 minutes while none of its idempotent or transactional producers wrote
to it recently.

This means the console cannot tell a topic with a mix of producers apart:
if one producer is idempotent and another is not, nothing is noted. Check
each producer's settings yourself. Topics written by a Postgres source are
left out, as Changeport's own connector writes them
([Postgres](../postgres.md)). This is a note, not a warning.

## In Kafka terms

This is the **idempotent producer** (`enable.idempotence`). Kafka gives
each idempotent producer a **producer ID** and tracks a **sequence number**
per partition, so it can drop a duplicate retry. `acks=all` waits for every
**in-sync replica**. Kafka keeps a producer's state for
`producer.id.expiration.ms` (a day by default). For exactly once from one
topic to another, see
[exactly once](../connecting.md#exactly-once-read-process-write) and
[delivery guarantees](../delivery-guarantees.md).

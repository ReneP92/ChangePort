# A long transaction holds back my consumers

The console found a transaction that has been open for a minute or more,
while consumers that read only committed messages wait behind it. Until it
commits or aborts, those consumers read nothing past its start on the
partitions it wrote to.

## Why it happens

A producer that uses transactions writes messages, then commits them all
at once. Consumers with `isolation.level=read_committed` read a partition
only up to its oldest open transaction, so they cannot tell whether those
messages will count. A healthy transactional producer holds them back for a
moment all the time. A transaction open for a minute or longer usually
means:

- **Long transactions by design,** such as committing once every few
  minutes, or after thousands of messages.
- **A producer that stopped in the middle** of a transaction: it crashed,
  hangs, or was stopped without aborting. Kafka then aborts the
  transaction only after the producer's `transaction.timeout.ms`.
- **A transaction stuck committing or aborting.** Kafka finishes a
  transaction by writing a marker to every partition it wrote to, and keeps
  trying while a partition cannot take it, such as one without a leader or
  without enough in-sync copies.

## What to do

The console says whether the transaction is open, committing or aborting.

If it is open:

1. **Keep transactions short:** commit every few seconds, or every few
   hundred messages.
2. **Keep `transaction.timeout.ms` short too,** so that a producer that
   stops mid-transaction holds consumers back only briefly. It is a minute
   by default in Java and librdkafka-based clients, and 40 seconds in
   franz-go. The console says when Kafka will abort this one.
3. **Check the producer** named by the `transactional.id`: is it running,
   or stuck on slow work inside the transaction?

If it is committing or aborting:

1. **Check the overview's Health card.** Markers wait for every partition
   the transaction wrote to to have a leader and enough copies in sync.
   Changeport's operators are paged when a broker is down.
2. **If the cluster is healthy and it stays stuck,** write to Changeport
   support.

## How the console tells

The console asks Kafka for the open transactions and how many messages
each holds back on each partition. It flags a transaction open for a minute
or more that holds back at least one message, counted on the partitions
where it is the oldest open transaction. The topic's **Producers** card and
the overview's **Health** card show open transactions too.

A transaction that Kafka has lost track of, and will never abort, is
critical instead: [A stuck transaction blocks my consumers](hanging-transaction.md).

## In Kafka terms

Consumers with `isolation.level=read_committed` read up to the partition's
**last stable offset** (LSO); messages between it and the **high
watermark** are held back. The coordinator aborts a transaction after
`transaction.timeout.ms`, which may not exceed the brokers'
`transaction.max.timeout.ms` (15 minutes): a producer that asks for more
fails to start. Its states are `Ongoing`, `PrepareCommit` and `PrepareAbort`.
See [delivery guarantees](../delivery-guarantees.md) and
[exactly once](../connecting.md#exactly-once-read-process-write).

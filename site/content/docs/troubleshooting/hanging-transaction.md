# A stuck transaction blocks my consumers

The console found a transaction left open on a partition that Kafka will
never finish by itself. Consumers that read only committed messages read
nothing on that partition past where the transaction started, until
Changeport support aborts it.

## Why it happens

A producer that uses transactions writes messages to partitions, then
commits or aborts. Kafka's transaction coordinator keeps track of every
open transaction and finishes it by writing a marker to each partition it
wrote to. If a producer stops in the middle, the coordinator aborts the
transaction once `transaction.timeout.ms` has passed.

A transaction hangs when a partition has it open but the coordinator no
longer does. Then nothing ever writes its marker. This is rare. It comes
from edge cases in Kafka's transaction protocol, such as a producer's late
write that reaches a partition after its transaction already ended.

Consumers with `isolation.level=read_committed` read a partition only up to
its oldest open transaction, so they wait behind the hanging one for ever.
Consumers that read uncommitted messages, the Java client's default, are
not held back.

Restarting the producer does not help: the coordinator no longer counts the
transaction as open on that partition, so it does not abort it there.

## What to do

1. **Write to Changeport support** with what the console's suggestion
   lists: the topic, and each partition with the offset the transaction
   started from. Help, under Changeport's mark at the foot of the console's
   sidebar, leads to support. Aborting a hanging transaction takes rights
   on the cluster that your Kafka user does not have, so support does it
   for you.
2. **Once it is aborted,** read_committed consumers continue past it. What
   the hanging transaction wrote is discarded, as with any aborted
   transaction.
3. **Keep your producers' clients up to date.** Newer Kafka clients and
   brokers guard against the edge cases that leave transactions hanging.

## How the console tells

The console looks at the producers Kafka keeps on each partition, and at
the transactions their coordinators have open. It flags a partition where a
transaction has been open from the same offset for longer than any
transaction may last on your cluster (15 minutes, the brokers'
`transaction.max.timeout.ms`), while the coordinator does not have it open
there. It names the producer by its `transactional.id` when Kafka still
knows it, or else by the ID Kafka gave it. It shows one finding per topic,
with every partition affected.

This is critical: it holds consumers back now, and does not clear by
itself. After the console restarts, it needs 15 minutes of looking before
it can tell a transaction hangs.

A transaction that is merely open for long, and that Kafka will still
abort, is a different diagnosis: [A long transaction holds back my consumers](open-transaction.md).

## In Kafka terms

This is a **hanging transaction**. It pins the partition's **last stable
offset** (LSO), the point read_committed consumers read up to. Kafka's
`kafka-transactions.sh` tool finds them with `find-hanging` and aborts them
with `abort`, which writes the missing **transaction marker**. See
[delivery guarantees](../delivery-guarantees.md) and
[exactly once](../connecting.md#exactly-once-read-process-write).

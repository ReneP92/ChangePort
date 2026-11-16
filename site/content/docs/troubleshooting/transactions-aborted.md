# My producer aborts transactions

The console noticed a producer that aborted transactions over the last 15
minutes. What an aborted transaction wrote is discarded: consumers that
read only committed messages never see it. Occasional aborts can be normal,
but frequent ones mean wasted work, and often an error you want to know
about.

## Why it happens

A transactional producer aborts a transaction instead of committing it
when:

- **processing failed:** your code hit an error and called abort, as the
  [exactly once example](../connecting.md#exactly-once-read-process-write)
  does;
- **the transaction took too long:** Kafka aborts a transaction once it has
  been open for the producer's `transaction.timeout.ms`;
- **a newer instance took over:** a producer that starts with the same
  `transactional.id` fences off the older one, and the older one's open
  transaction is aborted. Two running instances that share a
  `transactional.id` keep fencing each other.

## What to do

1. **Read the producer's log.** It says why each transaction aborted.
2. **For processing errors,** fix their cause, or skip messages that can
   never succeed, rather than abort and retry them for ever.
3. **For timeouts,** commit more often, so that each transaction finishes
   well within `transaction.timeout.ms`
   ([A long transaction holds back my consumers](open-transaction.md)).
4. **For fencing,** give each running instance its own `transactional.id`,
   one that stays the same when that instance restarts.
5. **Check that downstream consumers read committed messages only**, with
   `isolation.level=read_committed`. The Java client reads uncommitted
   messages unless told otherwise, so its consumers do see what aborted
   transactions wrote ([delivery
   guarantees](../connecting.md#delivery-guarantees)).

## How the console tells

Kafka counts aborts nowhere: not on the brokers, and not in the clients'
metrics. Java producers report the time they spend aborting, so the console
names the producers that spent any time aborting in the last 15 minutes,
but cannot say how many transactions they aborted. This is a note, not a
warning.

Only producers that send telemetry report it: Kafka's Java client 3.7 or
newer. Producers on other clients, or without telemetry, are not checked
([Some of my producers don't show up in the console](producers-without-telemetry.md)).

## In Kafka terms

The producer calls `abortTransaction()`; the **transaction coordinator**
writes **abort markers**, and consumers with `isolation.level=read_committed`
skip the aborted messages. Fencing comes from the **producer epoch** tied
to a `transactional.id`. The metric behind this note is the Java producer's
`txn-abort-time-ns-total`. See
[delivery guarantees](../delivery-guarantees.md).

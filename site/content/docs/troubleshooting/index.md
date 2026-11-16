# Troubleshooting

The console looks at your cluster for the common Kafka problems, and lists
what it finds on the overview under **What needs attention**, with the
numbers that show it. Each finding links to its page here, which says why
it happens and what to do.

## Consumers

- [My consumers can't keep up with a topic](consumer-too-slow.md)
- [My consumer group has stopped consuming](nobody-consuming.md)
- [My consumers are about to lose messages, or already lost some](behind-retention.md)
- [Some of my consumers aren't doing anything](idle-consumers.md)
- [My consumer group keeps rebalancing](frequent-rebalances.md)

## Topics

- [One partition of my topic gets most of the data](hot-partition.md)
- [My topic got more partitions, and messages may be out of order](partitions-added.md)
- [My topic refuses messages because they are too large](oversized-messages.md)

## Producers

- [My producer's messages fail, or are sent twice](producer-errors.md)
- [Some of my producers don't show up in the console](producers-without-telemetry.md)
- [My producers may write duplicate messages](producers-not-idempotent.md)

## Transactions

- [A long transaction holds back my consumers](open-transaction.md)
- [A stuck transaction blocks my consumers](hanging-transaction.md)
- [My producer aborts transactions](transactions-aborted.md)

## Can't connect?

See [If you cannot connect](../connecting.md#if-you-cannot-connect) in the
connection guide.

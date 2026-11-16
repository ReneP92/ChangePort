# My consumers can't keep up with a topic

The console found a consumer group that reads a topic more slowly than
messages arrive, so it falls further behind every minute. Left alone, the
delay keeps growing, and once it reaches the topic's retention, messages are
deleted before the group reads them.

## Why it happens

A topic is split into partitions, and within a consumer group each partition
is read by one consumer at a time. So a group can only go as fast as its
consumers, and it can only use as many consumers as the topic has
partitions.

The usual causes:

- **Too few consumers.** The group runs fewer consumers than the topic has
  partitions, so some consumers read several partitions each.
- **Slow processing.** Each message waits on something slow, such as a
  database write or an HTTP call per message.
- **More traffic than before.** Producers write more than the group was
  sized for, after a launch or a backfill.
- **A consumer stuck on one message,** retrying it again and again while
  the rest of its partition waits.

## What to do

The console's suggestion tells you which case you are in.

1. **If the group has fewer consumers than partitions,** run more
   consumers with the same `group.id`. The suggestion says how many more
   can share the work: one per partition at most. Extra consumers beyond
   that sit idle ([Some of my consumers aren't doing anything](idle-consumers.md)).
2. **If the group already has a consumer for each partition,** make each
   consumer faster. Process messages in batches, for example one database
   write per poll rather than one per message, and move slow work out of
   the loop that polls.
3. **Or add partitions,** so that more consumers can share the topic. An
   admin opens the topic's page and chooses **Edit** on its **Settings**.
   Adding partitions sends some keys to other partitions, so for a while
   messages with the same key may arrive out of order; the dialog warns
   you before you save ([My topic got more partitions, and messages may be out of order](partitions-added.md)).
4. **Look for a stuck consumer** in your consumers' logs: the same message
   failing again and again.

Once the group consumes at least as fast as messages arrive, its delay stops
growing and the diagnosis clears. It then still has to work through the
backlog; the group's page shows its **Lag** going down.

## How the console tells

Over the last 15 minutes, the console compares the messages the topic
received with the messages the group consumed, counted from the offsets it
commits. It flags the group when all of these hold:

- the topic receives at least 1 message a second;
- the group consumed less than 90% of what the topic received;
- the group is at least a minute behind, and more than 30 seconds further
  behind than 15 minutes ago;
- the group is stable (not sharing its partitions out) and some of its
  consumers read the topic.

A short burst does not count, and neither does a group whose offsets went
back in the window, such as one replaying the topic. Because the count
comes from committed offsets, a group that commits rarely, or not at all,
looks slower than it is ([commit offsets after
processing](../connecting.md#commit-offsets-after-processing)).

The `ConsumerGroupLagHigh` alert can notify you about the same group
([alerts](../alerts.md)).

## In Kafka terms

This is growing **consumer lag**: the group's committed offsets fall
further behind the partitions' latest offsets. Parallelism in a consumer
group is capped by the topic's **partition count**. Settings worth knowing:
`max.poll.records`, `fetch.min.bytes` and `fetch.max.wait.ms` on the
consumer. See [consumer groups](../consumer-groups.md) for how groups share
partitions.

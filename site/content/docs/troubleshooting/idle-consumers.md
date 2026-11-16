# Some of my consumers aren't doing anything

The console found a consumer group in which some consumers read no
partition at all. They use resources and do no work, which may be fine as
standbys, or may mean the group is not spread the way you think.

## Why it happens

Within a consumer group, each partition is read by one consumer at a time.
The console tells two cases apart:

- **More consumers than partitions.** A topic with 6 partitions can keep at
  most 6 consumers of one group busy. A seventh waits with nothing to read.
- **Partitions handed out unevenly.** The group reads several topics, and
  has enough partitions in total for every consumer, yet some get none. The
  default way many clients assign partitions (the range assignor) hands
  them out topic by topic, starting from the same consumers each time. Two
  topics of 2 partitions each, read by 3 consumers, give 2 consumers 2
  partitions each and the third none.

## What to do

1. **Decide whether you want standbys.** An idle consumer takes over
   partitions when another consumer stops, so a spare one can be a
   deliberate choice. Then there is nothing to do.
2. **If there are more consumers than partitions** and you want them all
   working, add partitions to the topic. An admin opens the topic's page and
   chooses **Edit** on its **Settings**. Adding partitions can change the
   order of a key's messages for a while; the dialog explains it before
   you save ([My topic got more partitions, and messages may be out of order](partitions-added.md)). Or run fewer
   consumers.
3. **If there are enough partitions but they are spread unevenly,** change
   how the group assigns them:
   - use the cooperative-sticky assignor:
     `partition.assignment.strategy=cooperative-sticky` in librdkafka-based
     clients, or `org.apache.kafka.clients.consumer.CooperativeStickyAssignor`
     in Java;
   - or, with Kafka 4.0 clients or newer, use Kafka's own consumer group
     protocol with `group.protocol=consumer`, where the brokers assign the
     partitions.

   Switching a running group to another assignor takes care, as its
   consumers must agree: your client's documentation says how (in Java,
   two rolling restarts).

## How the console tells

The console asks Kafka which partitions each member of the group reads.
It flags a stable group that has at least one member with no partition. If
the group has more members than its topics have partitions together, it
says so; otherwise, it says they are handed out unevenly. This is a note,
not a warning: it does not change the group's health.

## In Kafka terms

Kafka calls this **partition assignment**. The group's leader assigns
partitions with the **assignor** set in `partition.assignment.strategy`
(classic protocol), or the group coordinator does it under the new
**consumer group protocol** (KIP-848, `group.protocol=consumer`). The
**range assignor** assigns per topic; **round robin** and
**cooperative-sticky** spread across topics. See
[consumer groups](../consumer-groups.md).

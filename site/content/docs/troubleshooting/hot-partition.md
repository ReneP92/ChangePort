# One partition of my topic gets most of the data

The console found a topic where one partition received far more of the
topic's data over the last day than the others. Only one consumer of a
group reads that partition, so it does more work than the rest and can
hold the whole group back, however many consumers you add.

## Why it happens

Messages with the same key always go to the same partition. That is how
Kafka keeps a key's messages in order. It also means that a key with few
values, or one value much busier than the rest, loads some partitions more
than others:

- **A key with few distinct values,** such as a country, a status or a
  customer tier. A topic of 12 partitions keyed by 3 countries uses 3 of
  them at most.
- **One very busy key,** such as one large customer when the key is the
  customer ID.
- **The same key on every message,** such as an empty string or a fixed
  value set by mistake. Every message then goes to one partition.

## What to do

1. **Pick a key with more distinct values,** such as an order ID rather
   than a country. Use the ID of what the message is about, so that the
   messages that must stay in order still share a key
   ([use keys for order](../connecting.md#use-keys-for-order)).
2. **Leave the key out** where the order of messages does not matter.
   Messages without a key are spread over all partitions.
3. **Do not expect more partitions to help.** A key still maps to one
   partition, so a busy key stays on one partition after you add more.

Changing the key affects new messages only. As the console looks at the
last day, the warning clears about a day after the spread evens out.

## How the console tells

The console compares the size of each partition on the brokers. It does
this only for tiered topics (**Storage: Tiered** in the topic's
**Settings**), because the brokers keep just their last day, so their sizes
show how messages spread now. It flags a topic when:

- it has 3 partitions or more;
- its partitions hold at least 64 MB together on the brokers;
- one partition holds at least twice its fair share: with 4 partitions,
  50% or more of the bytes instead of 25%.

The console cannot see this on topics kept only on the brokers, or on
compacted topics, whose sizes reflect their whole history. Topics that got
partitions in the last day are left out too, as new partitions start empty.
The console also cannot see whether messages carry keys at all.

## In Kafka terms

This is a **hot partition**, or **partition skew**, caused by **key
skew**. The producer's **partitioner** hashes each record's key to pick its
partition, so the same key always lands on the same partition. Records
without a key are spread by the **sticky partitioner**. See
[topics](../topics.md).

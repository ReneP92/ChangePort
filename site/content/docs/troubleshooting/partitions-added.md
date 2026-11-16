# My topic got more partitions, and messages may be out of order

The console noticed that a topic that consumer groups read got more
partitions in the last day. Since then, a key's new messages may go to a
different partition than its earlier ones, so for a while consumers may
have read some messages of the same key out of order, or skipped some.

## Why it happens

Kafka keeps messages in order within a partition, and sends every message
with the same key to the same partition. Which partition depends on how
many partitions the topic has. Add partitions, and many keys move to
another partition:

- a key's last messages before the change sit in its old partition,
  possibly not read yet;
- its first messages after the change go to the new one, and may be read
  first.

Consumers also find new partitions only when they refresh what they know
about the topic, within 5 minutes. A consumer with
`auto.offset.reset=latest`, the Java client's default, starts a new
partition at its newest message, and so skips everything that reached it
before.

Partitions can be added from the console or with any Kafka tool. The
console notes both.

## What to do

1. **If the order of a key's messages does not matter to your consumers,**
   there is nothing to do. This is a note, not a warning.
2. **If order matters,** check what your consumers made of the messages
   around the time of the change, for example records updated in the wrong
   order.
3. **Check for skipped messages** in groups whose consumers use
   `auto.offset.reset=latest`. To have them processed, an admin can move
   the group back to a point in time just before the change, with
   **Change position** on the group's page. The group then reads everything
   since that time again, on every partition, so its processing must be
   safe to repeat. The dialog shows how many messages it reads again
   before anything changes.
4. **Next time,** set `auto.offset.reset=earliest` on consumers before you
   add partitions, so that they read new partitions from their start.

The note clears a day after the change.

## In Kafka terms

The default **partitioner** picks a partition from a hash of the key,
modulo the **partition count**, so changing the count remaps keys.
Consumers discover new partitions on their next **metadata refresh**
(`metadata.max.age.ms`, 5 minutes by default), and start reading a
partition without a **committed offset** at the position
`auto.offset.reset` says. Kafka cannot remove partitions once added. See
[topics](../topics.md) and [use keys for
order](../connecting.md#use-keys-for-order).

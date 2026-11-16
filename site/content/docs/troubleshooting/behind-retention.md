# My consumers are about to lose messages, or already lost some

The console found a consumer group so far behind on a topic that the topic
deletes, or already deleted, messages the group has not read yet. A topic
keeps messages only for its retention time, whether or not anyone read
them, so a group that falls too far behind loses them for good.

## Why it happens

Retention deletes old messages on a timer. It does not wait for consumer
groups. A group is at risk when it is behind by a large share of that time:

- **It is too slow for the topic,** and has been falling behind for hours
  ([My consumers can't keep up with a topic](consumer-too-slow.md)).
- **Its consumers stopped,** and nobody noticed
  ([My consumer group has stopped consuming](nobody-consuming.md)).
- **It was moved back,** for example to replay the last 20 hours of a
  topic that keeps messages for a day. For an hour after an admin moves a
  group in the console, the console says the move is why.
- **The retention was shortened** while the group was behind.

## What to do

The console shows a **warning** when the group is close, and marks it
**critical** once messages were deleted before it read them.

If it is close to losing messages:

1. **Help the group catch up.** Start its consumers if they stopped, or run
   more of them ([My consumers can't keep up with a topic](consumer-too-slow.md)).
2. **Keep the messages longer while it does.** An admin opens the topic's
   page, chooses **Edit** on its **Settings**, and picks a longer
   retention. This works only for messages not yet deleted.

If it already lost messages:

1. **Find out what the missed messages were for**, and whether you can get
   them again from their source, such as by re-running the producer.
2. **Check where the consumers continued.** They continue from where
   `auto.offset.reset` points them: `earliest` reads from the oldest message
   the topic still keeps, `latest` skips to the newest. With `none`, they
   stop with an error instead.
3. **Prevent it next time:** keep the group from falling that far behind,
   or keep the topic's messages longer. Keep the threshold of the lag alert
   well below your topics' retention, so that you hear of a group falling
   behind long before it loses messages ([alerts](../alerts.md)).
4. **If nothing uses the group any more, delete it.** Otherwise it stays
   critical ([My consumer group has stopped consuming](nobody-consuming.md)).

## How the console tells

- **Close to losing messages:** the oldest message the group has not read
  is at least half the topic's retention old. With a day of retention,
  that is 12 hours behind.
- **Lost messages:** on at least one partition, the group's committed
  offset points before the oldest message the topic still keeps.

Compacted topics keep each key's latest message however old, and topics
kept forever delete nothing, so both are left out. The console measures
delay up to 23 hours back, so on a topic kept longer than 46 hours it can
only tell once messages were lost.

## In Kafka terms

Topics delete old log segments after `retention.ms` (and `retention.bytes`,
if set). A group whose **committed offset** falls below the partition's
**log start offset** gets an out of range error, and the consumer resets
according to `auto.offset.reset`. See [topics](../topics.md) for retention,
and [consumer groups](../consumer-groups.md) for offsets.

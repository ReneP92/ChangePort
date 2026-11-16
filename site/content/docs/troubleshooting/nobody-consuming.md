# My consumer group has stopped consuming

The console found a consumer group with no consumers running while its
topic keeps receiving messages. Nothing reads them, so the group falls
further behind, and once it is far enough behind, retention deletes
messages it never read.

## Why it happens

A consumer group remembers how far it got, even when none of its consumers
are running. When they stop, the group stays, and new messages pile up
behind it. Common reasons:

- **The consumers crashed or were scaled to zero,** for example after a
  failed deploy or a configuration error at startup.
- **The consumers run on a schedule,** such as an hourly job, and are not
  running right now.
- **The group was renamed.** Your service now runs with a new `group.id`,
  and the old group is left behind with its offsets.
- **The service was retired,** and nobody deleted its group.

## What to do

1. **If the consumers should be running, start them again** with the same
   `group.id`. They continue from where the group got to. Check their logs
   for why they stopped.
2. **If the group is no longer used, delete it**, so that it no longer
   counts as behind. The console cannot delete groups yet: use Kafka's
   `kafka-consumer-groups.sh` tool with `--delete --group <name>`, passing
   your `client.properties` with `--command-config`, as the
   [connection guide](../connecting.md) shows for Kafka's own tools. Kafka
   deletes a group only while it has no consumers.
3. **If the consumers run on a schedule,** this warning shows whenever they
   are not running. Make sure each run catches up, and that the time
   between runs stays well within the topic's retention.

## When the console says a group "waits for its consumers"

If an admin moved the group in the console in the last hour, or created it
to replay another group's messages, the console shows a note instead of a
warning: the group waits for its consumers. Moving a group needs its
consumers stopped, so the delay is expected. Start the consumers with the
`group.id` the note gives; they read from where the group was moved, then
keep up with the topic. See **Change position** on a consumer group's page,
and [consumer groups](../consumer-groups.md).

## How the console tells

The console flags a group that has no consumers, while its topic received
messages over the last 15 minutes and the group has messages waiting on it.
The group's page shows how many consumers it has, and its **Lag**.

The `ConsumerGroupStuck` alert notifies you when a group has not committed
for a while as messages keep arriving ([alerts](../alerts.md)). If the
group stays stopped for long, see
[My consumers are about to lose messages, or already lost some](behind-retention.md).

## In Kafka terms

The group is **empty**: it has no members, but Kafka keeps its **committed
offsets**, so its **consumer lag** grows with every new message. Kafka
removes an empty group's offsets on its own only after
`offsets.retention.minutes` (7 days by default). Deleting a group is the
same as `kafka-consumer-groups.sh --delete`.

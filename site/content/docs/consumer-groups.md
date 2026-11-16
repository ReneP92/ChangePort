# Consumer groups and lag

A consumer group is a name your consumers share, such as
`search-indexer`, so that they read a topic together and Kafka remembers
how far they got. This page covers how groups share work, how the console
tells how far behind one is, and how to make a group read messages again.

## How a group shares a topic

Every consumer that starts with the same `group.id` joins the same group.
Kafka gives each of them some of the topic's partitions, and each
partition to one consumer of the group at a time:

- **A topic with 6 partitions** and a group of 3 consumers gives each
  consumer 2 partitions. With 6 consumers, each gets one.
- **More consumers than partitions** leaves the extra ones idle: they wait
  in case another one stops. To read with more consumers, the topic needs
  [more partitions](topics.md#adding-partitions).
- **Different groups** read the same topic independently, each from its
  own position. Give each service its own group: `search-indexer` and
  `billing` each read every order.

When a consumer joins or leaves, or stops responding, Kafka hands its
partitions out again. That is a rebalance: the group's consumers pause
while it happens, for a moment if all goes well.

## Where a group is

Kafka remembers each group's position on each partition: the offset of the
next message it will read. Your consumers commit their position as they go,
and a consumer that takes over a partition, after a restart or a rebalance,
starts where the last one committed.

Commit after you have processed a message, not before: then a crash means
reading a message again rather than skipping it. See
[Commit offsets after processing](connecting.md#commit-offsets-after-processing).

A group that has never committed on a partition starts from the setting
`auto.offset.reset`: the newest message (`latest`, the Java client's
default) or the oldest the topic keeps (`earliest`).

## Lag, in time

Lag is how far behind a group is: the messages that have arrived on a
topic and that the group has not read yet. The console shows it as time,
the age of the oldest message the group has not read: "2 minutes behind"
says more than "48,000 messages behind", and means the same whether the
topic gets ten messages a second or ten thousand.

- **A few seconds behind** is normal: messages take a moment to process.
- **Growing lag** means the group reads more slowly than messages arrive.
  The console says so when it sees it, as
  [a consumer group can't keep up](troubleshooting/consumer-too-slow.md).
- **Lag that keeps getting older while no messages are read** means the
  group's consumers stopped, or are stuck on a message.

The overview's **Consumer groups** card shows every group's lag over the
last hour. The console measures it every 30 seconds from the group's
committed position and the times your producers gave the messages, without
reading any. It is exact under steady traffic, and close otherwise. It
shows lag older than 23 hours as "more than 23 hours".

Admins set how far behind a group may fall before an alert fires: see
[A consumer group falls behind](alerts.md#a-consumer-group-falls-behind).

## Moving a group

Sometimes a group must read from somewhere else: again from yesterday,
after a bug corrupted what it wrote; or from now, skipping a backlog that
no longer matters. Admins do this from **Change position** on the group's
page. Kafka calls it resetting the group's offsets.

The group can start from:

- **A point in time:** the first message at or after it.
- **The oldest message** its topics keep.
- **Now**, skipping every message it has not read yet.

You choose which of the topics it reads to move. Before anything changes,
the console shows for each topic how many messages the group reads again
or skips, and from when. On compacted topics and topics written in
transactions the numbers are close rather than exact.

Kafka moves a group only while none of its consumers run, because a
running consumer keeps its own position and would commit over the new
one. Stop the group's consumers first: the console names the ones still
running, and waits until they have stopped. Then type the group's name to
confirm, and start your consumers again. They read from the new position.

Moving a group back means your service handles those messages again, with
everything they trigger, such as emails sent twice. Moving it forward
skips messages for good.

## Replaying into a new group

To read messages again without touching the group running in production,
**replay** them into a new group. In **Change position**, choose a new
group instead of moving this one: the console creates it at the position
you choose, and shows the `group.id` to start a service with. The original
group carries on as it was, with its consumers running.

A replay is how you rebuild a service's state, fill a new service with
history, or produce a fixed bug's output again, alongside production.

Old messages come back from object storage, as fast as the brokers allow
for reading history: up to 8 MB a second per broker on the production
profile, and 2 MB on the dev profiles. The console says how many of the
messages are that old, and the least time they take. That limit is what
keeps a replay from slowing your live consumers down, which read recent
messages from the brokers' disks.

## After a move

Each move and replay shows on the group's timeline, with who made it and
how many messages it reads again or skips. For the hour after, the console
explains the group's lag by the move, and a group with no consumers yet
is a note rather than a warning.

## Deleting a group

A group that no longer exists in your applications keeps its committed
positions, and its lag keeps growing as messages arrive, which can fire
alerts. Once you no longer need it, delete it with Kafka's
`kafka-consumer-groups.sh` and its `--delete` option, connected as the
[command line](connecting.md#command-line) example is.

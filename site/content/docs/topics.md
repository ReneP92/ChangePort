# Topics, partitions and keys

A topic is a named stream of messages, such as `orders` or `page-views`:
your producers append messages to it, and your consumers read them in the
order they arrived. Here is how to choose a topic's settings, and what
changing them later does.

## Messages

A message is a key, a value and a timestamp, plus optional headers. The
value is your data, in any format: JSON, Avro, Protobuf or plain text.
Kafka stores bytes and never looks inside.

Keep messages small: topics accept up to 1 MB each. Put large payloads,
such as files or images, in object storage, and send a message that refers
to them. See [Keep messages small](connecting.md#keep-messages-small).

Kafka keeps messages after they are read. Reading doesn't remove them, so
any number of services can read the same topic, each at its own pace, and
a service can read a topic again from the start.

## Partitions

A topic is split into partitions, which spread it across your brokers.
Each partition is an ordered log of its own. Partitions are how a topic
grows past one consumer: a consumer group spreads them among its
consumers, so a topic with six partitions can have up to six consumers in
one group reading it at once.

When you create a topic, choose its partitions by the most consumers you
expect to read it in parallel:

- **3**, the console's suggestion, suits most topics. It lets three
  consumers share the work, one on each of the production profile's
  brokers.
- **More**, such as 12, for a topic with a lot of traffic, or consumers
  that are slow per message: each partition is read by one consumer of a
  group at a time.
- **Up to 48** on the production profile, 24 on dev HA and 12 on dev. Each
  partition costs the brokers memory and file handles, so a cluster holds
  up to 1,000 partition copies per broker in all.

You can add partitions later, but [not remove them](#adding-partitions),
so start small rather than large.

## Keys and order

Kafka keeps messages in order within a partition, not across a topic. To
keep related messages in order, give them the same key: messages with the
same key always go to the same partition, so a consumer reads them in the
order they were sent.

Use the thing whose events must stay in order as the key: an order's ID
for its events, a customer's ID for their account changes. Messages
without a key are spread across partitions, in no order between them.

A key that a large share of messages have, such as one big customer, sends
them all to one partition, which then gets more work than the rest. The
console notices this, as
[one partition gets most of the messages](troubleshooting/hot-partition.md).
See also [Use keys for order](connecting.md#use-keys-for-order).

## Retention

Retention is how long a topic keeps messages, from an hour to 30 days.
Kafka deletes older ones, whether anyone read them or not, so choose a
retention longer than your consumers could ever fall behind, with room for
a weekend when nobody is looking:

- **7 days**, the console's suggestion, covers a consumer that is down
  over a long weekend.
- **Longer**, up to 30 days, for topics you want to replay from, such as
  events that rebuild a service's state.
- **Shorter**, down to an hour, for data that is worthless once old, such
  as a live location.

Messages older than a day move to cheaper object storage, so a long
retention costs less than its size suggests. See
[Old messages cost less](your-cluster.md#old-messages-cost-less).

## Compacted topics

A compacted topic keeps the latest message for each key, however old,
rather than everything for a time. It suits topics that hold the current
state of things, such as each customer's settings: a new consumer reads
the latest value of every key, without the history. To delete a key, send
a message with that key and an empty value (a tombstone).

Choose **Keep the latest message per key** under **Old messages** when you
create the topic (`cleanup.policy=compact`). Compacted topics delete
nothing by age, so their retention does not apply, and they stay on the
brokers' disks.

## Names

Topic names use letters, digits, dots, underscores and dashes, up to 200
characters. Kafka counts dots and underscores as the same in its metrics,
so `orders.eu` and `orders_eu` cannot both exist. Names starting with `_`,
`changeport`, `strimzi` or `connect-` belong to Kafka and the platform.

## Creating topics

Admins create topics in the console, from **Create topic** on the
overview, which applies all of the above as you type. Topics are never
created by a client that writes to a name that does not exist, so a typo
fails rather than starting a new, empty topic.

Your `admin` user can also create topics with Kafka's tools or Admin API.
Those topics get the cluster's copies, but not the console's other
guardrails: choose their partitions and retention with the same care.

## Changing retention

Admins change a topic's retention from **Edit** on the topic's page. Before
anything is saved, the console says what the change does to the consumer
groups reading the topic.

A shorter retention deletes older messages for good:

- **A consumer group further behind than the new retention** loses the
  messages it has not read yet. They are deleted before it gets to them.
  The console names such groups, and says so on the save button.
- **A group at least half as far behind** is close: unless it catches up,
  it loses messages too.
- **When the console doesn't know how far behind the groups are**, it
  counts that as a possible loss.

Kafka deletes old messages a file (log segment) at a time: within minutes
on a busy topic, and on a quiet one once its current file closes, which
can take up to 7 days. A longer retention keeps messages that would have
been deleted from then on; it cannot bring back what is gone.

## Adding partitions

Admins add partitions from **Edit** on the topic's page. Before you save,
know what it does:

- **Keys move.** A key's messages go to a partition chosen from the key and
  the number of partitions. With more partitions, a key's new messages may
  go to another partition than its earlier ones, so for a while consumers
  may read that key's messages out of order. If order matters, add
  partitions while the topic is quiet, or create a new topic and move your
  producers to it.
- **Consumers find the new partitions within 5 minutes**
  (`metadata.max.age.ms`). Consumers that start from the newest message
  when they have no position (`auto.offset.reset=latest`, the Java client's
  default) skip the messages that reach a new partition before they find
  it. Set `auto.offset.reset=earliest` on your consumers to read them.
- **Partitions can be added, never removed.** Kafka cannot take a
  partition away.

## Deleting topics

Deleting a topic deletes its messages for good. The console names the
consumer groups still reading it, and takes the topic's name typed to
confirm.

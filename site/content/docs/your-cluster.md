# What Changeport runs for you

Each of your environments, such as production, staging and dev, is a Kafka
cluster of its own, run for you alone. Changeport keeps it running, keeps
your data safe across failures, and decides the settings that newcomers to
Kafka most often get wrong.

## Your cluster

A Kafka cluster is a few servers, called brokers, that store your messages
and serve them to your applications. Your environment's size, its profile,
decides how many:

| Profile    | Brokers                                    | Copies of your data | Made for                                         |
| ---------- | ------------------------------------------ | ------------------- | ------------------------------------------------ |
| Production | 3, one in each of three availability zones | 3, one per zone     | Your production traffic                          |
| Dev HA     | 3, one in each of three availability zones | 3, one per zone     | Testing how your applications ride out a failure |
| Dev        | 1                                          | 1                   | Trying Kafka out, and development                |

On the production profile each broker has a server of its own, which no
other customer shares, with 100 GB of disk for recent messages.

The console shows each environment's profile on the **Environments** page.
Admins add environments there, up to five.

## How your data stays safe

On the production and dev HA profiles, Kafka keeps three copies of every
message, one in each availability zone. A message counts as written once
every copy that is up to date has it, and Kafka refuses to count it with
fewer than two. So a whole zone can fail without losing a message or
stopping your producers and consumers: Kafka moves the work to the other
two brokers, and your clients follow by themselves. We test this by taking
zones and brokers away under load, and check that every message arrives,
exactly once.

What your producers need for this is in the connection guide:
[keep the default retries](connecting.md#keep-the-default-retries), and let
the client wait for every copy (`acks=all`).

The dev profile keeps one copy on one broker. It is for trying things out:
when its broker restarts, your clients wait until it is back.

## Old messages cost less

Topics that delete old messages keep the last day on the brokers' disks
and move older ones to object storage (Amazon S3), where storing them costs
a fraction as much. This is Kafka's tiered storage. Your applications see
no difference: reading an old message works the same as reading a new
one, a little slower to start.

Reading a lot of history back, such as a replay of a month of messages, is
capped per broker, at 8 MB a second on the production profile and 2 MB on
the dev profiles. That keeps a replay from slowing down the consumers that
read recent messages, which come from the brokers' disks. See
[Consumer groups and lag](consumer-groups.md) for replays.

Compacted topics, which keep the latest message per key, stay on the
brokers' disks: tiered storage does not support them.

## What you decide

- **Your topics:** their names, how many partitions, how long they keep
  messages, and whether they delete old messages or keep the latest per
  key. See [Topics, partitions and keys](topics.md).
- **Your applications:** your producers, consumers and consumer groups,
  in any language with a Kafka client.
- **Your alerts:** how far behind a consumer group may fall, and where
  alerts go: Slack, PagerDuty or a webhook. See [Alerts](alerts.md).
- **Your Postgres sources:** which tables stream into topics. See
  [Postgres sources](postgres.md).
- **Your team:** who may change things (admins) and who may look
  (members).

## What the platform decides

These are the settings that are hard to get right and costly to get wrong.
They are fixed per profile, so that you never have to think about them:

- **Copies:** three per partition on the production and dev HA profiles.
  A message counts as written once every up-to-date copy has it, and never
  with fewer than two (`replication.factor=3`, `min.insync.replicas=2`). Topics you create
  get them too.
- **No topics by accident:** a topic is created when someone creates it,
  never because a client wrote to or read a name that does not exist
  (`auto.create.topics.enable=false`). A typo in a topic name fails
  loudly rather than starting an empty topic.
- **Limits that keep a cluster fast:** up to 48 partitions per topic on
  the production profile (24 on dev HA, 12 on dev), and up to 1,000
  partition copies per broker. Messages of up to 1 MB.
- **Kafka's version and its upgrades.** Your cluster runs
  Kafka 4.3, without ZooKeeper (KRaft).
- **Connections:** TLS with a certificate every runtime trusts, and a
  user name and password (SASL SCRAM-SHA-512). See
  [Connecting](connecting.md).
- **Platform topics:** topics and consumer groups whose names start with
  `_`, `changeport`, `strimzi` or `connect-` belong to Kafka or to the
  platform. The console hides them and won't create topics with those
  names. Your `admin` user could, with Kafka's own tools: leave them
  alone.

## What we watch

Changeport's operators are paged about your cluster itself, such as a
broker that is down, a disk filling up or partitions with too few copies,
and act on it. You don't need to watch your brokers.

What we can't fix for you is in your applications: a consumer that falls
behind, a producer that fails to send. The console watches those too, and
explains what it finds on the overview under **What needs attention**. See
[Troubleshooting](troubleshooting/index.md).

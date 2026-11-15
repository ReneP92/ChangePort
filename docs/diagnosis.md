# Diagnoses

Customers new to Kafka get answers rather than dashboards: the console looks
at each environment's cluster for the common Kafka problems and says, in
plain language, what is wrong, the numbers that show it and what to do. They
show on the overview under "What needs attention", on the page of the topic
or consumer group they are about, and as a mark on the data flow map.

Alerts and diagnoses differ: an alert notifies, at a threshold the customer
sets ([alerts.md](alerts.md)); a diagnosis explains, whenever the console is
open. A group behind its alert threshold may also be diagnosed as too slow
for its topic, which says why and what to do.

## How it works

`internal/diagnosis` holds the rules. Each is a Go function over a snapshot
of the cluster, which `internal/console/diagnoses.go` puts together on
request:

- **What Kafka says now:** the topics with their partitions, retention,
  cleanup policy, tiering and largest message, and the consumer groups with their state and
  every member with the partitions it reads. The console asks Kafka on each
  diagnosis rather than keep these as metrics, so they are never older
  than the request.
- **The metrics over a window,** 15 minutes by default (`-diagnosis-window`
  on the console; 5 minutes on kind, so that the end-to-end test needs
  minutes rather than a quarter of an hour), so that a moment's spike or
  dip is no diagnosis: the messages each topic received, how many messages
  each group consumed (from its committed offsets, so every client counts,
  with or without telemetry), each group's lag now and a window ago, each
  partition's size, the bytes each topic refused as too large, and each
  producer's sends, errors and retries, with its errors per topic.
- **The timeline:** each group's rebalances in the last hour, and the
  topics that got partitions in the last day.

The console keeps an environment's diagnoses for 30 seconds, as the overview
asks every 15. `GET /api/customer/diagnoses` returns them, the most severe
first; each has a stable ID, its rule and its subject, so the console can
tell it is the same one while it holds.

The rules are code rather than alert rules because a diagnosis needs what
metrics lack, such as a topic's partitions and a group's members, and its
explanation needs arithmetic: "up to 3 more consumers". Each rule is tested
from hand-written snapshots (`internal/diagnosis/diagnosis_test.go`), and
`web/e2e/clerk/diagnosis.spec.ts` checks one end to end on kind: a consumer
group that falls behind is told it can't keep up, and the diagnosis clears
once it catches up.

## Health

Each topic and consumer group has a health level, from the diagnoses about
it, so a customer sees at a glance where to look:

- **Healthy:** no warnings. Notes, which may be fine, leave it healthy.
- **Needs attention:** warnings.
- **At risk:** anything critical.

Three levels rather than a score from 0 to 100, which would invite a
question it cannot answer ("why 73?"): a level always comes with the
diagnoses that set it. A diagnosis about a group reading a topic is the
group's, as the group falls behind, not the topic: a topic's page lists
those apart, under the consumer groups that read it. The topics and consumer
groups tables show it, and sort by it worst first; the data flow map marks
the nodes that are not healthy; and a topic's and a group's pages show it
beside their names, with the last 24 hours in 15-minute steps and the
diagnoses that explain it.

The history comes from the console's health monitor
(`internal/console/health.go`), which diagnoses every ready customer's
cluster every minute (`-health-interval`), whether or not anyone looks, and
exports the levels on its own metrics port (9090, which the Service leaves
out and a NetworkPolicy opens to VictoriaMetrics alone) for it to keep:
`changeport_health_level` for each topic and group that is not healthy, and
`changeport_health_checked` for each customer it looked at. A step without
a look, such as while the console was down, shows as unknown rather than
healthy. It diagnoses four clusters at a time, each for at most 30
seconds, so one that does not answer holds up only its own look, and its
diagnoses stay fresh until its next look, so the pages find them ready. The
lists wait at most 2 seconds for health, and without it show none rather
than keep the customer waiting.

## The rules

Severities: **critical** hurts now, **warning** needs attention before it
hurts, **info** is worth knowing and may be fine. The platform's own topics
and groups are left out.

| Rule | Finds | Severity |
| --- | --- | --- |
| `consumer-too-slow` | A stable group, some of whose members read the topic, that consumed less than 90% of what the topic received over the window, at least a minute behind, and further behind than a window ago. Topics below a message a second are too quiet to tell, and a group whose offsets went back in the window (replaying the topic) is left alone. | warning |
| `nobody-consuming` | A group without members, with messages waiting, whose topic still receives more. | warning |
| `behind-retention` | A group behind by half its topic's retention or more: messages may be deleted before it reads them. Critical once retention deleted messages it never read (its committed offset is below the log's start). Compacted topics keep each key's latest message, so they are left out. | warning, critical |
| `idle-consumers` | A stable group with members assigned no partition: more consumers than partitions, or partitions handed out topic by topic to the same consumers. | info |
| `frequent-rebalances` | A group that shared its partitions out again 6 times or more in the last hour, as members joined or left while it consumed, from the timeline. A group starting, or its last member leaving, does not count. | warning |
| `hot-partition` | A tiered topic of 3 partitions or more, with at least 64 MB on the brokers, where one partition holds twice its fair share or more: a few busy keys. Tiered topics keep only their last day on the brokers, so the sizes show how messages spread now; topics that got partitions within the day are left out, as new partitions start empty. | warning |
| `partitions-added` | A topic that got partitions in the last day, from the console or any other tool, while consumer groups read it: a key's new messages may go to another partition than its earlier ones. | info |
| `oversized-messages` | A topic that refused batches larger than its `max.message.bytes` over the window (the brokers' rejected bytes, counted from 0 as the brokers report a topic's only from its first refusal), with the producers whose records failed on it, as far as they report them. A single message that large is lost unless its producer handles the error; Java producers split a batch of smaller ones and send them again. | warning |
| `producer-errors` | A producer trying to send a record every 10 seconds or more, 1% or more of whose records fail; or, short of that, that sends 10% or more of its records again. It tells records that failed in the producer itself, before reaching Kafka (such as ones over `max.request.size`), from those that failed on their way to a topic or at it (refused, or expired after `delivery.timeout.ms`), by the producer's errors per topic. Errors on a topic that refused batches for their size are `oversized-messages`', and do not count here. Records that fail before they are sent count as tried. | warning, info |
| `producers-without-telemetry` | A topic that receives more than 100 B/s, and more than a tenth of its bytes, from producers that send no telemetry (KIP-714), which leaves them off the data flow map. A Postgres source's topics, written by Changeport's own Connect worker, are left out. | info |

What each suggests:

- **Too slow:** with fewer consumers than partitions, run more, up to the
  partition count; with a consumer per partition already, make each faster,
  or add partitions, which sends keys to other partitions so that for a
  while messages with the same key may arrive out of order.
- **Nobody consuming:** start the consumers again, or delete a group nothing
  uses, so that it no longer counts as behind.
- **Behind retention:** help the group catch up, or keep the topic's
  messages longer; once messages were lost, check what they were for, and
  where `auto.offset.reset` sent the consumers.
- **Idle consumers:** fine as standbys; to share the work, add partitions,
  or, when there are enough, spread them with the cooperative-sticky
  assignor or Kafka's own consumer group protocol.
- **Rebalances:** `group.instance.id`, so that a restart keeps its
  partitions, and short work between polls (`max.poll.interval.ms`,
  `max.poll.records`).
- **Hot partition:** a key with more distinct values, or no key where order
  does not matter.
- **Partitions added:** nothing where the order of a key's messages does
  not matter; where it does, a check of what consumers made of the messages
  around the change, and of groups with `auto.offset.reset=latest`, which
  skip what reached a new partition before they found it.
- **Oversized messages:** large payloads in object storage, with a message
  that refers to them; or compression (`compression.type=zstd`), or a
  larger `max.message.bytes`, which the producer's `max.request.size` must
  then allow too.
- **Producer errors:** the producer's own log, and the usual causes. Before
  reaching Kafka: records over `max.request.size` (1 MB by default), topics
  that do not exist (they are not created on first use), missing
  permissions. On the way or at Kafka: brokers out of reach within
  `delivery.timeout.ms`, too few in-sync replicas for `acks=all`, records
  without a key on a compacted topic, messages over the topic's
  `max.message.bytes`. Retries follow timeouts and moving partitions.
- **Without telemetry:** a Java client 3.7 or newer, or librdkafka 2.5 or
  newer.

## What the rules cannot see

- **Lag older than 23 hours** is reported as 23 hours (the agent's oldest
  lookup, [metrics-labels.md](metrics-labels.md)), so a group behind a topic
  kept for more than 46 hours is only flagged as close to retention once
  messages were lost. Such a group still counts as falling behind while
  messages wait.
- **Hot partitions of compacted or untiered topics,** whose sizes on the
  brokers hold their whole history rather than how messages spread now.
- **Unkeyed messages where order matters.** The brokers' metrics do not say
  whether messages carry keys, and even the messages themselves cannot say
  whether their consumers rely on their order, so a guess would flag every
  unkeyed topic, most of them fine. The connection guide explains keys and
  order instead ([connecting.md](connecting.md#use-keys-for-order)), and
  the console warns before partitions are added; `partitions-added` notes
  partitions added elsewhere.
- **Offsets committed before the messages are processed** happen inside
  consumers and leave no trace on the brokers: a commit looks the same
  whenever it comes. The connection guide's examples, which the console's
  snippets are, commit after processing
  ([connecting.md](connecting.md#commit-offsets-after-processing)).
- **Why a record failed** is not in a producer's telemetry, so
  `producer-errors` names the usual causes rather than the one; only the
  brokers' rejected bytes pin a refusal on a message's size, and while a
  topic refuses batches for their size, all of a producer's errors on it
  count as that.
- **Rebalances shorter than the agent's 30 seconds** between looks may be
  missed, so the count is a lower bound. The agent marks a change of
  members as a rebalance when the group had members before and after it.

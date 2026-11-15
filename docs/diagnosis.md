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

- **What Kafka says now:** the topics with their partitions, retention and
  cleanup policy, and the consumer groups with their state and every member
  with the partitions it reads.
- **The metrics over a window,** 15 minutes by default (`-diagnosis-window`
  on the console; 5 minutes on kind, so that the end-to-end test needs
  minutes rather than a quarter of an hour), so that a moment's spike or
  dip is no diagnosis: the messages each topic received, how many messages
  each group consumed (from its committed offsets, so every client counts,
  with or without telemetry), each group's lag now and a window ago, each
  partition's size, and each producer's sends, errors and retries.
- **The timeline's last hour:** how often each group's members changed.

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

## The rules

Severities: **critical** hurts now, **warning** needs attention before it
hurts, **info** is worth knowing and may be fine. The platform's own topics
and groups are left out.

| Rule | Finds | Severity |
| --- | --- | --- |
| `consumer-too-slow` | A group with members that consumed less than 90% of what its topic received over the window, at least a minute behind, and further behind than a window ago. Topics below a message a second are too quiet to tell. | warning |
| `nobody-consuming` | A group without members whose topic still receives messages, so its lag grows. | warning |
| `behind-retention` | A group behind by half its topic's retention or more: messages may be deleted before it reads them. Critical once retention deleted messages it never read (its committed offset is below the log's start). Compacted topics keep each key's latest message, so they are left out. | warning, critical |
| `idle-consumers` | A stable group with members assigned no partition: more consumers than partitions. | info |
| `frequent-rebalances` | A group whose members changed 6 times or more in the last hour, from the timeline. | warning |
| `hot-partition` | A topic of 3 partitions or more, with at least 64 MB on the brokers, where one partition holds twice its fair share or more: a few busy keys. | warning |
| `producer-errors` | A producer sending a record a second or more whose records fail, 1% or more; or, short of that, that sends 10% or more of its records again. | warning, info |
| `producers-without-telemetry` | A topic that receives more than 100 B/s, and more than a tenth of its bytes, from producers that send no telemetry (KIP-714), which leaves them off the data flow map. | info |

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
- **Idle consumers:** fine as standbys; to share the work, add partitions.
- **Rebalances:** `group.instance.id`, so that a restart keeps its
  partitions, and short work between polls (`max.poll.interval.ms`,
  `max.poll.records`).
- **Hot partition:** a key with more distinct values, or no key where order
  does not matter.
- **Producer errors:** the producer's own log, and the usual causes: records
  over `max.request.size` or the topic's `max.message.bytes`, topics that do
  not exist (they are not created on first use), missing permissions.
  Retries follow timeouts and moving partitions.
- **Without telemetry:** a Java client 3.7 or newer, or librdkafka 2.5 or
  newer.

## What the rules cannot see

- **Lag older than 23 hours** is reported as 23 hours (the agent's oldest
  lookup, [metrics-labels.md](metrics-labels.md)), so a group behind a topic
  kept for more than 46 hours is only flagged once messages were lost.
- **Unkeyed messages where order matters, and offsets committed before the
  messages are processed,** happen inside clients and leave no trace on the
  brokers; the connection guide's settings and snippets cover them instead
  (#177).
- **Rebalances shorter than the agent's 30 seconds** between looks may be
  missed, so the count is a lower bound.

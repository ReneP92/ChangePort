# The change timeline

When something changes in a customer's cluster, the console shows it on a
timeline over its graphs: a lag spike sits next to the rebalance or the
configuration change that caused it. The overview shows every change over
its throughput graph; a topic's page shows the topic's changes, and a
consumer group's page the group's and those of the topics it reads. Broker
restarts, which affect everything, show everywhere.

## What the timeline shows

| Change | Recorded by | When it shows |
| --- | --- | --- |
| A topic created or deleted, in the console or by a client | The Kafka agent, and the console, which names who did it | Within 30 seconds |
| A topic's configuration changed, such as its retention | The Kafka agent | Within 30 seconds |
| A topic given more partitions | The Kafka agent | Within 30 seconds |
| An ACL added or removed | The Kafka agent | Within 30 seconds |
| A consumer group rebalanced: members joined, left or rejoined | The Kafka agent | Within 30 seconds |
| A broker restarted: a rolling update, a certificate renewal, a crash, or its pod replaced | The provisioner | At once |
| The brokers started or stopped throttling a client | vmalert, Alertmanager and the provisioner | Within a minute or two, stamped with when it started and stopped |

Changes are found by comparing the cluster from one look to the next, so a
change the agent finds is stamped with when it found it, up to 30 seconds
after it happened. The agent leaves the platform's own topics, groups and
users out, as the console does, and the tiered storage it turns on itself.

### Throttling

The brokers throttle a client that exceeds a quota: they delay its
requests. Clients report how long they are throttled themselves (KIP-714),
so throttling shows for clients of Kafka 3.7 and newer, and of librdkafka
2.5 and newer. Any throttling at all marks the timeline, unlike the
`ClientThrottled` alert, which has a threshold ([alerts.md](alerts.md)).

## How it works

For operators:

- **The store** is VictoriaLogs (`deploy/base/victoria-logs`), next to
  VictoriaMetrics in the `metrics` namespace, and kept as long: 30 days. Each
  event is a log entry in its customer's stream (`tenant`), with its `kind`,
  a plain-language `_msg`, and what it is about: `topic`,
  `consumer_group`, `broker` or `client_id`. Its NetworkPolicy lets only the
  Kafka agents, the provisioner and the console reach it.
- **The Kafka agent** of each customer compares the cluster's topics, their
  own settings, its ACLs and its consumer groups' members every 30 seconds
  (`internal/agent/changes.go`); its first look after it starts only sets
  the baseline.
- **The provisioner** watches the Kubernetes Events Strimzi records when it
  restarts a broker (`StrimziInitiatedPodRestart`), which say why, and the
  brokers' pods, for a Kafka process that exits and a pod replaced without
  Strimzi (`internal/controller/brokerevents.go`). It also receives the
  timeline's alerts (`deploy/base/alerting/rules/timeline.yaml`), which
  Alertmanager sends only to it, and records when each fired and resolved.
- **Writers never wait for the store:** each keeps events in memory and
  writes them in batches, again while VictoriaLogs is down, up to 10,000
  events (`internal/events`). The `changeport_events_*` metrics count what
  was written, dropped and failed.
- **The console** reads a customer's events for the graph's range, always
  in the signed-in customer's stream, and drops the agent's record of a
  topic the console created or deleted itself, whose record names who did
  it.

To read a customer's events directly:

```bash
kubectl -n metrics port-forward svc/victoria-logs 9428
curl -s localhost:9428/select/logsql/query -d 'query={tenant="c-7f3k2q"} _time:1h | sort by (_time) desc'
```

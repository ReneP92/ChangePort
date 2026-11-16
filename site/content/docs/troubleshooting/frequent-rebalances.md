# My consumer group keeps rebalancing

The console found a consumer group whose members changed 6 times or more in
the last hour. Each time a consumer joins or leaves, the group stops to
share its partitions out again, and reads less meanwhile, so frequent
changes slow it down and can make it fall behind.

## Why it happens

A group shares its partitions out again (rebalances) whenever its set of
consumers changes. Some changes are expected, such as scaling up. Frequent
ones usually come from:

- **Consumers that restart often:** crashes, health checks that kill them,
  out of memory, or autoscaling that adds and removes them every few
  minutes. Each restart is two changes: once when the consumer leaves, and
  once when it joins again.
- **Consumers that take too long between polls.** A consumer must ask for
  more messages within `max.poll.interval.ms` (5 minutes by default). If
  processing one batch takes longer, Kafka considers it gone, removes it,
  and it joins again on its next poll.
- **Consumers that stop sending heartbeats,** for example under long
  garbage collection pauses or CPU starvation, for longer than
  `session.timeout.ms` (45 seconds by default in current clients).
- **Deploys.** A rolling deploy restarts every consumer, one after another.

## What to do

1. **Check whether your consumers restart.** Look at your platform's
   restart counts and the consumers' logs around the rebalances. The
   group's page marks each rebalance on its **Lag** graph, so you can match
   them with your deploys.
2. **Keep the work between polls short.** Ask for fewer messages per poll
   (`max.poll.records`, 500 by default in Java), or hand slow work to
   another thread and keep polling. Raise `max.poll.interval.ms` only if
   some batches truly need longer.
3. **Give each consumer a `group.instance.id`**, unique per instance and
   the same after a restart, such as a pod name in a Kubernetes
   StatefulSet. Kafka then keeps its partitions for it while it restarts,
   as long as it is back within `session.timeout.ms`.
4. **Slow down autoscaling** of consumers, so that it does not add and
   remove them every few minutes.

## How the console tells

The console records the group's members every 30 seconds, and counts a
rebalance whenever they changed while the group had consumers before and
after. A group starting, or its last consumer leaving, does not count.
Changes shorter than 30 seconds may be missed, so the real number can be
higher than the count shown.

The count covers the last hour, not the last 15 minutes, so after you fix
the cause the warning can take up to an hour to clear.

## In Kafka terms

This is a **consumer group rebalance**. Settings involved:
`max.poll.interval.ms`, `max.poll.records`, `session.timeout.ms`,
`heartbeat.interval.ms`, and `group.instance.id` for **static membership**.
The **cooperative-sticky** assignor and Kafka's newer consumer group
protocol (`group.protocol=consumer`) let consumers keep reading their other
partitions during a rebalance, which makes each one cheaper, but the
console still counts every change. See
[consumer groups](../consumer-groups.md) and Kafka's
[consumer configs](https://kafka.apache.org/documentation/#consumerconfigs).

# Alerts

Changeport watches how your applications use your cluster, and alerts you
when something looks wrong: a consumer group that falls behind or stops,
clients that are throttled, requests that fail, and Postgres sources that
hold back your database. This page says what each alert means and what to
do.

These alerts are about your applications and your sources. Changeport's
operators are paged about the cluster itself, such as a broker that is
down, and act on it.

## Where alerts show

Every alert shows in the console as it fires and clears: at the top of the
overview, as a badge on the topic or consumer group it is about, and on the
**Alerts** page, which the sidebar counts them next to.

Admins choose on the Alerts page where else alerts go, any or all of:

- **Slack:** an incoming webhook URL (`https://hooks.slack.com/...`), which
  posts to the channel it was made for, when an alert fires and when it
  resolves.
- **PagerDuty:** the routing key of an Events API v2 integration. Alerts
  raise incidents, and resolve them.
- **Webhook:** an HTTPS URL that receives
  [Alertmanager's webhook payload](https://prometheus.io/docs/alerting/latest/configuration/#webhook_config)
  as JSON, once when an alert fires and once when it resolves.

Each alert links to its section of this page. The console keeps your
receivers' secrets where nobody can read them back, the console included:
to change one, enter it again.

## Thresholds

Admins set when the alerts about your applications fire, on the Alerts
page, and can turn each one off. Changes take effect within a minute.

| Alert                         | Fires when                                                                                                  | Default    |
| ----------------------------- | ----------------------------------------------------------------------------------------------------------- | ---------- |
| A consumer group falls behind | Its oldest unread message on a topic is older than the threshold, for a minute                              | 10 minutes |
| A consumer group is stuck     | It has not committed on a topic for longer than the threshold, while messages wait for it and keep arriving | 15 minutes |
| A client is throttled         | The brokers delay its requests for longer than the threshold at a time, for 5 minutes                       | 100 ms     |
| Requests fail                 | More than the threshold of a topic's requests, or of a producer's messages, fail, for 5 minutes             | 5%         |

## A consumer group falls behind

The group's oldest message on a topic that it has not read yet is older
than your threshold (`ConsumerGroupLagHigh`). The consumer groups card on
the overview shows the same lag. A group falls behind when its consumers
are slower than its producers, or not running.

- Check that the group's consumers run and are not restarting.
- Compare how fast messages arrive on the topic with how fast the group
  reads them. A group with fewer consumers than the topic has partitions
  can add some.
- A burst of messages can put a healthy group behind for a while. Raise the
  threshold if that is normal for the topic.

The console's diagnoses often say why, such as
[a consumer group that can't keep up](troubleshooting/consumer-too-slow.md).
See [Lag, in time](consumer-groups.md#lag-in-time).

## A consumer group is stuck

The group has not committed its position on the topic for longer than your
threshold, while messages wait for it and new ones arrive
(`ConsumerGroupStuck`). Its consumers are stuck on a message, crash on it
before they commit, or are gone.

- Look for a consumer that fails on the same message again and again, and
  fix or skip that message.
- A group that no longer exists in your applications keeps its position
  and its lag. Delete the group once you no longer need it.

See also [a consumer group has stopped consuming](troubleshooting/nobody-consuming.md).

## A client is throttled

The brokers delay a client's requests because it exceeds a quota
(`ClientThrottled`). Clients report the delay themselves, so this alert
works for clients of Kafka 3.7 and newer, and of librdkafka 2.5 and newer,
such as the Python and Node.js clients.

- Check whether the client sends or reads far more than usual, such as a
  replay or a loop.
- Spread the work over more time, or contact us if your traffic has grown
  for good.

## Requests fail

The brokers refuse a share of a topic's requests to write or read
(`TopicRequestErrors`), or a producer fails to send a share of its messages
(`ClientRecordErrors`). Common causes are messages larger than the topic
allows, a client without permission, or a client that keeps reconnecting.
Your clients' logs say why each request failed.

See [messages refused as too large](troubleshooting/oversized-messages.md)
and [a producer's messages fail](troubleshooting/producer-errors.md).

## Postgres sources

These alerts are about a [Postgres source](postgres.md) and the database it
reads. Their thresholds are fixed.

### A source holds back a lot of history

The database keeps more than 20 GB of change history (WAL) that the source
has not read yet, for 30 minutes (`PostgresSourceRetainedWALHigh`). A
paused, failed or slow source makes it grow, on your database's disk.

- Check the source's page: is it paused, failed, or behind?
- Make sure `max_slot_wal_keep_size` is set, so that the history can never
  fill your disk. See
  [Keeping your database safe](postgres.md#keeping-your-database-safe).

### A source is behind its database

The changes the source writes to Kafka happened in the database more than
15 minutes ago, for 15 minutes (`PostgresSourceBehind`). The database
changes faster than the source can stream, often during a large batch
update. It usually catches up once the batch is done; if it keeps falling
further behind, contact us.

### Changeport can't reach a source's database

Changeport has not been able to read the source's replication slot for more
than 15 minutes (`PostgresSourceDatabaseUnreachable`), so nobody sees
whether the history it holds back grows.

- Check that the database runs, and still accepts connections from
  Changeport's addresses, which the source's page lists.
- Check that the role's password has not changed, and that the database's
  TLS certificate is still valid.

### A source has been paused for a day

The source has been paused for a day (`PostgresSourcePausedLong`). A paused
source keeps its place, so your database keeps every change since it
paused. Resume it, or delete it if you no longer need it.

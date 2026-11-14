# Alerts about your applications

Changeport watches how your applications use your cluster and tells you when
something looks wrong: a consumer group that falls behind or stops, clients
that are throttled, and requests that fail. Alerts show in the console as
they fire and clear, on the overview and on the topic or consumer group they
are about. Admins choose their thresholds, and where else they go: Slack,
PagerDuty or a webhook of their own.

These alerts are about your applications. Changeport's operators are paged
about the cluster itself, such as a broker that is down, and act on it; see
[runbook.md](runbook.md).

## The alerts

| Alert | Fires when | Default |
| --- | --- | --- |
| `ConsumerGroupLagHigh` | A consumer group's oldest unconsumed message on a topic is older than the threshold, for a minute | 10 minutes |
| `ConsumerGroupStuck` | A consumer group has not committed on a topic for longer than the threshold, while messages wait for it and keep arriving | 15 minutes |
| `ClientThrottled` | A client is throttled for longer than the threshold at a time, for 5 minutes | 100 ms |
| `TopicRequestErrors` | More than the threshold of a topic's produce and fetch requests fail, for 5 minutes | 5% |
| `ClientRecordErrors` | A producer fails to send more than the threshold of its records, for 5 minutes | 5% |

Each alert can be turned off. The errors threshold is shared by the two
error alerts. Settings take effect within a minute.

### A consumer group falls behind

The age of the oldest message a group has not consumed yet, which the
console's consumer groups card shows too. A group falls behind when its
consumers are slower than its producers, or not running.

- Check that the group's consumers run and are not restarting.
- Compare the topic's messages in with how fast the group consumes; a group
  with fewer consumers than the topic has partitions can add some.
- A burst of messages can put a healthy group behind for a while; raise the
  threshold if that is normal for the topic.

### A consumer group is stuck

The group has not committed offsets on the topic for longer than the
threshold, while messages wait for it and new ones arrive: its consumers are
stuck on a message, crashing on it before they commit, or gone.

- Look for a consumer that fails on the same message again and again.
- A group that no longer exists in your applications keeps its committed
  offsets; delete the group once you no longer need it.

### A client is throttled

The brokers delay a client's requests when it exceeds a quota. Clients
report the delay themselves (KIP-714), so it shows for clients of Kafka 3.7
and newer, and of librdkafka 2.5 and newer.

### Requests fail

The brokers refuse produce or fetch requests to a topic, or a producer fails
to send its records. Common causes are messages larger than the topic
allows, a client without permission, or a client that keeps reconnecting.
Your clients' logs say why each request failed.

## Where alerts go

Every alert shows in the console: at the top of the overview, as a badge on
the topic or consumer group it is about, and on the Alerts page, which the
sidebar counts them next to. Admins choose on the Alerts page where else
alerts go, and can choose all three:

- **Slack:** an incoming webhook URL (`https://hooks.slack.com/...`), which
  posts to the channel it was made for. Alerts post when they fire and when
  they resolve.
- **PagerDuty:** the routing key of an Events API v2 integration. Alerts
  raise incidents, and resolve them.
- **Webhook:** an HTTPS URL that receives
  [Alertmanager's webhook payload](https://prometheus.io/docs/alerting/latest/configuration/#webhook_config)
  as JSON, once when an alert fires and once when it resolves.

The console keeps these secrets where nobody can read them back, not even
the console: it shows only which receivers are set, with a hint such as the
webhook's host. To change one, enter it again.

## How it works

For operators:

- **Thresholds:** a customer's settings are its `AlertSettings`, named
  `alerts`, in its namespace; without them the defaults apply
  (`v1alpha1.DefaultAlertSettings`, which the CRD's defaults match). The
  provisioner exports each customer's thresholds as
  `changeport_alert_threshold{tenant, alert}`, and the rules in
  `deploy/base/alerting/rules/customers.yaml` compare with them, so one set
  of rules serves every customer. An alert that is off has no threshold.
- **Receivers:** the console writes each receiver's secret once into a
  Secret of type `changeport.io/alert-receiver` in the customer's namespace,
  which the console's admission policy allows it and nothing else, and
  names the Secret in the settings. Only the provisioner reads it.
- **Routing:** the provisioner writes Alertmanager's configuration, the
  Secret `alertmanager-generated` in `metrics`, from the base configuration
  (the ConfigMap `alertmanager-config`, with the operators' routes) and a
  route per customer with receivers. A customer's route matches only alerts
  for customers (`audience="customer"`) with its own `tenant`, so one
  customer's alerts never reach another's receivers; customers' alerts never
  reach the operators' Slack either. Alertmanager reloads the configuration
  within a minute of a change.
- **Alertmanager** may reach only DNS and the internet (its NetworkPolicy),
  so a customer's webhook cannot point it into the platform; the console
  also refuses webhooks that resolve to private addresses, and plain HTTP.
  On kind, the console's `-allow-private-receivers` lets the test webhook
  in, and the kind overlay lets Alertmanager reach it.
- **The console** reads the customer's active alerts from Alertmanager,
  always for the signed-in customer's tenant, and streams them to the
  browser as server-sent events (`GET /api/customer/alerts/stream`): the
  list at once, then again whenever it changes. A stream ends when the
  session token expires, and the browser opens a new one with a fresh
  token.
- `web/e2e/clerk/alerts.spec.ts` stops a consumer group on kind, and waits
  for its alert in the console and at a webhook, then for both to clear.

# Alerts about customers' applications

Changeport watches how customers' applications use their clusters and tells
them when something looks wrong: a consumer group that falls behind or
stops, clients that are throttled, requests that fail, and Postgres sources
that hold back their database. Alerts show in the console as they fire and
clear, and go to the customer's own Slack, PagerDuty or webhook. Admins
choose the thresholds of the alerts about their applications and their
Postgres sources.

What each alert means for the customer, and what to do about it, is in the
customer docs: [site/content/docs/alerts.md](../site/content/docs/alerts.md),
published at `/docs/alerts.html` on the site. Each customer alert's `docs`
annotation names its section there, relative to the docs' address, which
`internal/alerts`'s tests check; the console links it from the alert, and
customers' Slack messages and PagerDuty incidents link it too
(`CHANGEPORT_DOCS_URL`, below).

Changeport's operators are paged about the cluster itself, such as a broker
that is down, and act on it; see [runbook.md](runbook.md).

| Alert | Fires when | Default |
| --- | --- | --- |
| `ConsumerGroupLagHigh` | A consumer group's oldest unconsumed message on a topic is older than the threshold, for a minute | 10 minutes |
| `ConsumerGroupStuck` | A consumer group has not committed on a topic for longer than the threshold, while messages wait for it and keep arriving | 15 minutes |
| `ClientThrottled` | A client is throttled for longer than the threshold at a time, for 5 minutes | 100 ms |
| `TopicRequestErrors` | More than the threshold of a topic's produce and fetch requests fail, for 5 minutes | 5% |
| `ClientRecordErrors` | A producer fails to send more than the threshold of its records, for 5 minutes | 5% |
| `PostgresSourceBehind` | The changes a Postgres source writes happened in its database longer ago than the threshold, for 5 minutes | 15 minutes |
| `PostgresSourceRetainedWALHigh` | A source's slot keeps more WAL than the threshold in a database without `max_slot_wal_keep_size`, for 30 minutes | 20 GB |
| `PostgresSourceSlotFilling` | A source's slot keeps more than the threshold's share of what the database's `max_slot_wal_keep_size` allows, for 10 minutes | 50% |

Each alert can be turned off. The errors threshold is shared by the two
error alerts. Settings take effect within a minute. The Postgres sources'
rules are in `deploy/base/alerting/rules/postgres-sources.yaml`, with the
alerts whose thresholds are fixed:

- `PostgresSourceStalled` (for the customer) and
  `PostgresSourceConnectorStalled` (for operators): a running source has
  written no heartbeat for about 35 minutes (5 minutes without one, held
  for 30), while its slot keeps more and more WAL, and only while its
  customer's brokers are scraped, as without their metrics every source
  would look stalled. Its connector stopped reading without failing, as when it cannot
  open its replication connection and retries without end: Connect calls
  it running, its phase stays Streaming, and Debezium's own metrics, which
  `PostgresSourceBehind` reads, go with its task. A source writes its
  heartbeat to `changeport.cdc.heartbeat.<source>` every minute, which the
  brokers' per-topic metrics count; Connect's count of written records does
  not tell, as each retry writes a notification.
- `PostgresSourceConnectorBehind` (for operators): a source more than an
  hour behind, whatever the customer allows, before its slot nears the
  limit, where `PostgresSourceSlotNearLimit` pages at 80%.

## How it works

For operators:

- **Thresholds:** a customer's settings are its `AlertSettings`, named
  `alerts`, in its namespace; without them the defaults apply
  (`v1alpha1.DefaultAlertSettings`, which the CRD's defaults match). The
  provisioner exports each customer's thresholds as
  `changeport_alert_threshold{tenant, alert}`, and the rules in
  `deploy/base/alerting/rules/customers.yaml` and `postgres-sources.yaml`
  compare with them, so one set of rules serves every customer. An alert
  that is off has no threshold.
- **Receivers:** the console writes each receiver's secret once into a
  Secret of type `changeport.io/alert-receiver` in the customer's namespace,
  which the console's admission policy allows it and nothing else, and
  names the Secret in the settings. Only the provisioner reads it.
- **Links to the docs:** the console and the provisioner read the docs'
  address from `CHANGEPORT_DOCS_URL`. The console passes each alert's `docs`
  annotation on, and the web app joins the two. The provisioner puts the
  address into customers' Slack messages ("What to do") and PagerDuty
  incidents (a link), and refuses an address a template would read as its
  own. Webhooks get the annotation as it is. Without the address, nothing
  links the docs.
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

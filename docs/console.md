# The console

What the console does for customers and for Changeport's operators, page by
page: the customer console, and the operator console under `/operator` for
Changeport's staff. Both are one address for every customer:
`https://console.changeport.app` in prod, `https://console.dev.changeport.app`
in dev and `http://localhost:8088` on kind; a user's Clerk organization
picks the customer, and customers get no subdomains of their own
([terraform/README.md](../terraform/README.md#addresses)). The console's
end-to-end tests (`web/e2e`, `just ui-e2e`) exercise every page on kind.

## Customers

Customers sign in through Clerk.

The console is laid out as Linear is: a sidebar on a grey frame, and the
page on a panel beside it, headed by a bar with where the page sits, its
title and its main action, such as **Add source**. At the sidebar's top,
the organization's workspace menu leads to its Environments and Team, the
user's profile and signing out. Under it, each of the organization's
environments is a section with its cluster's pages, Overview, Sources and
Alerts. The environment shown is unfolded, and a folded one counts its
active alerts. Then come the whole organization's pages, Environments and
Team. The sidebar collapses to icons, from its foot or with ctrl-B (cmd-B
on a Mac), and remembers that. On phones it opens from the top bar's menu
button. Operators see the operator console's pages in it instead.

Changeport's mark at the sidebar's foot opens **Help**, as Linear's does.
It leads to the getting started guide of the environment shown, to support
by email when the console has an address for it (`CHANGEPORT_SUPPORT_EMAIL`,
see the runbook), and to the keyboard shortcuts. As in Linear, G then a
page's key goes to it: O the overview, S sources, C connect, A alerts, E
environments and T the team, or for operators F the fleet, C customers and R trial
requests. Cmd-/ (ctrl-/) lists them. Keys typed into a field, or while a
dialog or menu is open, are left alone.

The help menu also switches **dark mode** on and off; the console is light
unless it is on. The browser keeps the choice, for every tab, and applies it
before the page draws (a script in `web/index.html`, which the console's
Content-Security-Policy allows by its hash), so a dark console never flashes
white. Clerk's sign-in and profile take the same colors (its shadcn theme).

### Getting started

A new customer lands on a welcome page instead of the overview, while their
cluster is still being created or has no topics yet. It takes them from
their cluster to their own first message in four steps, and **Skip to the
overview** is always there. Once they finish or skip it, this browser
remembers, and the Connect page's "Open the getting started guide" brings
it back.

1. **Your cluster.** What a topic, a producer and a consumer group are, in
   plain words, and the cluster's progress, part by part, while it is being
   created.

2. **A topic**, created in the usual form with its guardrails, or one the
   customer has.

3. **Connect.** The download saves `client.properties` under the name the
   examples read it by. Then come the variables the examples read, with the
   bootstrap address, the user and the topic filled in and the password
   read from `client.properties`, and the examples of the
   [connection guide](../site/content/docs/connecting.md) in Python, Go and JavaScript (Node.js),
   which `hack/connecting-guide-test.sh` runs against kind.

4. **First message.** The customer runs their example, and the page shows
   their message as it arrives, from the topic's live messages, then the
   consumer group that read it back. Members, who cannot read messages,
   see it arrive by the topic's traffic.

The overview of a ready cluster, in the order a customer needs it: its
active alerts and what needs attention, which ask them to act; the brokers'
health, with the transactions open and how long the oldest has been;
throughput, with what changed meanwhile; how data flows from producers
through topics to consumer groups, which leads to the Connect page while
nothing flows yet; the topics with their retention, tiering, size and
traffic; and how far behind each consumer group is, in plain language, with
its lag over the last hour.

**Connect**, a page of its own under each environment in the sidebar, is how
apps connect: those that write and those that read alike, with the same
address and credentials, so it is not a kind of source. It shows the
bootstrap address, the security settings and the user, with
`client.properties` for admins (members are told to ask one), and the
connection guide's examples in every language it has, Python, Go and
JavaScript first as on the welcome page, for a topic the customer picks.
None is picked for them, as each example writes a test message, which the
topic's own consumers read too. Topics a Postgres source writes are not
offered, as the message would land among the database's changes. Until a
topic is picked, the examples write to `my-topic`; a cluster without topics
of its own links admins to create one and tells members to ask an admin.

Keeping the brokers ready and every partition in sync is Changeport's job,
so while they are, **Health** says so in a line, with the open transactions,
which are the customer's; its counts and brokers are a click away. Once no
broker runs, a broker is not ready, a partition is out of sync or offline,
or the cluster does not have exactly one active controller, it shows them by
itself, whatever the viewer chose while all was well, unless they close
them; its line names a controller problem too.

**What needs attention** lists the common Kafka problems the console found
in the cluster's topics, consumer groups and producers over the last 15
minutes, the most pressing first, each with the numbers that show it and
what to do: a group that can't keep up with its topic, one nobody consumes,
one about to lose messages to retention, a partition that takes more than
its share, producers whose records fail. A topic's and a group's
pages show what is about them, and the data flow map marks them. Each
finding's **Learn more** opens its page of the customer docs in a new tab,
as an alert's **What to do** and a topic change's warnings do
([site.md](site.md#the-customer-docs)).
[diagnosis.md](diagnosis.md) describes every rule.

Each topic and consumer group has a health level from them: **Healthy**,
**Needs attention** with warnings, **At risk** with anything critical. The
topics and consumer groups tables show it in a column whose header sorts
them worst first, and a topic's and a group's pages show it beside their
names, with a strip of the last 24 hours and the diagnoses behind it.

The overview's data flow map shows how data moves through the cluster now:
the producers on the left, by client ID and Kafka user, the topics they
write in the middle, and the consumer groups that read them on the right,
over the last 5 minutes. Edges grow with the bytes per second along them,
and their dashes flow while data moves. Each group says how many members it
has and how far behind it is: amber once it is a minute behind, red while
an alert is about it, as `search-indexer` is here. Topics and groups that
need attention carry a mark, whose reasons show on pointing at them.
Pointing at a node picks out its edges, and topics and groups lead to their
pages.

Producers show once they report their own metrics (KIP-714), as clients of
Kafka 3.7 and newer, and of librdkafka 2.5 and newer, do; what the brokers
receive that no producer reports shows as clients without telemetry, so no
traffic goes missing. Producers that share a client ID and Kafka user show
as one. The platform's own clients, topics and groups are left out, as are
topics without traffic, and past the 40 busiest. Consumers are matched to
their groups by the groups' members, as Kafka lists them, and each member
counts, even when several share a client ID. On a phone the map scrolls
sideways within its card.

A topic's page and a consumer group's, from the overview's names: their
health over the last 24 hours and what needs attention; the topic's
settings, its traffic, its producers and the groups that read it; the
group's lag on each topic it reads. Every graph marks what changed meanwhile, numbered,
with the changes listed under it in plain words: topics created or
reconfigured, rebalances, broker restarts, throttling (see
[timeline.md](timeline.md)).

### Delivery guarantees

A topic's **Producers** are those Kafka keeps state for on its partitions:
the ones that write idempotently or in transactions, over the last day
(`producer.id.expiration.ms`). Kafka knows them by an ID it gives each, not
by client ID, so a transactional producer shows by its `transactional.id`,
a Postgres source's connector as its source, and the others by that ID; each says whether it has a transaction open on
the topic, how many partitions it wrote to, and the timestamp of its latest
message, which is the producer's own clock: a replay's or a stream
processor's may be old. Every transactional producer shows, and the 10
idempotent ones with the latest timestamps, with how many more there are:
short-lived producers each get an ID of their own. Producers that are not
idempotent never show, as Kafka keeps no state for them: when a busy topic
has no idempotent producer writing at all, the diagnoses note it. Once a
transaction has been open on the topic for a minute, the card warns how
many messages it holds back from `read_committed` consumers: about those
after a partition's last stable offset. Healthy transactional producers
hold back a moment's messages all the time.

The overview's health says how many transactions are open, and how long
the oldest has been, with a warning once that is a minute. It waits at
most 3 seconds for Kafka, and leaves them out otherwise. The diagnoses find a
transaction that holds consumers back for longer than a minute, a hanging
one that its coordinator lost and Kafka never aborts, and producers that
aborted transactions (see [diagnosis.md](diagnosis.md)). Kafka counts
aborts nowhere, neither on the brokers nor in the clients' metrics, so the
console shows no abort rate: it names the producers that spent time
aborting instead.

The console asks Kafka directly, on each request: open transactions with
`ListTransactions` and `DescribeTransactions` as its own user, which may
describe transactional IDs and nothing more; the producers with
`DescribeProducers`, which Kafka authorizes as reading a topic, as the
read-only user of the topic tail, which returns no messages here. The
connection guide's examples set `acks=all` and idempotence outright, and
show exactly once from topic to topic
([connecting.md](../site/content/docs/connecting.md#delivery-guarantees)).

### Changing a topic

Admins add partitions to a topic, or change how long it keeps messages,
from **Edit** on its settings, within the same guardrails as creating one:
up to the profile's partitions and the cluster's replica budget, and the
retention presets. Partitions can be added, never removed, as Kafka cannot
take them away; a compacted topic has no retention to change, and a
Postgres source's topics keep the settings their source gives them.

Before anything is saved, the dialog says what the change does to the
consumers reading the topic:

- **A shorter retention** deletes older messages for good, a log segment at
  a time: within minutes on a busy topic, and on a quiet one once its
  current segment closes, after up to 7 days. A consumer group further
  behind than the new retention loses the messages it has not read yet, and
  the save button says so; one at least half as far behind is warned it
  would be close. While the groups' lag is not known, the dialog counts that
  as a possible loss.
- **More partitions** send a key's new messages to another partition than
  its earlier ones, so for a while consumers may read them out of order.
  Consumers find new partitions within 5 minutes, and those with
  `auto.offset.reset=latest` skip what reaches them before they do.

Every change is audited (`topic changed`, with the partitions and retention
before and after), and shows on the topic's timeline with who made it.

### A topic's messages

Admins read a topic's messages on its page: up to its last 50, then new ones as
they arrive, at most 20 a second, with how many more were skipped. Each
shows its partition, offset, time, key and value; opened, the whole of it
and its headers. Values show as indented JSON, as text, or as hex when
binary, up to 8 KiB each. A Postgres source's topics hold Avro, which shows
as JSON, decoded with the schemas of the source's registry. Like a
`read_committed` consumer, the tail shows committed messages only: never
what an aborted transaction wrote, and a transaction's messages once it
commits. Transactions' markers take offsets of their own, so the offsets
it shows skip them, and the last 50 offsets of a topic written in
transactions, such as a Postgres source's, hold fewer than 50 messages.

The console reads the messages as a Kafka user of their own,
`changeport-tail`, which may read topics and nothing else: it neither
writes nor uses a consumer group, so it never moves a group's offsets. Only
admins may read messages; members see why not. Every tail is audited when
it starts and when it ends, with who read which topic, never with what the
messages hold, and Changeport keeps no message: they pass through on their
way to the browser. An organization runs at most three tails at once.

### Moving a consumer group

Admins change where a consumer group reads from **Change position** on its
page: from a point in time, from the oldest message its topics keep, or
from now, skipping what it has not read, on all the topics it reads or
some of them. The group itself moves, or a new
group is created there to **replay** the messages to a service started with
it, leaving the group where it is. A replay is how a service rebuilds its
state, or a fixed bug's output is made again, without touching the
consumers running in production.

Before anything changes, the dialog says per topic how many messages the
group reads again or skips, and from when, worked out on the server from
the group's committed offsets and Kafka's offsets at that time. Offsets
count transaction markers and messages compaction removed too, so on such
topics the numbers are about right rather than exact. It says how many
messages come back from tiered storage, older than the brokers keep on
disk, and how long that takes at least at the brokers' limit for reading it
back (below). It warns that the service handles the messages again, with
whatever they trigger, and that skipped messages are never read.

Kafka moves a group only while it has no consumers, as they keep their own
position: the dialog names the ones running and waits until they stop,
which a replay does not need. Moving the group itself takes its name typed.
After a replay the dialog shows the `group.id` to start the service with.
Platform groups cannot be moved, and a replay's group must be new.

Every move and replay is audited (`consumer group moved` and `consumer
group replayed`, with the position, the topics and the messages read again
and skipped), and shows on the group's timeline with who made it. For the
hour after it, the diagnoses explain the group's lag by the move, and its
having no consumers yet is a note rather than a warning
([diagnosis.md](diagnosis.md)).

The console moves groups as a Kafka user of its own, `changeport-offsets`.
Kafka authorizes committing a group's offsets as reading the group and its
topics, which the console's own user may not: it describes groups only, so
a compromise of the console's main user still cannot move one. The offsets
user is used for nothing but commits.

Reading history back from tiered storage is capped per broker
(`remote.log.manager.fetch.max.bytes.per.second`: 8 MiB/s on the
production profile, 2 MiB/s on the dev profiles) and runs on the remote
log reader's own threads, for every client, so a replay or any read of old
messages leaves the brokers to the consumers of recent ones, which read
from local disk.

Creating a topic applies the guardrails as the admin types. Deleting a
topic names the consumer groups still reading it and takes the topic's
name typed.

Outside their overview, customers find the operator console closed to them,
a page for an unknown address, and, in an organization without a cluster,
what to do next.

### Environments

An organization has a cluster per environment, such as production, staging and dev. The console shows one at a time, under `/e/<environment>/` in its URL, so a link opens the environment it was copied in. The sidebar lists each environment with its pages, so moving to another one is a click on its section, then on the page. Everything on a cluster's pages is the environment's own: its connection and credentials, topics, consumer groups, sources and alerts. The team and trials belong to the whole organization. A page from before environments, such as `/topics/orders`, opens in the default environment.

The Environments page, from the sidebar's Environments link, the settings button beside "Your environments", or the workspace menu, lists them. Each shows its profile, whether its cluster is ready, starting or being removed, which one is the default, and which are protected. Members can see the page; admins change it:

- **Add environment** names the new one, suggesting production, staging or dev when the organization lacks it, and picks its profile and deletion protection, which is on for production unless chosen otherwise. An organization has at most 5 environments (`-max-environments` on the console). Operators add more from the operator console. A trial is one environment.
- **Rename** changes only its links. Its cluster, connection and credentials stay as they are, so its clients keep working.
- **Make it the default** sets the environment the console opens first. A browser that used another environment last opens that one instead.
- **Protect** turns deletion protection on. Turning it off takes the environment's name typed.
- **Remove** takes the environment's name typed, and deletes its cluster with its data. It is refused while deletion protection is on, and for the organization's last environment: closing an account goes through Changeport.

### Team

The organization's members and pending invitations, from the sidebar's
Team link or the workspace menu. A person belongs to one
organization, their company's, so an invitation to an address that
belongs, or is invited, to another organization is refused, as is an
operator's invitation of such an address as a new customer's admin, and a
trial for someone whose organization has its cluster already. Admins
invite colleagues as admins or members, revoke invitations, change roles
and remove members; the organization keeps at least one admin. Members
see the team and ask an admin to change it. Someone who ends up in several
organizations anyway sees only that they do, until support sorts it out.

## Trials

Someone new signs up at `/trial`, which shows Clerk's sign-up; sign-in links
to it.

Signed in without an organization, or in one without a cluster, home points
to the trial.

The request: the company, which names the cluster's organization, the
requester's role, what they want Kafka for, rough peak throughput and
retention, and when they would like to start. The form checks what the
console's API checks before sending.

The request waits for an operator, and the page moves on by itself once
they decide. Approved, it switches the requester to their new organization,
whose admin they are, and opens its cluster as it gets ready. Declined, it
shows the operator's reason and when they may ask again, a week later.

An approved customer is on a 14-day trial, which every page of its console
counts down at its top; in the last three days it warns that the
cluster then goes.

## Usage

Each environment's **Usage** page shows the data its cluster processed over
the last 30 days: what producers wrote to its topics (data in) and what
consumers read from them (data out), each byte once, as the brokers count
them per topic. Replication does not count, as the brokers count what
followers fetch to copy a topic apart, nor do the platform's own topics
(Kafka's, Changeport's, Strimzi's and Kafka Connect's internal ones). A
Postgres source's topics count like any other: the source writes them.
Members see it as admins do.

- **Totals:** the 30 days' data in and out, and what the topics store now,
  on the brokers' disks (one copy) and in tiered storage.
- **Per day:** data in and out per day, in UTC, today so far last.
- **Topics:** each topic's data in and out over the 30 days, the most first,
  with what it stores now.

The organization's Environments page shows what each environment processed
over the 30 days, linking to its Usage page, and operators see each
customer's on the operator console's Customers page.

What a cluster processes while the metrics store is down is not recorded,
and usage leaves it out rather than guess; a topic created meanwhile counts
from its first byte. Thirty days is as long as the metrics store keeps the
brokers' counters.
Billing will add usage by calendar month, kept for longer, with its rates;
there are no prices yet (docs/PROJECT_PLAN.md).

## Alerts

Alerts about the customer's own applications and Postgres sources (see
[alerts.md](alerts.md)), under Alerts in the sidebar, which counts the active
ones. Admins set each alert's threshold, in two groups, the applications'
and the sources', and where alerts go: Slack, PagerDuty or a webhook. A
receiver's secret is never shown again once entered, only a hint.

An alert shows at the top of the overview and as a badge on the topic or
group it is about, as it fires and without a reload, and goes once it
clears. The Alerts page lists the active alerts, the thresholds, and where
alerts go; members see them, and only admins change them.

## Postgres sources

A customer's Postgres sources, under Sources in the sidebar (see
[cdc.md](cdc.md)). Admins add one through the wizard, and run its SQL as
the database's owner; on kind, a Postgres stands in for a customer's
database. Members of the organization see everything, and change nothing.

The wizard takes six steps:

1. **Where the database is,** with the addresses to allow in and how to
   check its certificate; Changeport checks that it can reach it.
2. **The role** Changeport logs in as, with the password the console chose,
   shown only here.
3. **The tables,** each with its topic. Those without a primary key
   Changeport can use are listed, disabled, with the reason.
4. **The SQL** that grants and publishes the tables.
5. **Every check,** each with what it found or what to do.
6. **Starting the source,** which copies each table's existing rows, then
   streams every change.

Adding tables: Changeport's source controller lists the database's tables,
as the console cannot log in to it, and the new tables' rows are copied once
their SQL has run.

Deleting a source takes its name typed, and shows the SQL that removes
Changeport's role and publication once it is gone. Its topics stay.

## Operators

Operators land on the **Fleet**: every customer's cluster on one page, the
worst first, with how many are down, degraded and healthy at the top. Each
cluster shows its state and why, in plain words: its brokers by the active
controller's count, its partitions out of sync, its fullest volume, the
Kafka version it runs, with a note when the platform runs another, and the
operators' alerts firing for it with their runbook links, and how many of
the customer's own alerts fire. The operators' alerts about the platform
itself show above the clusters. The page asks again every 30 seconds.

The console reads the fleet at once: the metrics with fleet-wide queries by
tenant (`metrics.FleetQuery`), which are kept apart from the queries scoped
to one customer so that no customer's request can reach them, every active
alert from Alertmanager, and each cluster's Kafka version from its Kafka
resource, which the template lets the console read. It keeps the view for
15 seconds; a source that does not answer leaves its part out. The
runbook says what each state means
([runbook.md](runbook.md#where-to-look)). A customer's page shows the same
health for its cluster.

**Customers** lists every customer with a dot per part of its cluster, its
profile, its trial and what it processed over the last 30 days. A customer
with several environments, each a cluster of its own, heads them with its
name, their number and what they processed together, and lists them under
it: production first, then by name, and those being removed last. (Its
console puts the environment it opens by default first, which this list does
not know.) Provisioning
one takes its name, which also names its Clerk organization, its profile,
deletion protection and an optional invitation for its first admin; the new
customer's page then follows its cluster as it gets ready. Deprovisioning
takes the customer ID typed, and keeps the Clerk organization; a protected
customer cannot be deprovisioned.

Trial requests, the pending ones first, with a count of them in the
navigation. Approving creates the organization, makes the requester its
admin and provisions the cluster, on dev unless the operator picks another
profile and without deletion protection; declining needs a reason, which the
requester sees.

The customers table shows when each trial ends. On a customer's page,
operators extend its trial, to a new end at most 90 days away, or convert
the customer, which ends its trial so that its cluster stays. At its end, a
trial's customer is deprovisioned.


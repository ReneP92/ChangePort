# The console

Every screen of the first console, as `just ui-screenshots` captures it on
kind: an operator provisions Acme Corp and Globex through the operator
console, Acme's admin Ada creates topics and adds a Postgres source,
producers and consumer groups run for a few minutes, and Acme is
deprovisioned again. Each screen also has a
phone version, under the same name ending in `-phone.png`, and both are
captured at twice the pixels, as a retina screen shows them, so that they
stay sharp here and on the landing site ([site.md](site.md)). To capture them
again after changing the console, run `just dev-up` with `.env` filled in,
then:

```bash
just ui-screenshots
```

## Customers

Signing in, through Clerk.

![Sign-in](screenshots/01-sign-in-desktop.png)

### Getting started

A new customer lands on a welcome page instead of the overview, while their
cluster is still being created or has no topics yet. It takes them from
their cluster to their own first message in four steps, and **Skip to the
overview** is always there. Once they finish or skip it, this browser
remembers, and the Connection card's "Open the getting started guide"
brings it back.

1. **Your cluster.** What a topic, a producer and a consumer group are, in
   plain words, and the cluster's progress, part by part, while it is being
   created.

   ![The welcome page while the cluster is created](screenshots/02-customer-starting-desktop.png)

2. **A topic**, created in the usual form with its guardrails, or one the
   customer has.

   ![Creating a topic](screenshots/47-welcome-topic-desktop.png)

3. **Connect.** The download saves `client.properties` under the name the
   examples read it by. Then come the variables the examples read, with the
   bootstrap address, the user and the topic filled in and the password
   read from `client.properties`, and the examples of the
   [connection guide](connecting.md) in Python, Go and JavaScript (Node.js),
   which `hack/connecting-guide-test.sh` runs against kind.

   ![Connecting an app](screenshots/48-welcome-connect-desktop.png)

4. **First message.** The customer runs their example, and the page shows
   their message as it arrives, from the topic's live messages, then the
   consumer group that read it back. Members, who cannot read messages,
   see it arrive by the topic's traffic.

   ![The first message arriving](screenshots/49-welcome-first-message-desktop.png)

The overview of a ready cluster: its active alerts; how to connect, with
`client.properties` for admins; the brokers' health; throughput, with what
changed meanwhile; how data flows from producers through topics to consumer
groups; the topics with their retention, tiering, size and traffic; and how
far behind each consumer group is, in plain language, with its lag over the
last hour.

![The overview](screenshots/03-overview-desktop.png)

The overview's data flow map shows how data moves through the cluster now:
the producers on the left, by client ID and Kafka user, the topics they
write in the middle, and the consumer groups that read them on the right,
over the last 5 minutes. Edges grow with the bytes per second along them,
and their dashes flow while data moves. Each group says how many members it
has and how far behind it is: amber once it is a minute behind, red while
an alert is about it, as `search-indexer` is here. Pointing at a node picks
out its edges, and topics and groups lead to their pages.

Producers show once they report their own metrics (KIP-714), as clients of
Kafka 3.7 and newer, and of librdkafka 2.5 and newer, do; what the brokers
receive that no producer reports shows as clients without telemetry, so no
traffic goes missing. Producers that share a client ID and Kafka user show
as one. The platform's own clients, topics and groups are left out, as are
topics without traffic, and past the 40 busiest. Consumers are matched to
their groups by the groups' members, as Kafka lists them. On a phone the
map scrolls sideways within its card.

A topic's page and a consumer group's, from the overview's names: the
topic's settings, its traffic and the groups that read it; the group's lag
on each topic it reads. Every graph marks what changed meanwhile, numbered,
with the changes listed under it in plain words: topics created or
reconfigured, rebalances, broker restarts, throttling (see
[timeline.md](timeline.md)).

![A topic](screenshots/17-topic-desktop.png)

![A consumer group](screenshots/18-consumer-group-desktop.png)

### A topic's messages

Admins read a topic's messages on its page: its last 50, then new ones as
they arrive, at most 20 a second, with how many more were skipped. Each
shows its partition, offset, time, key and value; opened, the whole of it
and its headers. Values show as indented JSON, as text, or as hex when
binary, up to 8 KiB each. A Postgres source's topics hold Avro, which shows
as JSON, decoded with the schemas of the source's registry. Here Ada reads
the orders of the source `shop`.

![A topic's messages](screenshots/19-topic-messages-desktop.png)

The console reads the messages as a Kafka user of their own,
`changeport-tail`, which may read topics and nothing else: it neither
writes nor uses a consumer group, so it never moves a group's offsets. Only
admins may read messages; members see why not. Every tail is audited when
it starts and when it ends, with who read which topic, never with what the
messages hold, and Changeport keeps no message: they pass through on their
way to the browser. An organization runs at most three tails at once.

Creating a topic within the guardrails, which the dialog applies as Ada
types.

![Creating a topic](screenshots/04-create-topic-desktop.png)

![The guardrails](screenshots/05-create-topic-guardrails-desktop.png)

Deleting a topic names the consumer groups still reading it and takes the
topic's name typed.

![Deleting a topic](screenshots/06-delete-topic-desktop.png)

What customers see outside their overview: the operator console is closed
to them, an unknown page, and an organization without a cluster.

![Operators only](screenshots/07-operators-only-desktop.png)

![No such page](screenshots/08-not-found-desktop.png)

![No cluster](screenshots/09-no-cluster-desktop.png)

### Environments

An organization has a cluster per environment, such as production, staging and dev. The console shows one at a time, under `/e/<environment>/` in its URL, so a link opens the environment it was copied in. The header's switcher moves between them, landing on the chosen one's overview. Everything on a cluster's pages is the environment's own: its connection and credentials, topics, consumer groups, sources and alerts. The team and trials belong to the whole organization. A page from before environments, such as `/topics/orders`, opens in the default environment.

![Switching environments](screenshots/51-environments-desktop.png)

The Environments page, from the header's Environments link or the switcher's **Manage environments**, lists them. Each shows its profile, whether its cluster is ready, starting or being removed, which one is the default, and which are protected. Members can see the page; admins change it:

- **Add environment** names the new one, suggesting production, staging or dev when the organization lacks it, and picks its profile and deletion protection, which is on for production unless chosen otherwise. An organization has at most 5 environments (`-max-environments` on the console). Operators add more from the operator console. A trial is one environment.
- **Rename** changes only its links. Its cluster, connection and credentials stay as they are, so its clients keep working.
- **Make it the default** sets the environment the console opens first. A browser that used another environment last opens that one instead.
- **Protect** turns deletion protection on. Turning it off takes the environment's name typed.
- **Remove** takes the environment's name typed, and deletes its cluster with its data. It is refused while deletion protection is on, and for the organization's last environment: closing an account goes through Changeport.

![The environments page](screenshots/52-environments-page-desktop.png)

![Adding an environment](screenshots/53-add-environment-desktop.png)

![Removing an environment](screenshots/54-remove-environment-desktop.png)

### Team

The organization's members and pending invitations, from the header's
Team link or the organization's name. A person belongs to one
organization, their company's, so an invitation to an address that
belongs, or is invited, to another organization is refused, as is an
operator's invitation of such an address as a new customer's admin, and a
trial for someone whose organization has its cluster already. Admins
invite colleagues as admins or members, revoke invitations, change roles
and remove members; the organization keeps at least one admin. Members
see the team and ask an admin to change it. Someone who ends up in several
organizations anyway sees only that they do, until support sorts it out.

![The team](screenshots/50-team-desktop.png)

## Trials

Someone new signs up at `/trial`, which shows Clerk's sign-up; sign-in links
to it.

![Signing up for a trial](screenshots/32-trial-sign-up-desktop.png)

Signed in without an organization, or in one without a cluster, home points
to the trial.

![No organization](screenshots/33-no-organization-desktop.png)

The request: the company, which names the cluster's organization, the
requester's role, what they want Kafka for, rough peak throughput and
retention, and when they would like to start. The form checks what the
console's API checks before sending.

![Requesting a trial](screenshots/34-trial-request-desktop.png)

![What is missing](screenshots/35-trial-request-problems-desktop.png)

The request waits for an operator, and the page moves on by itself once
they decide. Approved, it switches the requester to their new organization,
whose admin they are, and opens its cluster as it gets ready. Declined, it
shows the operator's reason and when they may ask again, a week later.

![A pending request](screenshots/36-trial-pending-desktop.png)

![A declined request](screenshots/41-trial-declined-desktop.png)

An approved customer is on a 14-day trial, which every page of its console
counts down under the header; in the last three days it warns that the
cluster then goes.

![A customer on a trial](screenshots/42-customer-trial-desktop.png)

## Alerts

Alerts about Acme's own applications (see [alerts.md](alerts.md)), under
Alerts in the header, which counts the active ones. Ada wants to know
within a minute when a consumer group falls behind, and sends alerts to a
webhook of hers too: on kind, the alert receiver. The secret she enters is
never shown again, only a hint.

![A webhook for alerts](screenshots/31-alert-webhook-desktop.png)

Once `search-indexer` has been more than a minute behind on `orders`, the
alert shows at the top of the overview and as a badge on the group, as it
fires and without a reload, and goes once it clears. The Alerts page lists
the active alerts, the thresholds, and where alerts go; members see them,
and only admins change them.

![The alerts](screenshots/30-alerts-desktop.png)

## Postgres sources

A customer's Postgres sources, under Sources in the header (see
[cdc.md](cdc.md)). Ada adds one through the wizard, against the Postgres on
kind that stands in for a customer's database, and runs its SQL as the
database's owner would. Members of the organization see everything, and
change nothing.

![No sources yet](screenshots/20-sources-empty-desktop.png)

The wizard's six steps. First, where the database is, with the addresses to
allow in and how to check its certificate; Changeport checks that it can
reach it.

![Connecting](screenshots/21-source-connect-desktop.png)

Then the role Changeport logs in as, with the password the console chose,
shown only here.

![The role](screenshots/22-source-role-desktop.png)

The tables, each with its topic. Those without a primary key Changeport can
use are listed, disabled, with the reason.

![Choosing tables](screenshots/23-source-tables-desktop.png)

After the SQL that grants and publishes the tables, every check, each with
what it found or what to do. The last step starts the source.

![Verifying](screenshots/24-source-verify-desktop.png)

The source copies each table's existing rows, then streams every change.

![A source starting](screenshots/25-source-starting-desktop.png)

![A source streaming](screenshots/26-source-desktop.png)

Adding tables: Changeport's source controller lists the database's tables,
as the console cannot log in to it, and the new tables' rows are copied once
their SQL has run.

![Adding tables](screenshots/27-source-add-tables-desktop.png)

Deleting a source takes its name typed, and shows the SQL that removes
Changeport's role and publication once it is gone. Its topics stay.

![Deleting a source](screenshots/28-source-delete-desktop.png)

![The sources](screenshots/29-sources-desktop.png)

## Operators

Provisioning a customer: its name, which also names its Clerk organization,
its profile, deletion protection and an optional invitation for its first
admin.

![Provisioning a customer](screenshots/10-operator-provision-desktop.png)

The new customer's page follows its cluster as it gets ready.

![A customer starting](screenshots/11-operator-customer-starting-desktop.png)

Every customer, with a dot per part of its cluster.

![Every customer](screenshots/12-operator-customers-desktop.png)

![A customer ready](screenshots/13-operator-customer-ready-desktop.png)

A protected customer cannot be deprovisioned.

![A protected customer](screenshots/14-operator-customer-protected-desktop.png)

Deprovisioning takes the customer ID typed; the Clerk organization is kept.

![Deprovisioning](screenshots/15-operator-deprovision-desktop.png)

![A customer being deleted](screenshots/16-operator-customer-deleting-desktop.png)

Trial requests, the pending ones first, with a count of them in the
navigation. Approving creates the organization, makes the requester its
admin and provisions the cluster, on dev unless the operator picks another
profile and without deletion protection; declining needs a reason, which the
requester sees.

![Trial requests](screenshots/37-operator-trial-requests-desktop.png)

![Approving a trial](screenshots/38-operator-trial-approve-desktop.png)

![Declining a trial](screenshots/39-operator-trial-decline-desktop.png)

![Decided requests](screenshots/40-operator-trial-decided-desktop.png)

The customers table shows when each trial ends. On a customer's page,
operators extend its trial, to a new end at most 90 days away, or convert
the customer, which ends its trial so that its cluster stays. At its end, a
trial's customer is deprovisioned.

![Customers on trials](screenshots/43-operator-customers-trial-desktop.png)

![A customer's trial](screenshots/44-operator-customer-trial-desktop.png)

![Extending a trial](screenshots/45-operator-trial-extend-desktop.png)

![Converting a customer](screenshots/46-operator-trial-convert-desktop.png)

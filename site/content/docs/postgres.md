# Postgres sources

A Postgres source streams the tables you pick from your database into
topics, one per table: first the rows already there, then every insert,
update and delete as it happens. This is change data capture, and it needs
no changes to your application.

## What you need

- **PostgreSQL 14 or newer**, with logical replication on
  (`wal_level = logical`). Managed databases call it something else; the
  console says what, for your provider.
- **A limit on how much change history a source may hold back**
  (`max_slot_wal_keep_size`), such as 50 GB. Without it, a source that stops
  reading can fill your database's disk. The console warns when it is not
  set.
- **A free replication slot and WAL sender** for each source
  (`max_replication_slots`, `max_wal_senders`).
- **Your database reachable over TLS** from Changeport's fixed addresses,
  which the console lists, so that you can allow just those in.
- **Tables with a primary key** of integer, numeric, text, varchar, char,
  uuid or date columns. Changeport copies a table in chunks by its key, and
  keys messages by it. The console lists other tables, with why they can't
  be picked.

The console knows the steps for Amazon RDS and Aurora, Google Cloud SQL,
Azure Database for PostgreSQL, Supabase, Neon and plain PostgreSQL.

## Adding a source

Admins add a source from **Sources** in the console. The wizard takes six
steps:

1. **Connect:** your database's host, port and name, and how to check its
   certificate. Changeport checks it can reach it.
2. **The role:** the SQL that creates the role Changeport logs in as. It
   may replicate and nothing else, with a password the console chose and
   shows only here. You run the SQL yourself.
3. **Tables:** the tables to stream, each with the topic it goes to.
4. **Grant and publish:** the SQL that lets the role read those tables and
   publishes them for replication. You run it as the tables' owner.
5. **Verify:** every check, each with what it found or what to do.
6. **Start.**

Changeport never runs SQL that changes your database: you run every
statement, and can read it first. It copies tables with read-only queries,
without locks, and without holding a transaction open long enough to hold
back vacuum.

## What arrives in Kafka

Each table gets the topic `<source>.<schema>.<table>`, such as
`shop.public.orders`. Each message is one row's change, keyed by the row's
primary key, so a row's changes stay in order.

Messages are Avro. Their schemas are in your schema registry, at the
address the console shows, which takes the same user name and password as
your cluster. Point your Avro deserializer's `schema.registry.url` at it.

A source's topics keep its retention, 7 days unless you chose another when
adding it. You can't delete them while the source keeps them.

Each change arrives at least once: while a table is copied, a row that
changes meanwhile may arrive twice. Consumers that upsert by key see each
row once. See [Delivery guarantees](delivery-guarantees.md).

## Adding and removing tables

Add tables from the source's page: the console shows the SQL that grants
and publishes them, and copies their rows once it has run. A removed table
stops streaming, and its topic stays, as an ordinary topic.

## Keeping your database safe

Your database keeps the change history (write-ahead log, WAL) that a source
has not read yet, on its own disk. A source that stops reading makes it
grow. The console shows how much each source holds back, and alerts you:

- **Pausing a source** keeps its place: it catches up from where it
  stopped when it runs again. Meanwhile your database keeps every change,
  so don't pause for long. After a day, an alert says so.
- **A source that falls behind** or **holds back a lot of history** gets
  an alert, as does **a database Changeport can't reach**. See
  [Alerts](alerts.md#postgres-sources).
- **The limit you set** (`max_slot_wal_keep_size`) protects your disk:
  past it, your database drops the source's slot rather than fill up. The
  source then fails and says why, and needs a new copy of its tables to
  start again. Changeport's support sets that up with you.

Neon removes replication slots that are unused for about 40 hours, so a
source paused that long loses its slot in the same way.

## Deleting a source

Deleting a source takes its name typed. Changeport drops its replication
slot, so that your database stops keeping history for it, and the console
shows the SQL that removes the role and the publication. The source's
topics stay. If Changeport can't reach your database to drop the slot,
the console shows the SQL to drop it yourself.

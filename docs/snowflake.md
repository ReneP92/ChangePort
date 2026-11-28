# Snowflake destination

A customer connects their Snowflake account in the console and picks tables
of one of their Postgres sources ([cdc.md](cdc.md)). Each table then lands in
a Snowflake table of its own, merged by primary key, so that the warehouse
table holds what the source table holds: every insert, update and delete,
exactly once. This is the destination half of change data capture; the
source half writes each table's changes into a topic, and the destination
reads those topics.

It is tracked in the
[Snowflake destination milestone](https://github.com/ReneP92/changeport/milestone/12),
pulled forward from Phase 9 for the first CDC pair, Postgres to Snowflake.
This document is the design; the decisions behind it are in
[PROJECT_PLAN.md](PROJECT_PLAN.md).

## What the customer does

1. **Pick a source and its tables:** on a source's page, adds a Snowflake
   destination, and picks which of the source's tables to deliver.
2. **Connect:** enters the account identifier
   (`<organization>-<account>`), and the database, schema and warehouse to
   use. The console suggests a new schema named after the source and a
   warehouse of Changeport's own, and shows what each choice costs (see
   [Cost](#cost)).
3. **Run our SQL:** an account admin runs the SQL the console shows. It
   creates a role and a service user for the destination, with the public
   half of a key pair Changeport generated and no password; gives the role
   the warehouse, the schema and nothing else; and gives the user a network
   policy of its own that allows only Changeport's outbound IP addresses. A
   user's network policy takes the place of the account's for that user, so
   the account's, if it has one, stays as it is.
4. **Choose how often:** picks how often the tables catch up, from about a
   minute to once a day, with the credits each choice uses a month.
5. **Verify and start:** the console checks everything below, then starts.
   Each table's rows arrive first, then every change, at the chosen pace.

The warehouse tables are the customer's: Changeport creates them, adds
columns as the source's tables gain them, and merges into them, but never
drops them. A destination that is deleted leaves them in place.

## Requirements on the customer's account

- Any Snowflake edition, cloud and region.
- An account admin, or a user with `SECURITYADMIN` and `SYSADMIN`, to run the
  setup SQL once.
- A warehouse the role may use. Changeport's suggestion is one of its own,
  extra small, suspended after a minute without queries, so that its credits
  are plainly Changeport's.
- Network policies, which every edition has.

## How it runs

### The sink

- **One sink per customer:** a Deployment, `changeport-sink`, of one replica,
  on the customer's dedicated nodes, rendered from the template like the
  Kafka Connect worker. It exists only while the customer has a
  destination, and serves all of the customer's destinations and tables,
  each destination as a Snowflake user of its own, which nothing else signs
  in as but the destination's checks.
  - One replica, replaced rather than rolled, so that two sinks never write
    one table at once.
  - Our own engine in Go (`cmd/sink`), not Snowflake's Kafka connector. The
    connector lands raw rows in a table and cannot record the offsets it
    consumed in the transaction that merges them, so exactly-once results
    would need a second engine anyway.
- **Reading:** as the mTLS user `changeport-sink`, which may read the topics
  of the source tables its destinations deliver, and use the consumer groups
  `changeport.sink.<destination>`, and nothing else.
  - `read_committed`, as the source writes each poll in a transaction.
  - It decodes each change through the customer's schema registry, with the
    registry client the console's topic tail uses (`internal/tail`).
- **Catching up:** at the destination's pace, the sink reads each of its
  tables' topics from the table's offset to the topic's end, in batches of up
  to about 100,000 changes, and merges each batch; all of the destination's
  tables catch up together, so the warehouse wakes once for them. Nothing is
  kept between catch-ups. For each batch:
  1. The sink folds each key's changes into one row: the latest change, with
     any unchanged large value taken from an earlier change of the batch
     (see below). The batch goes into a temporary table: with bound arrays
     while it has fewer values (rows times columns) than gosnowflake's
     threshold for binding through a stage of its own, which the server
     sets (`CLIENT_STAGE_ARRAY_BINDING_THRESHOLD`), and beyond it as a file
     the sink writes, PUT on a temporary stage and COPYed, which the spike
     found faster than the driver's own stage at 100,000 rows.
  2. Any change to the warehouse table's columns comes first, on its own,
     as Snowflake commits DDL at once (see [Changing tables](#changing-tables)).
  3. One request, one transaction, MERGEs the batch into the warehouse table
     and records the next offset in the offsets table, the offset after the
     batch's last change (not its count of changes, as the markers of the
     source's transactions take offsets too):
     `BEGIN; MERGE ...; MERGE INTO _CHANGEPORT_OFFSETS ...; COMMIT;`
     The offsets table has a row per destination, table and partition, with
     the topic's ID: a topic deleted and made again under the same name, as
     when a table is removed from the source and added back, starts again at
     offset 0, so a new ID resyncs the table rather than comparing its
     offsets with the old topic's. The next offset only ever grows, so that a
     sink thought dead but still running cannot move it back.
  4. Once it commits, the sink commits the same offsets to its consumer
     group, as a copy, so that the agent's lag and the alerts see how far
     behind the destination is.
- **Exactly once:**
  - The MERGE keeps each key's latest change in the batch, by its offset,
    and applies it only if the row holds an older one. A batch applied twice
    changes nothing, and writes nothing.
  - The offsets the sink resumes from are those in Snowflake, recorded in
    the transaction that merged the batch, never Kafka's: a batch is
    committed with its offsets or not at all.
  - The whole transaction is one request, which Snowflake finishes even if
    the sink dies while it runs: the spike killed a sink midway, and found
    the batch and its offsets committed together seconds later.
  - A statement that fails leaves its transaction open in the session,
    holding its locks, so the sink rolls back after any failure.
  - On start, the sink aborts the open transactions of each destination
    user's other sessions (`SHOW TRANSACTIONS`, `SYSTEM$ABORT_TRANSACTION`),
    which a killed sink can leave behind: their locks would make every MERGE
    of the table wait, for 12 hours by default. The checks, the only other
    sessions of the user, open no transactions. A statement waits at most
    five minutes for a lock (`LOCK_TIMEOUT`), so that a lock left behind
    fails a batch, which is tried again, rather than stalling the table.
  - **Behind retention:** were a table's next offset older than the topic's
    first, as after a destination is paused for longer than the source's
    retention, the changes between are gone: the table stops with the
    reason, and the console offers to resync it. So does the source's
    recovery from a lost replication slot, which copies every table again
    (cdc.md).
- **Deletes:** a deleted row stays in the warehouse table, marked with
  `_CHANGEPORT_DELETED = TRUE`, with its last values: a delete carries only
  the row's key, as Postgres logs only that of a deleted row unless the
  table's replica identity is `FULL`. A change
  older than the delete, such as a snapshot row read again, then cannot bring
  the row back. The docs show the view that hides deleted rows. The
  tombstone after each delete, for Kafka's compaction, is skipped. The
  warehouse's columns are nullable but for the key, whatever Postgres's
  constraints.
- **Truncates:** Debezium skips a `TRUNCATE` by default, so it does not reach
  the warehouse. #223 decides between having the source write them
  (`skipped.operations: none`), so that the sink marks every row of the
  table deleted, and saying so in the docs, with a resync to follow one.
- **Unchanged large values:** Postgres keeps a large value (above about
  2 kB, such as a long text, JSON document or array) out of line, and an
  update that leaves it alone does not log it. Debezium writes
  `__debezium_unavailable_value` in its place. Folding a key's changes, the
  sink takes such a column's value from the batch's latest change that has
  it, such as the insert before the update, and records the columns no
  change of the batch has in a column of the batch table; the MERGE keeps
  those columns' warehouse values:
  `NOTES = IFF(ARRAY_CONTAINS('NOTES'::VARIANT, S._UNCHANGED), T.NOTES, S.NOTES)`.
  Were the row in neither, which a snapshot's ordering rules out, the column
  would be null until the row's next change of it. Tables need no
  `REPLICA IDENTITY FULL`.
- **Columns Changeport adds** to every table:
  - `_CHANGEPORT_DELETED`: whether the source deleted the row;
  - `_CHANGEPORT_OFFSET`: the position of the row's latest change in its
    topic, which orders changes;
  - `_CHANGEPORT_CHANGED_AT`: when the change committed in Postgres;
  - `_CHANGEPORT_SYNCED_AT`: when it landed in Snowflake.
- **Starting a table:** a source's topics keep its retention, 7 days by
  default, so a destination added later may find its tables' first rows
  gone. A new table starts from the topic's first offset when the topic
  still holds everything the source wrote, and otherwise asks the source
  for a new snapshot of it and starts there. Snapshot rows arrive at least
  once; merging by key, the warehouse sees each once.

### Destinations

A namespaced `SnowflakeDestination` resource in `customer-<id>` describes
each destination (#222):

- **Spec:** the source it reads; the account identifier, user, role,
  warehouse, database and schema; the tables, each a table of the source;
  the pace; and `state`, `Running` or `Paused`.
- **Status:** per table, whether it is starting, catching up or current,
  the rows merged, the latest change merged and how far behind the source
  it is; and conditions.
- **The key:** generated in the cluster when the destination is created,
  into a Secret only the sink and the provisioner's controller read. It
  never leaves the cluster; the console shows only its public half, in the
  setup SQL. The controller runs the destination's checks with it and
  reports them in the status, which the console shows. A new key goes in
  the user's second key slot: the console shows
  `ALTER USER ... SET RSA_PUBLIC_KEY_2 = '...'`, the controller checks the
  new key, the sink moves to it, and the console shows the statement that
  unsets the old one, so that the sink is never without a working key.

### Changing tables

- A column added to a source table is added to its warehouse table before the
  next MERGE that carries it, in a statement of its own, which is a no-op
  when it is there already. Postgres fills a column added with a default
  without logging the rows, and the spike found no defaults in the
  registry's schemas, so the warehouse's existing rows have it null; #224
  finds whether Debezium can say so, and otherwise the console offers a
  resync.
- A type widened in Postgres is widened in Snowflake where Snowflake allows
  it.
- A column dropped stays in the warehouse, its new rows null.
- What cannot change in place, such as a changed primary key or a narrowed
  type, stops that table with a reason, and the console offers to resync it
  (#229).

## Types

How each Postgres type a source allows arrives, as Debezium writes it with
the source's settings, and the Snowflake type it lands as, from the spike's
decoded messages:

| Postgres | Arrives as | Snowflake |
|---|---|---|
| `smallint`, `integer`, `bigint` | int or long | `NUMBER(38,0)` |
| `numeric(p,s)` | decimal at scale s | `NUMBER(p,s)`; above 38 digits, `TEXT` |
| `numeric` without a precision | a value and its own scale per row (`VariableScaleDecimal`), 35 digits in the spike | `TEXT`, exact (see below) |
| `money` | decimal at scale 2 | `NUMBER(38,2)` |
| `real`, `double precision` | float or double | `FLOAT` |
| `boolean` | boolean | `BOOLEAN` |
| `text`, `varchar`, `char(n)` | string; `char` keeps its padding | `TEXT` |
| `bytea` | bytes | `BINARY` |
| `date` | days since 1970 (`Date`) | `DATE` |
| `time` | microseconds since midnight (`MicroTime`) | `TIME(6)` |
| `timetz` | string, in UTC (`ZonedTime`) | `TIME(6)`, in UTC |
| `timestamp`, `timestamp(4)` to `(6)` | microseconds since 1970 (`MicroTimestamp`) | `TIMESTAMP_NTZ(6)` |
| `timestamp(0)` to `(3)` | milliseconds since 1970 (`Timestamp`) | `TIMESTAMP_NTZ(3)` |
| `timestamptz` | ISO 8601 string in UTC (`ZonedTimestamp`) | `TIMESTAMP_TZ(6)` |
| `interval` | microseconds (`MicroDuration`), a month taken as 365.25 / 12 days | `NUMBER(38,0)` of microseconds |
| `uuid` | string (`Uuid`) | `TEXT` |
| `json`, `jsonb` | string (`Json`) | `VARIANT`, parsed |
| arrays, such as `int[]`, `text[]` | array | `ARRAY` |
| enums | string (`Enum`) | `TEXT` |
| `inet`, `cidr`, `macaddr`, `xml`, `tsvector`, ranges | string | `TEXT` |
| `bit(n)`, `varbit` | bytes (`Bits`) | `BINARY` |
| `point` | x, y and its WKB (`geometry.Point`) | `GEOMETRY`, from the WKB |

The names in parentheses are Debezium's semantic types, which the registry's
schemas carry as `connect.name` and the sink reads to tell, for one, a
`timestamp`'s microseconds from a `timestamp(3)`'s milliseconds: the Avro
types alone are the same. Three choices to settle in #221:

- **`numeric` without a precision** can hold more digits than any Snowflake
  `NUMBER`, and a different scale in every row, so it lands as exact text
  rather than rounded. The console's checks say so at setup, and that a
  precision on the column makes it a number.
- **`interval`** arrives as microseconds with months approximated; Debezium
  can write ISO 8601 durations instead (`interval.handling.mode: string`),
  exact, but that changes the source's topics for customers who read them.
- **Size limits:** a value larger than Snowflake allows in a column stops
  the table with the column and the row's key.

## Cost

Snowflake bills a warehouse by the second while it runs, at least a minute
each time it resumes, at 1 credit an hour for an extra-small one. The spike
found a batch's work small: loading took 1 to 7 seconds and the MERGE 1 to
3.5 seconds for 1,000 to 100,000 changes, into tables of up to ten million
rows. A warehouse that suspends a minute after its last query therefore
costs about a minute a catch-up for a destination of a few tables, and the
pace decides the bill:

| Pace | Warehouse awake | Credits a month, extra small |
|---|---|---|
| About a minute | Always | about 730 |
| Every 5 minutes | about 13 minutes an hour | about 150 |
| Every 15 minutes | about 4 minutes an hour | about 50 |
| Every hour | about a minute an hour | about 13 |
| Every 6 hours | about 4 minutes a day | about 2 |

A destination of many or large tables keeps the warehouse awake longer
each catch-up: about 3 seconds for each table that changed. The console
estimates from the tables' count before the destination starts, and from
the warehouse's seconds each catch-up took afterwards, which the sink
reports. The first copy of each table, and a resync, cost the time they
take once. Tables much larger than ten million rows were not measured; the
MERGE's time grows with the micro-partitions it reads, and #223 measures
them. The console shows this when the pace is chosen, and the default is
every 15 minutes. A warehouse the customer runs for other work anyway costs only the
seconds of each batch. Snowflake's serverless loading (Snowpipe Streaming)
would cost less at a minute's pace, but lands rows rather than merging them,
so it would need a merge of its own after it, with its own way of keeping
results exactly once; it is a later option for customers who want minutes
cheaply.

## Keeping the customer's account safe

- **Least privilege:** the role may use the one warehouse and own the one
  schema, which lets it create and change tables, temporary tables and
  temporary stages there. It has nothing else.
- **No password:** the user is a service user with a key pair, whose private
  half never leaves the cluster.
- **Credits:** the sink resumes the warehouse only to load and merge a batch,
  and the setup SQL suggests a resource monitor with a monthly cap for
  Changeport's warehouse.
- **Their data:** the sink holds a batch only until it commits; nothing is
  kept outside the customer's Kafka and the customer's Snowflake.

## The console's checks

Each with a plain-language reason when it fails (#221):

- The account identifier names an account that answers.
- The key signs in as the user, from Changeport's outbound addresses, so a
  network policy that leaves them out fails here.
- The user is no other destination's.
- The role is the user's, may use the warehouse, and owns the schema.
- The warehouse resumes.
- The source's tables have no warehouse tables of another destination in the
  schema.

## Guardrails

- One destination per source per schema, and a Snowflake user per
  destination; two destinations may share an account.
- A table's warehouse name is its Postgres name, upper case as Snowflake
  keeps unquoted names, and two tables that would collide are refused.
- A pace from about a minute to once a day.

## What the spike proved

Against the Snowflake test account (#220), with the tests in
`internal/snowflake`:

- **Key-pair sign-in works** for a service user with no password, with
  `gosnowflake` and nothing else.
- **A batch applied twice changes nothing,** writing no row again; an
  earlier batch does not move the next offset back; and a delete, kept as a
  mark with the row's last values, cannot be undone by an older change
  (`TestMergeKeepsEachKeysLatestChangeOnce`).
- **A transaction sent as one request survives the sink's death:** killed
  two seconds into a fifteen-second request, the sink's batch and offsets
  committed together after it was gone
  (`TestASinkKilledDuringItsOneRequestTransactionLeavesItWhole`).
- **A transaction of separate requests does not:** killed with its MERGE
  done, the sink left the transaction open, and the restarted sink's MERGE
  waited for its lock until the session's lock timeout. Aborting the dead
  transaction, which the same user may, freed the table, and the dead
  sink's MERGE had never happened
  (`TestASinkKilledInAnOpenTransactionLeavesItHoldingLocks`).
- **Loading and merging take seconds,** on an extra-small warehouse, over
  two runs (`TestMeasureLoadingAndMerging`, with `SNOWFLAKE_SPIKE=1`), into
  a temporary table of seven columns. At 10,000 rows and more, the bound
  arrays went through gosnowflake's own stage:

  | Rows | Bound arrays | PUT and COPY | MERGE, inserts | MERGE, updates |
  |---|---|---|---|---|
  | 1,000 | 1.2 to 1.3 s | 3.1 to 3.8 s | 1.0 to 1.2 s | 1.1 s |
  | 10,000 | 2.0 to 2.6 s | 2.6 to 3.0 s | 1.1 to 1.4 s | 1.0 to 1.2 s |
  | 100,000 | 9.4 to 9.5 s | 3.5 to 6.6 s | 1.2 to 1.7 s | 1.9 to 2.2 s |

- **A MERGE into ten million rows takes seconds too**
  (`TestMeasureMergingIntoALargeTable`): 3.0 to 3.5 seconds for 1,000 to
  100,000 changes of keys spread over the whole table, 1.0 to 2.1 seconds
  for the newest 1%'s keys, as an application's updates mostly are, and 0.8
  to 1.7 seconds for new rows.

- **Every column type decodes,** in a spike on kind (to become #221's
  type mapping test): a table with a column of each type, read
  `read_committed` from its topic and decoded with `internal/tail`'s registry
  client, through a snapshot, an update, a delete and an insert. It found
  the unchanged large value's placeholder, the key-only delete and the
  variable-scale `numeric` above, and that the registry's schemas carry
  Debezium's semantic types.

## Testing

- **`internal/snowflake`'s tests** run against the Snowflake test account
  (README.md, Snowflake), each in a schema of its own, `RUN_<time>_<random>`,
  which the test drops; a test drops the schemas of runs that crashed, and
  aborts their transactions, after an hour. `just snowflake-test` runs them
  with the account in `.env`; `just test` skips them unless the environment
  has the account, as CI's Go job does for the changes that need them.
- **On kind:** the sink on kind reaches Snowflake over the internet, as on
  EKS, and a destination of the demo customer's source delivers the stand-in
  database's tables into the test account (#225, #230).
- **The destinations suite** (#230), on a kind cluster of its own in CI,
  compares each warehouse table with its source table row by row, while the
  sink, the Connect worker and a broker are killed.

## Steps

Each step is an issue in the milestone and a pull request with its tests:

1. [#219](https://github.com/ReneP92/changeport/issues/219): this design, and a spike of the parts no one has run yet
2. [#220](https://github.com/ReneP92/changeport/issues/220): the Snowflake test account
3. [#221](https://github.com/ReneP92/changeport/issues/221): naming, setup SQL, the type mapping and checks (`internal/snowflake`)
4. [#222](https://github.com/ReneP92/changeport/issues/222): the `SnowflakeDestination` resource
5. [#223](https://github.com/ReneP92/changeport/issues/223): the sink engine, merging each table exactly once
6. [#224](https://github.com/ReneP92/changeport/issues/224): creating and changing warehouse tables as sources change
7. [#225](https://github.com/ReneP92/changeport/issues/225): the sink's runtime from the template
8. [#226](https://github.com/ReneP92/changeport/issues/226): the console's API
9. [#227](https://github.com/ReneP92/changeport/issues/227): the console's pages and the customer docs
10. [#228](https://github.com/ReneP92/changeport/issues/228): monitoring, alerts and the runbook
11. [#229](https://github.com/ReneP92/changeport/issues/229): resyncing a table, which starting a table uses too
12. [#230](https://github.com/ReneP92/changeport/issues/230): exactly-once results proven end to end, through failures
13. [#231](https://github.com/ReneP92/changeport/issues/231): the pipeline from source table to warehouse table on the topology map

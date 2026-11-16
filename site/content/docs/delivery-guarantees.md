# Delivery guarantees

Networks fail, and services restart halfway through their work. Whether a
message then arrives once, twice or not at all depends on your producers
and consumers as much as on Kafka. This page says what each guarantee
takes, and where exactly once honestly ends.

## The three guarantees

- **At most once:** a message arrives once or never. Nothing is repeated,
  but a failure at the wrong moment loses it.
- **At least once:** a message arrives once, or more than once after a
  failure. Nothing is lost.
- **Exactly once:** a message's effect happens once, even across
  failures.

At least once is the right default for most applications, with processing
that is safe to repeat. Exactly once is possible from Kafka topic to Kafka
topic, with transactions, and takes more care.

## Writing: nothing lost, nothing twice

Two producer settings make a write safe:

- **Wait for every copy** (`acks=all`): the producer counts a message as
  sent once every in-sync copy has it. On the production profile that is
  at least two of three zones, so a broker or a zone that fails right after
  doesn't lose it.
- **Idempotence** (`enable.idempotence=true`): when the answer to a send
  is lost and the producer retries, the brokers recognize the retry and
  keep one copy, in order.

Current clients default to both, but not all of them, and some drop
idempotence silently when another setting conflicts with it. Set both in
your code, as every example in the
[connection guide](connecting.md#delivery-guarantees) does, and
[keep the default retries](connecting.md#keep-the-default-retries).

With both, every message a producer sends is written once. The console
checks: when a busy topic has no idempotent producer writing to it, it
says so, as
[producers write without idempotence](troubleshooting/producers-not-idempotent.md).

## Reading: at least once

A consumer that commits its position after processing a message never
skips one: if it crashes before committing, the next consumer reads the
message again. That is at least once, and it means your processing may
see a message twice.

Make processing safe to repeat:

- **Upsert rather than insert**, keyed by something in the message, such
  as an order's ID.
- **Remember what you processed**, for example by each message's topic,
  partition and offset, which together identify it.

Committing before processing turns this into at most once: a crash after
the commit skips the message. See
[Commit offsets after processing](connecting.md#commit-offsets-after-processing).

## Exactly once, from topic to topic

An application that reads a topic, processes each message and writes the
result to another topic can do so exactly once, with a transaction: its
output and its input's committed position are written together, or not at
all. A crash halfway leaves neither, and the work is done again from the
same position, without a trace of the first try.

The connection guide has a tested Java example:
[Exactly once: read, process, write](connecting.md#exactly-once-read-process-write).

It holds end to end only with the consumers downstream:

- **They read committed messages only** (`isolation.level=read_committed`),
  so that they never see what an aborted transaction wrote. The Java
  client reads uncommitted messages unless told otherwise.
- **Effects outside Kafka are not in the transaction.** Writing to a
  database, calling an API or sending an email from the processing step
  happens again when a transaction is aborted and tried again. Make those
  effects safe to repeat, as for at least once.

So exactly once covers what stays in Kafka. Anything your code does
outside Kafka is at least once, and needs processing that is safe to
repeat.

## Transactions that stay open

A transaction holds back `read_committed` consumers on its partitions
until it commits or aborts: they read up to its first message and wait.
Healthy producers keep their transactions short, a moment each. A producer
that hangs, or stops without aborting, leaves its transaction open until
Kafka aborts it, after the producer's `transaction.timeout.ms`.

The console watches for this:

- **The overview's health** says how many transactions are open, and how
  long the oldest has been.
- **A topic's Producers card** shows the producers that write to it
  idempotently or in transactions, and warns when a transaction has held
  messages back for a minute.
- **The diagnoses** find
  [a transaction that holds consumers back](troubleshooting/open-transaction.md),
  [one that hangs](troubleshooting/hanging-transaction.md), and
  [producers that abort transactions](troubleshooting/transactions-aborted.md).

## Postgres sources

A Postgres source delivers each change at least once. While a table is
copied, a row that changes at the same time may arrive twice. Consumers
that upsert by the message's key, the row's primary key, see each row
once. See [Postgres sources](postgres.md).

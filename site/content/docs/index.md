# Changeport docs

Changeport runs a Kafka cluster for you, and a console that explains what
happens in it. These docs cover what you need to build on it, in plain
language first and Kafka's own terms second.

## Start here

- **[What Changeport runs for you](your-cluster.md):** your cluster, how it
  keeps your data safe, and what the platform decides so that you don't
  have to.
- **[Connecting to your Kafka cluster](connecting.md):** your connection
  details, the settings that matter, and examples in Java, Python, Go and
  Node.js.

## Using Kafka

- **[Topics, partitions and keys](topics.md):** where your messages live,
  how long they stay, and how keys keep them in order.
- **[Consumer groups and lag](consumer-groups.md):** how your services read
  a topic together, how far behind they are, and how to read messages
  again.
- **[Delivery guarantees](delivery-guarantees.md):** at least once, exactly
  once, and what each takes from your code.
- **[Postgres sources](postgres.md):** stream your database's tables into
  topics as they change.

## When something is wrong

- **[Alerts](alerts.md):** what each alert means and what to do about it.
- **[Troubleshooting](troubleshooting/index.md):** a page for each problem
  the console finds in your cluster, with what to do.

Something missing or unclear? Every question we answer twice becomes a page
here or a check in the console, so tell us, from **Help** in the console.

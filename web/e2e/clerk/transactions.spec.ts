import { expect, test } from "@playwright/test";
import { readFixtures } from "./fixtures";
import {
  card,
  connect,
  downloadClientProperties,
  expectNoSidewaysScroll,
  kafka,
  kafkaInBackground,
  openOverview,
  stopKafkaTools,
} from "./helpers";

// Delivery guarantees on customer A's cluster (docs/console.md): a topic's
// page names its transactional producers and counts the idempotent ones,
// the overview counts the open transactions, and one left open holds back
// read_committed consumers, which the diagnoses say.

let fixtures: ReturnType<typeof readFixtures>;
test.beforeAll(() => {
  fixtures = readFixtures();
});

test("a transaction left open shows, and is diagnosed as holding consumers back", async ({
  page,
}, testInfo) => {
  test.setTimeout(8 * 60_000);
  const a = fixtures.customers.a;
  const topic = `txn-${fixtures.run}`;
  const transactional = `txn-producer-${fixtures.run}`;
  await openOverview(page, fixtures.users.adminA);
  const properties = testInfo.outputPath("client.properties");
  await downloadClientProperties(page, properties);
  kafka(a.id, properties, "kafka-topics.sh", [...connect(a.id), "--create", "--topic", topic]);
  const running: Promise<unknown>[] = [];
  try {
    // An idempotent producer writes a message, as the console producer is.
    kafka(
      a.id,
      properties,
      "kafka-console-producer.sh",
      [...connect(a.id), "--topic", topic],
      "plain\n",
    );
    // A transactional one writes a message a second, in a transaction it
    // commits only after 10 minutes: one left open.
    running.push(
      kafkaInBackground(a.id, properties, "kafka-producer-perf-test.sh", [
        "--topic",
        topic,
        "--num-records",
        "600",
        "--throughput",
        "1",
        "--record-size",
        "100",
        "--transactional-id",
        transactional,
        "--transaction-duration-ms",
        "600000",
        "--producer.config",
        "/config/client.properties",
        "--producer-props",
        `bootstrap.servers=bootstrap.${a.id}.kafka.changeport.test:9094`,
        "transaction.timeout.ms=600000",
      ]).catch(() => ""),
    );

    // The topic's page names it, in its transaction, beside the idempotent one.
    await page.goto(`/topics/${topic}`);
    const producers = card(page, "Producers");
    const rows = producers.getByRole("list", { name: "Producers" }).getByRole("listitem");
    await expect(async () => {
      await page.reload();
      await expect(rows.filter({ hasText: transactional })).toContainText(
        "TransactionalIn a transaction",
        { timeout: 5_000 },
      );
    }).toPass({ timeout: 2 * 60_000, intervals: [10_000] });
    await expect(rows.filter({ hasText: "Idempotent" })).toHaveCount(1);
    await expect(producers).toContainText(
      /Open transactions hold back [\d,]+ messages? from read_committed consumers/,
    );
    await page.setViewportSize({ width: 390, height: 844 });
    await expectNoSidewaysScroll(page);
    await page.setViewportSize({ width: 1280, height: 900 });

    // The overview counts it, and once it has been open a minute, says it
    // holds consumers back.
    await page.goto("/");
    await expect(card(page, "Health")).toContainText(/Transactions: \d+ open/);
    const finding = card(page, "What needs attention").getByRole("listitem", {
      name: `A transaction holds back the consumers of ${topic}`,
    });
    await expect(async () => {
      await page.reload();
      await expect(finding).toBeVisible({ timeout: 5_000 });
    }).toPass({ timeout: 4 * 60_000, intervals: [15_000] });
    await expect(finding).toContainText(
      new RegExp(
        `Transaction ${transactional} has been open for \\d+ minutes?\\. Until it commits or aborts, read_committed consumers cannot read the [\\d,]+ messages after its start on ${topic}\\. Kafka aborts it once it has been open for 10 minutes\\.`,
      ),
    );
    await expect(finding).toContainText("transaction.timeout.ms");
    await expectNoSidewaysScroll(page);
  } finally {
    stopKafkaTools(topic);
    kafka(a.id, properties, "kafka-topics.sh", [...connect(a.id), "--delete", "--topic", topic]);
    await Promise.all(running);
  }
});

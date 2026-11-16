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

// The diagnoses (docs/diagnosis.md) on customer A's cluster: a consumer
// group that reads slower than its topic receives is told it can't keep up,
// with the numbers, and the diagnosis clears once it catches up. On kind the
// diagnoses look back 5 minutes (deploy/overlays/kind), so the group must
// fall behind for longer than that first.

let fixtures: ReturnType<typeof readFixtures>;
test.beforeAll(() => {
  fixtures = readFixtures();
});

test("a consumer group that can't keep up is told so, until it catches up", async ({
  page,
}, testInfo) => {
  test.setTimeout(14 * 60_000);
  const a = fixtures.customers.a;
  const topic = `diagnosed-${fixtures.run}`;
  const group = `slow-${fixtures.run}`;
  await openOverview(page, fixtures.users.adminA);
  const properties = testInfo.outputPath("client.properties");
  await downloadClientProperties(page, properties);
  kafka(a.id, properties, "kafka-topics.sh", [
    ...connect(a.id),
    "--create",
    "--topic",
    topic,
    "--partitions",
    "3",
  ]);
  const bootstrap = `bootstrap.${a.id}.kafka.changeport.test:9094`;
  const running: Promise<unknown>[] = [];
  try {
    // 20 messages a second for 8 minutes.
    running.push(
      kafkaInBackground(a.id, properties, "kafka-producer-perf-test.sh", [
        "--topic",
        topic,
        "--num-records",
        String(20 * 8 * 60),
        "--throughput",
        "20",
        "--record-size",
        "100",
        "--producer.config",
        "/config/client.properties",
        "--producer-props",
        `bootstrap.servers=${bootstrap}`,
      ]).catch(() => ""),
    );
    // A consumer of the group that gives up after idle seconds without a
    // message.
    const consume = (idle: number, ...throttle: string[]) =>
      kafkaInBackground(a.id, properties, "kafka-console-consumer.sh", [
        ...connect(a.id),
        "--topic",
        topic,
        "--group",
        group,
        "--timeout-ms",
        String(idle * 1000),
        ...throttle,
      ]).catch(() => "");
    // One that takes a single small batch of each partition every half
    // second: about 2 messages a second.
    running.push(
      consume(
        30,
        "--consumer-property",
        "max.partition.fetch.bytes=1",
        "--consumer-property",
        "fetch.min.bytes=1000000",
        "--consumer-property",
        "fetch.max.wait.ms=500",
      ),
    );

    // Once the group has been behind for longer than the window, and
    // further behind than a window ago.
    const diagnoses = card(page, "What needs attention");
    const finding = diagnoses.getByRole("listitem", {
      name: `${group} can't keep up with ${topic}`,
    });
    await expect(async () => {
      await page.reload();
      await expect(finding).toBeVisible({ timeout: 5_000 });
    }).toPass({ timeout: 9 * 60_000, intervals: [20_000] });
    await expect(finding).toContainText(
      new RegExp(
        `${group} consumes [\\d.]+ messages a second of ${topic}, which receives (19|20|21), so it falls further behind`,
      ),
    );
    await expect(finding).toContainText(
      `Run more consumers in ${group}. It has 1 consumer for the 3 partitions of ${topic}, so up to 2 more can share the work.`,
    );
    // Its page in the customer docs, where kind's console says they are
    // (deploy/overlays/kind), in a new tab.
    const docs = finding.getByRole("link", { name: /^Learn more/ });
    await expect(docs).toHaveAttribute(
      "href",
      "http://localhost:5174/docs/troubleshooting/consumer-too-slow.html",
    );
    await expect(docs).toHaveAttribute("target", "_blank");
    await expectNoSidewaysScroll(page);

    // The group needs attention: in the consumer groups' table, which sorts
    // it first by health.
    const groups = card(page, "Consumer groups");
    const row = groups.getByRole("row").filter({ hasText: group });
    await expect(row).toContainText("Needs attention");
    await groups.getByRole("button", { name: /^Health/ }).click();
    await expect(groups.locator("th[aria-sort=descending]")).toHaveText(/Health/);
    await expect(groups.getByRole("row").nth(1)).toContainText(/Needs attention|At risk/);

    // The group's page says it too, and the data flow map marks the group.
    await expect(
      card(page, "Data flow")
        .getByRole("link", { name: new RegExp(`^${group}`) })
        .getByRole("img", { name: "Needs attention" }),
    ).toBeVisible();
    await finding.getByRole("link", { name: `Consumer group ${group}` }).click();
    await expect(page.getByRole("heading", { level: 1, name: group })).toBeVisible();
    const health = card(page, "Health");
    await expect(health).toContainText(`${group} can't keep up with ${topic}`);
    await expect(health).toContainText("Needs attention");
    // The last 24 hours, from the console's health monitor, which looks
    // every minute.
    await expect(async () => {
      await page.reload();
      await expect(health.getByRole("img", { name: /needed attention for/ })).toBeVisible({
        timeout: 5_000,
      });
    }).toPass({ timeout: 3 * 60_000, intervals: [15_000] });

    // A consumer that reads at full speed takes over, catches up, and the
    // diagnosis clears. It gets nothing until the group has dropped the one
    // stopped, once its session times out (45 seconds), so it waits longer
    // than that before it gives up.
    stopKafkaTools(group);
    running.push(consume(120));
    await page.goto("/");
    await expect(async () => {
      await page.reload();
      // Once the diagnoses are in, not while the card waits for them.
      await expect(card(page, "What needs attention")).toHaveAttribute("aria-busy", "false", {
        timeout: 10_000,
      });
      // Nothing about the group at all: not "can't keep up", and not
      // "nothing is consuming" either.
      await expect(
        card(page, "What needs attention").getByRole("listitem").filter({ hasText: group }),
      ).toHaveCount(0);
      // And the group is healthy again.
      await expect(
        card(page, "Consumer groups").getByRole("row").filter({ hasText: group }),
      ).toContainText("Healthy", { timeout: 5_000 });
    }).toPass({ timeout: 4 * 60_000, intervals: [20_000] });
  } finally {
    // A tool may still be starting, its container not there yet.
    for (let attempt = 0; attempt < 3; attempt++) {
      stopKafkaTools(topic);
      await page.waitForTimeout(5_000);
    }
    kafka(a.id, properties, "kafka-topics.sh", [...connect(a.id), "--delete", "--topic", topic]);
    await Promise.all(running);
  }
});

test("a member sees what needs attention too", async ({ page }) => {
  await openOverview(page, fixtures.users.memberA);
  await expect(card(page, "What needs attention")).toBeVisible();
});

import { expect, test } from "@playwright/test";
import { readFixtures } from "./fixtures";
import {
  card,
  connect,
  downloadClientProperties,
  expectNoSidewaysScroll,
  kafka,
  openOverview,
} from "./helpers";

// Usage on customer A's cluster (docs/console.md): the data a topic
// processed shows on the environment's Usage page, each byte once, and the
// organization's Environments page counts it too.

let fixtures: ReturnType<typeof readFixtures>;
test.beforeAll(() => {
  fixtures = readFixtures();
});

test("usage shows the data a topic processed, once", async ({ page }, testInfo) => {
  test.setTimeout(6 * 60_000);
  const a = fixtures.customers.a;
  const topic = `usage-${fixtures.run}`;
  await openOverview(page, fixtures.users.adminA);
  const properties = testInfo.outputPath("client.properties");
  await downloadClientProperties(page, properties);

  // 500 messages of 1,000 bytes, read back once.
  kafka(a.id, properties, "kafka-topics.sh", [
    ...connect(a.id),
    "--create",
    "--topic",
    topic,
    "--partitions",
    "1",
  ]);
  kafka(
    a.id,
    properties,
    "kafka-console-producer.sh",
    [...connect(a.id), "--topic", topic],
    Array.from({ length: 500 }, (_, i) => String(i).padStart(1000, "0")).join("\n") + "\n",
  );
  kafka(a.id, properties, "kafka-console-consumer.sh", [
    ...connect(a.id),
    "--topic",
    topic,
    "--group",
    topic,
    "--from-beginning",
    "--max-messages",
    "500",
    "--timeout-ms",
    "60000",
  ]);

  await page
    .getByRole("navigation", { name: "Console" })
    .getByRole("link", { name: "Usage" })
    .first()
    .click();
  await expect(page.getByRole("heading", { level: 1, name: "Usage" })).toBeVisible();
  const row = card(page, "Topics").getByRole("row").filter({ hasText: topic });
  // The brokers' counters are scraped every 30 seconds; the page asks every
  // minute, so reload until they are in.
  await expect(async () => {
    await page.reload();
    // 500 KB of messages and a few bytes per message of Kafka's batches.
    await expect(row.getByRole("cell").nth(1)).toHaveText(/^50\d KB$/, { timeout: 5_000 });
    await expect(row.getByRole("cell").nth(2)).toHaveText(/^50\d KB$/, { timeout: 1_000 });
  }).toPass({ timeout: 150_000 });
  // Today's bars and the totals count it too.
  await expect(card(page, "Per day").locator(".recharts-bar-rectangle").first()).toBeVisible();
  await expect(page.getByRole("main").getByText("Data in")).toBeVisible();
  await expectNoSidewaysScroll(page);

  // The organization's environments show what each processed.
  await page.goto("/environments");
  await expect(
    page.getByRole("listitem", { name: "production" }).getByRole("link", {
      name: /processed in the last 30 days$/,
    }),
  ).toBeVisible();
});

test("a member sees the environment's usage too", async ({ page }) => {
  await openOverview(page, fixtures.users.memberA);
  await page
    .getByRole("navigation", { name: "Console" })
    .getByRole("link", { name: "Usage" })
    .first()
    .click();
  await expect(card(page, "Per day")).toBeVisible();
});

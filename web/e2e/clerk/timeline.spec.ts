import { expect, test, type Locator, type Page } from "@playwright/test";
import { readFixtures } from "./fixtures";
import {
  connect,
  downloadClientProperties,
  expectNoSidewaysScroll,
  kafka,
  kafkaInBackground,
  openOverview,
} from "./helpers";

// The change timeline on customer A's cluster (docs/timeline.md): a topic
// created in the console, a change of its configuration and a consumer
// group's rebalance appear over the graphs, each at the time it happened, on
// the overview, the topic's page and the group's page.

let fixtures: ReturnType<typeof readFixtures>;
test.beforeAll(() => {
  fixtures = readFixtures();
});

/** The change that says text, in the list under a page's graph. */
function change(page: Page, text: string | RegExp): Locator {
  return page
    .getByRole("region", { name: "Changes" })
    .getByRole("listitem")
    .filter({ hasText: text });
}

/**
 * Expects a change to show when it happened: the agent finds changes within
 * its 30-second interval, so it is stamped at most a little later.
 */
async function expectAt(item: Locator, from: number, to: number): Promise<void> {
  const shown = await item.locator("time").getAttribute("dateTime");
  const at = new Date(shown ?? "").getTime();
  expect(at, `the change at ${shown ?? "no time"}`).toBeGreaterThanOrEqual(from - 1_000);
  expect(at, `the change at ${shown ?? "no time"}`).toBeLessThanOrEqual(to + 45_000);
}

test("a config change and a rebalance appear on the timeline when they happened", async ({
  page,
}, testInfo) => {
  test.setTimeout(6 * 60_000);
  const a = fixtures.customers.a;
  const topic = `timeline-${fixtures.run}`;
  const group = `timeline-group-${fixtures.run}`;
  await openOverview(page, fixtures.users.adminA);
  const main = page.getByRole("main");

  // The admin creates the topic in the console, which names them.
  await main.getByRole("button", { name: "Create topic" }).click();
  const create = page.getByRole("dialog", { name: "Create a topic" });
  await create.getByLabel("Name").fill(topic);
  await create.getByLabel("Partitions").fill("2");
  await create.getByRole("combobox", { name: "Retention" }).click();
  await page.getByRole("option", { name: "1 hour" }).click();
  await create.getByRole("button", { name: "Create topic" }).click();
  await expect(create).toBeHidden();
  const created = change(page, `created topic ${topic} with 2 partitions in the console`);
  await expect(created).toBeVisible({ timeout: 30_000 });

  // Its retention changes, as a client would change it, once the agent has
  // seen the topic: it compares the cluster every 30 seconds, so a change
  // within the same look as the topic's creation is part of how it was
  // created.
  const properties = testInfo.outputPath("client.properties");
  await downloadClientProperties(page, properties);
  await page.waitForTimeout(35_000);
  const configured = Date.now();
  kafka(a.id, properties, "kafka-configs.sh", [
    ...connect(a.id),
    "--alter",
    "--entity-type",
    "topics",
    "--entity-name",
    topic,
    "--add-config",
    "retention.ms=172800000",
  ]);
  const configChange = change(
    page,
    `Topic ${topic}: retention.ms changed from 3600000 (1 hour) to 172800000 (2 days).`,
  );
  await expect(configChange).toBeVisible({ timeout: 90_000 });
  await expectAt(configChange, configured, Date.now());
  // The agent's record of the topic it found created goes, as the console's
  // says who created it.
  await expect(change(page, `Topic ${topic} was created`)).toHaveCount(0);

  // Messages for a consumer group, which starts with one member; a second
  // joins, so it rebalances.
  kafka(
    a.id,
    properties,
    "kafka-console-producer.sh",
    [...connect(a.id), "--topic", topic],
    Array.from({ length: 10 }, (_, i) => String(i)).join("\n") + "\n",
  );
  const consume = () =>
    kafkaInBackground(a.id, properties, "kafka-console-consumer.sh", [
      ...connect(a.id),
      "--topic",
      topic,
      "--group",
      group,
      "--from-beginning",
      "--timeout-ms",
      "100000",
    ]).catch(() => "");
  const first = consume();
  await expect(change(page, `Consumer group ${group} started`)).toBeVisible({ timeout: 90_000 });
  const joined = Date.now();
  const second = consume();
  const rebalance = change(
    page,
    new RegExp(`Consumer group ${group} rebalanced: .* joined\\. It has 2 members\\.`),
  );
  await expect(rebalance).toBeVisible({ timeout: 90_000 });
  await expectAt(rebalance, joined, Date.now());
  // Each change is a numbered marker on the graph.
  await expect(page.getByLabel(/^Marker \d+$/).first()).toBeVisible();
  await expectNoSidewaysScroll(page);

  // The topic's page shows its own changes; the group's page its
  // rebalance, and the changes of the topic it reads.
  await main.getByRole("link", { name: topic, exact: true }).first().click();
  await expect(page.getByRole("heading", { level: 1, name: topic })).toBeVisible();
  await expect(change(page, `Topic ${topic}: retention.ms changed`)).toBeVisible({
    timeout: 30_000,
  });
  await expect(change(page, `Consumer group ${group}`)).toHaveCount(0);
  await expectNoSidewaysScroll(page);
  await page.goto(`/consumer-groups/${encodeURIComponent(group)}`);
  await expect(page.getByRole("heading", { level: 1, name: group })).toBeVisible({
    timeout: 30_000,
  });
  await expect(change(page, new RegExp(`Consumer group ${group} rebalanced`))).toBeVisible({
    timeout: 30_000,
  });
  // Once the agent has measured the group's lag on the topic, and the
  // metrics store has it, the page knows the group reads it.
  await expect(change(page, `Topic ${topic}: retention.ms changed`)).toBeVisible({
    timeout: 90_000,
  });
  // Its start too, before the group's lag was first measured.
  await expect(change(page, `Consumer group ${group} started`)).toBeVisible();
  await expectNoSidewaysScroll(page);
  await Promise.all([first, second]);
});

test("a customer sees only its own changes", async ({ page }) => {
  await openOverview(page, fixtures.users.memberB);
  const changes = page.getByRole("region", { name: "Changes" });
  await expect(changes).toBeVisible();
  await expect(changes).not.toContainText(`timeline-${fixtures.run}`);
  await expect(changes).not.toContainText(fixtures.customers.a.id);
});

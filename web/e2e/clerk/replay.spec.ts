import { expect, test, type Page } from "@playwright/test";
import { kubectl, readFixtures } from "./fixtures";
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

// Moving a consumer group and replaying into a new one (docs/console.md), on
// customer A's cluster: the preview says what the move does, the group then
// reads exactly that again, Kafka's refusal while consumers run is shown
// before anything is tried, and every change is audited and on the timeline.

let fixtures: ReturnType<typeof readFixtures>;
test.beforeAll(() => {
  fixtures = readFixtures();
});

/** The console's audit log lines of a change to a group. */
function audited(change: string, group: string): string[] {
  return kubectl(["-n", "changeport", "logs", "deploy/changeport-console"])
    .split("\n")
    .filter((line) => line.includes(`msg="${change}"`) && line.includes(`group=${group}`));
}

/**
 * The lines a consumer read in group from topic until it found no more: from
 * the group's position, or from the beginning for a group without one. It
 * joins as a classic consumer, or with KIP-848's protocol.
 */
function consume(
  customer: string,
  properties: string,
  topic: string,
  group: string,
  protocol: "classic" | "consumer" = "classic",
): string[] {
  return kafka(customer, properties, "kafka-console-consumer.sh", [
    ...connect(customer),
    "--topic",
    topic,
    "--group",
    group,
    "--command-property",
    `group.protocol=${protocol}`,
    "--from-beginning",
    "--timeout-ms",
    "15000",
  ])
    .split("\n")
    .filter((line) => line !== "");
}

/**
 * Opens a group's page once the agent has measured its lag on its topics,
 * and its dialog.
 */
async function openDialog(page: Page, group: string, topics = 1) {
  await page.goto(`/consumer-groups/${group}`);
  const open = page.getByRole("button", { name: "Change position" });
  // The action shows once the group's lag is measured: every 30 seconds,
  // then scraped.
  await expect(async () => {
    await page.reload();
    await expect(open).toBeVisible({ timeout: 5_000 });
    await expect(card(page, "Topics").getByRole("listitem")).toHaveCount(topics, {
      timeout: 1_000,
    });
  }).toPass({ timeout: 150_000 });
  await open.click();
  return page.getByRole("dialog");
}

test("an admin moves a group back to a time, and it reads again what the preview said", async ({
  page,
}, testInfo) => {
  test.setTimeout(6 * 60_000);
  const a = fixtures.customers.a;
  const topic = `move-${fixtures.run}`;
  // A second topic the group reads, which it stays on.
  const other = `move-other-${fixtures.run}`;
  const group = `move-${fixtures.run}`;
  await openOverview(page, fixtures.users.adminA);
  const properties = testInfo.outputPath("client.properties");
  await downloadClientProperties(page, properties);
  kafka(a.id, properties, "kafka-topics.sh", [
    ...connect(a.id),
    "--create",
    "--topic",
    topic,
    "--partitions",
    "2",
  ]);
  const produce = (prefix: string, n: number) =>
    kafka(
      a.id,
      properties,
      "kafka-console-producer.sh",
      [...connect(a.id), "--topic", topic],
      Array.from({ length: n }, (_, i) => `${prefix}-${String(i)}`).join("\n") + "\n",
    );
  // 50 messages, then, from the next minute on, 30 more: the time picker
  // chooses minutes.
  kafka(a.id, properties, "kafka-topics.sh", [
    ...connect(a.id),
    "--create",
    "--topic",
    other,
    "--partitions",
    "1",
  ]);
  kafka(
    a.id,
    properties,
    "kafka-console-producer.sh",
    [...connect(a.id), "--topic", other],
    "o-1\no-2\no-3\n",
  );
  expect(consume(a.id, properties, other, group)).toHaveLength(3);
  produce("before", 50);
  const minute = new Date(Math.ceil(Date.now() / 60_000) * 60_000);
  await page.waitForTimeout(minute.getTime() - Date.now() + 1_000);
  produce("after", 30);
  expect(consume(a.id, properties, topic, group)).toHaveLength(80);

  const dialog = await openDialog(page, group, 2);
  await expect(dialog.getByRole("heading", { name: `Change where ${group} reads` })).toBeVisible();
  const pad = (n: number) => String(n).padStart(2, "0");
  await dialog
    .getByLabel("Time", { exact: true })
    .fill(
      `${String(minute.getFullYear())}-${pad(minute.getMonth() + 1)}-${pad(minute.getDate())}T${pad(minute.getHours())}:${pad(minute.getMinutes())}`,
    );
  // Only the topic the group moves on.
  await dialog.getByRole("checkbox", { name: other }).click();
  const effects = dialog.getByRole("region", { name: "What this does" });
  await expect(effects.getByRole("listitem")).toHaveText([
    new RegExp(`^${topic}reads 30 messages again, from `),
  ]);
  await expect(effects).toContainText("Your service handles these messages again");
  await expectNoSidewaysScroll(page);

  // Nothing moves until the group's name is typed.
  const move = dialog.getByRole("button", { name: `Move ${group}` });
  await expect(move).toBeDisabled();
  await dialog.getByLabel(`Type ${group} to confirm`).fill(group);
  await move.click();
  await expect(dialog).toBeHidden();

  // The group reads exactly the 30 later messages again.
  const again = consume(a.id, properties, topic, group);
  expect(again.toSorted()).toEqual(
    Array.from({ length: 30 }, (_, i) => `after-${String(i)}`).toSorted(),
  );
  expect(consume(a.id, properties, other, group)).toHaveLength(0);
  expect(audited("consumer group moved", group)).toHaveLength(1);
  await expect(page.getByRole("main")).toContainText(`moved consumer group ${group} to `, {
    timeout: 30_000,
  });
});

test("consumers block a move, and a replay into a new group leaves the group alone", async ({
  page,
}, testInfo) => {
  test.setTimeout(6 * 60_000);
  const a = fixtures.customers.a;
  const topic = `replay-${fixtures.run}`;
  const group = `replay-${fixtures.run}`;
  const into = `${group}-again`;
  await openOverview(page, fixtures.users.adminA);
  const properties = testInfo.outputPath("client.properties");
  await downloadClientProperties(page, properties);
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
    Array.from({ length: 40 }, (_, i) => String(i)).join("\n") + "\n",
  );
  // A consumer that keeps running in the group.
  const running = kafkaInBackground(a.id, properties, "kafka-console-consumer.sh", [
    ...connect(a.id),
    "--topic",
    topic,
    "--group",
    group,
    "--from-beginning",
  ]).catch(() => "");
  try {
    const dialog = await openDialog(page, group);
    await dialog.getByRole("radio", { name: /The oldest message kept/ }).click();
    await expect(dialog.getByRole("alert")).toContainText(
      `is reading ${group}. Stop its consumers first`,
      { timeout: 30_000 },
    );
    await dialog.getByLabel(`Type ${group} to confirm`).fill(group);
    await expect(dialog.getByRole("button", { name: `Move ${group}` })).toBeDisabled();

    // A replay does not move the group, so its consumers may keep running.
    await dialog.getByRole("radio", { name: /A new group, to replay/ }).click();
    await dialog.getByLabel("New group", { exact: true }).fill(into);
    const effects = dialog.getByRole("region", { name: "What this does" });
    await expect(effects.getByRole("listitem")).toHaveText([
      new RegExp(`^${topic}reads 40 messages, from `),
    ]);
    await expect(dialog.getByRole("alert")).toBeHidden();
    await dialog.getByRole("button", { name: `Create ${into}` }).click();
    await expect(dialog.getByRole("heading", { name: `${into} is ready` })).toBeVisible();
    await expect(dialog.getByRole("figure")).toContainText(`group.id=${into}`);
  } finally {
    stopKafkaTools(group);
    await running;
  }

  // The new group serves consumers of either protocol: here KIP-848's.
  expect(consume(a.id, properties, topic, into, "consumer")).toHaveLength(40);
  expect(audited("consumer group replayed", into)).toHaveLength(1);
});

test("members do not see where to change a group's position", async ({ page }) => {
  await openOverview(page, fixtures.users.memberA);
  await page.goto(`/consumer-groups/move-${fixtures.run}`);
  await expect(page.getByRole("main").getByRole("heading", { level: 1 })).toBeVisible();
  await expect(page.getByRole("button", { name: "Change position" })).toHaveCount(0);
});

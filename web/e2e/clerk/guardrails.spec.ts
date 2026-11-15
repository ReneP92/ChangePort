import { expect, test, type Page } from "@playwright/test";
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

// The guardrails of #177 on customer A's cluster: changing a topic says what
// it does to its consumers first (docs/console.md), and the diagnoses find
// partitions added and messages too large for their topic
// (docs/diagnosis.md).

let fixtures: ReturnType<typeof readFixtures>;
test.beforeAll(() => {
  fixtures = readFixtures();
});

/** Waits for a diagnosis on the overview, reloading as the diagnoses refresh. */
async function expectDiagnosis(page: Page, title: string, timeout: number) {
  const finding = card(page, "What needs attention").getByRole("listitem", { name: title });
  await expect(async () => {
    await page.goto("/");
    await expect(finding).toBeVisible({ timeout: 5_000 });
  }).toPass({ timeout, intervals: [15_000] });
  return finding;
}

test("an admin adds partitions and shortens retention, told what it does first", async ({
  page,
  browser,
}, testInfo) => {
  test.setTimeout(6 * 60_000);
  const a = fixtures.customers.a;
  const topic = `edited-${fixtures.run}`;
  const group = `edited-reader-${fixtures.run}`;
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
    "--config",
    "retention.ms=86400000",
  ]);
  try {
    // A group that read it, and is caught up.
    kafka(
      a.id,
      properties,
      "kafka-console-producer.sh",
      [
        ...connect(a.id).slice(0, 2),
        "--producer.config",
        "/config/client.properties",
        "--topic",
        topic,
      ],
      "one\ntwo\nthree\n",
    );
    kafka(a.id, properties, "kafka-console-consumer.sh", [
      ...connect(a.id).slice(0, 2),
      "--consumer.config",
      "/config/client.properties",
      "--topic",
      topic,
      "--group",
      group,
      "--from-beginning",
      "--max-messages",
      "3",
      "--timeout-ms",
      "30000",
    ]);

    await page.goto(`/topics/${topic}`);
    const settings = card(page, "Settings");
    await expect(settings).toContainText("Partitions3");
    await expect(settings).toContainText("Retention1 day");
    await settings.getByRole("button", { name: "Edit" }).click();
    const edit = page.getByRole("dialog", { name: `Edit ${topic}` });
    const save = edit.getByRole("button", { name: "Save changes" });
    await expect(save).toBeDisabled();

    // Fewer partitions are refused before anything is sent.
    await edit.getByLabel("Partitions").fill("2");
    await expect(
      edit.getByText("Partitions can be added, never removed: keep at least 3."),
    ).toBeVisible();
    await expect(save).toBeDisabled();

    await edit.getByLabel("Partitions").fill("6");
    await edit.getByRole("combobox", { name: "Retention" }).click();
    await page.getByRole("option", { name: "1 hour" }).click();
    const warnings = edit.getByRole("list", { name: "What this change does" });
    await expect(warnings).toContainText(
      "Kafka then deletes messages older than 1 hour for good, a log segment at a time",
    );
    await expect(warnings).toContainText("a key's new messages may go to another partition");
    await expect(warnings).toContainText("Partitions can be added, never removed.");
    // The group is caught up, so it loses nothing.
    await expect(warnings).not.toContainText(group);
    await page.setViewportSize({ width: 390, height: 844 });
    await expectNoSidewaysScroll(page);
    await page.setViewportSize({ width: 1280, height: 900 });
    await save.click();
    await expect(edit).toBeHidden();
    await expect(settings).toContainText("Partitions6");
    await expect(settings).toContainText("Retention1 hour");

    // Kafka has it, tiered storage kept valid: the brokers keep an hour too.
    expect(
      kafka(a.id, properties, "kafka-topics.sh", [
        ...connect(a.id),
        "--describe",
        "--topic",
        topic,
      ]),
    ).toContain("PartitionCount: 6");
    const configs = kafka(a.id, properties, "kafka-configs.sh", [
      ...connect(a.id),
      "--describe",
      "--entity-type",
      "topics",
      "--entity-name",
      topic,
    ]);
    expect(configs).toContain("retention.ms=3600000");
    expect(configs).toContain("local.retention.ms=3600000");

    // A day again: the brokers keep their default day on disk.
    await settings.getByRole("button", { name: "Edit" }).click();
    await edit.getByRole("combobox", { name: "Retention" }).click();
    await page.getByRole("option", { name: "1 day" }).click();
    await expect(edit.getByRole("list", { name: "What this change does" })).toHaveCount(0);
    await edit.getByRole("button", { name: "Save changes" }).click();
    await expect(edit).toBeHidden();
    await expect(settings).toContainText("Retention1 day");
    const lengthened = kafka(a.id, properties, "kafka-configs.sh", [
      ...connect(a.id),
      "--describe",
      "--entity-type",
      "topics",
      "--entity-name",
      topic,
    ]);
    expect(lengthened).toContain("retention.ms=86400000");
    expect(lengthened).not.toContain("local.retention.ms=");

    // The timeline says who changed it.
    const changes = page.getByRole("region", { name: "Changes" });
    await expect(
      changes.getByText(
        new RegExp(`took topic ${topic} from 3 partitions to 6 partitions in the console`),
      ),
    ).toBeVisible({ timeout: 60_000 });
    await expect(
      changes.getByText(
        new RegExp(`changed the retention of topic ${topic} from 1 day to 1 hour in the console`),
      ),
    ).toBeVisible();

    // The diagnoses note that a key's messages may have moved.
    const note = await expectDiagnosis(page, `${topic} got more partitions`, 3 * 60_000);
    await expect(note).toContainText(
      `${topic} got partitions in the last day, and has 6 partitions now.`,
    );

    // Members see the settings, not the button.
    const context = await browser.newContext();
    try {
      const member = await context.newPage();
      await openOverview(member, fixtures.users.memberA);
      await member.goto(`/topics/${topic}`);
      await expect(card(member, "Settings")).toContainText("Partitions6");
      await expect(card(member, "Settings").getByRole("button", { name: "Edit" })).toHaveCount(0);
    } finally {
      await context.close();
    }
  } finally {
    kafka(a.id, properties, "kafka-topics.sh", [...connect(a.id), "--delete", "--topic", topic]);
  }
});

test("messages too large for their topic are diagnosed, with who sent them", async ({
  page,
}, testInfo) => {
  test.setTimeout(10 * 60_000);
  const a = fixtures.customers.a;
  const topic = `large-${fixtures.run}`;
  const refused = `uploader-${fixtures.run}`;
  const tooLarge = `oversender-${fixtures.run}`;
  await openOverview(page, fixtures.users.adminA);
  const properties = testInfo.outputPath("client.properties");
  await downloadClientProperties(page, properties);
  kafka(a.id, properties, "kafka-topics.sh", [...connect(a.id), "--create", "--topic", topic]);
  const bootstrap = `bootstrap.${a.id}.kafka.changeport.test:9094`;
  // Records of 1.5 MB, a second apart: the topic accepts up to 1 MB.
  const send = (records: number, clientID: string, ...props: string[]) =>
    kafkaInBackground(a.id, properties, "kafka-producer-perf-test.sh", [
      "--topic",
      topic,
      "--num-records",
      String(records),
      "--throughput",
      "1",
      "--record-size",
      "1500000",
      "--producer.config",
      "/config/client.properties",
      "--producer-props",
      `bootstrap.servers=${bootstrap}`,
      `client.id=${clientID}`,
      ...props,
    ]).catch(() => "");
  const running: Promise<unknown>[] = [];
  try {
    // One producer allows itself larger records, so the brokers refuse
    // them: a short burst, which the brokers report from their first
    // refusal, long enough for the producer to push its telemetry once. The
    // other refuses them itself, before they reach Kafka, for a while.
    running.push(send(20, refused, "max.request.size=2000000"), send(120, tooLarge));

    const oversized = await expectDiagnosis(
      page,
      `${topic} refuses messages too large for it`,
      5 * 60_000,
    );
    await expect(oversized).toContainText(
      new RegExp(
        `${topic} refused [\\d.]+ MB sent in batches larger than 1\\.0 MB, the most it accepts`,
      ),
    );
    await expect(oversized).toContainText(`They came from ${refused}.`);
    await expect(oversized).toContainText("object storage");

    const before = await expectDiagnosis(
      page,
      `Records from ${tooLarge} fail before reaching Kafka`,
      3 * 60_000,
    );
    await expect(before).toContainText("max.request.size (1 MB by default)");
    // The refused producer's errors are the topic's diagnosis, not its own.
    await expect(
      card(page, "What needs attention").getByRole("listitem", {
        name: new RegExp(`${refused}$`),
      }),
    ).toHaveCount(0);
    await expectNoSidewaysScroll(page);
  } finally {
    stopKafkaTools(topic);
    kafka(a.id, properties, "kafka-topics.sh", [...connect(a.id), "--delete", "--topic", topic]);
    await Promise.all(running);
  }
});

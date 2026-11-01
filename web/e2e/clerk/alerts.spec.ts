import { expect, test, type Page } from "@playwright/test";
import { kubectl, readFixtures } from "./fixtures";
import {
  connect,
  downloadClientProperties,
  expectNoSidewaysScroll,
  kafka,
  openOverview,
} from "./helpers";

// Alerts about customer A's own applications (docs/alerts.md): a consumer
// group that stops consuming raises an alert in the console, live, and at a
// webhook, and both clear once it catches up. On kind the webhook is the
// alert receiver (deploy/overlays/kind/alert-receiver.yaml), which logs
// each alert with the path it came to.

let fixtures: ReturnType<typeof readFixtures>;
test.beforeAll(() => {
  fixtures = readFixtures();
});

/** The alerts the alert receiver got at path, as it logs them. */
function received(path: string): { status: string; labels: Record<string, string> }[] {
  return kubectl(["-n", "metrics", "logs", "deploy/alert-receiver"])
    .split("\n")
    .filter((line) => line.startsWith("{"))
    .map(
      (line) =>
        JSON.parse(line) as { receiver: string; status: string; labels: Record<string, string> },
    )
    .filter((alert) => alert.receiver === path);
}

/** Opens the Alerts page from the console's navigation. */
async function openAlerts(page: Page): Promise<void> {
  await page
    .getByRole("navigation", { name: "Console" })
    .getByRole("link", { name: /^Alerts/ })
    .click();
  await expect(page.getByRole("heading", { name: "When alerts fire" })).toBeVisible();
}

test("a stalled consumer group raises an alert in the console and at a webhook, until it catches up", async ({
  page,
}, testInfo) => {
  test.setTimeout(12 * 60_000);
  const a = fixtures.customers.a;
  const topic = `alerts-${fixtures.run}`;
  const group = `stalled-${fixtures.run}`;
  const path = `customer-${a.id}`;
  await openOverview(page, fixtures.users.adminA);
  const properties = testInfo.outputPath("client.properties");
  await downloadClientProperties(page, properties);

  // A minute behind is enough, and the webhook gets the alerts too.
  await openAlerts(page);
  const lag = page.getByRole("spinbutton", { name: "A consumer group falls behind, in minutes" });
  await expect(lag).toHaveValue("10");
  await lag.fill("1");
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.getByRole("status").filter({ hasText: "Saved" })).toBeVisible();
  const webhook = page
    .getByRole("listitem")
    .filter({ has: page.getByRole("heading", { name: "Webhook" }) });
  await expect(webhook).toContainText("Not set");
  await webhook.getByRole("button", { name: "Set up" }).click();
  const dialog = page.getByRole("dialog", { name: "Send alerts to Webhook" });
  // The webhook is on the cluster's own network, which the console allows
  // only on kind, as it does plain HTTP.
  await dialog.getByLabel("Webhook URL").fill(`http://alert-receiver.metrics.svc:8080/${path}`);
  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(dialog).toBeHidden();
  await expect(webhook).toContainText("alert-receiver.metrics.svc:8080");
  await expect(page.getByRole("main")).not.toContainText(path);
  await expectNoSidewaysScroll(page);

  // 100 messages that the group never consumes.
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
    Array.from({ length: 100 }, (_, i) => String(i)).join("\n") + "\n",
  );
  kafka(a.id, properties, "kafka-consumer-groups.sh", [
    ...connect(a.id),
    "--group",
    group,
    "--topic",
    topic,
    "--reset-offsets",
    "--to-earliest",
    "--execute",
  ]);

  // The overview shows the alert as it fires, without a reload: once the
  // messages are a minute old, and the agent, the metrics store and the
  // rule have each had their turn, and the rule's minute has passed.
  await page
    .getByRole("navigation", { name: "Console" })
    .getByRole("link", { name: "Cluster" })
    .click();
  const banner = page.getByRole("region", { name: /^\d+ alerts?$/ });
  const alert = banner.getByRole("listitem").filter({ hasText: `Consumer group ${group} is` });
  await expect(alert).toContainText(`behind on ${topic}`, { timeout: 6 * 60_000 });
  const groups = page
    .getByRole("main")
    .locator('[data-slot="card"]')
    .filter({ has: page.getByRole("heading", { name: "Consumer groups" }) });
  await expect(groups.getByRole("row").filter({ hasText: group })).toContainText("Behind");
  await expect(
    page.getByRole("navigation", { name: "Console" }).getByRole("link", { name: /^Alerts/ }),
  ).toHaveAccessibleName(/^Alerts\W*\d+ active$/);
  await expectNoSidewaysScroll(page);
  await expect
    .poll(
      () =>
        received(path)
          .filter((a) => a.labels.consumer_group === group)
          .map((a) => a.status),
      {
        timeout: 2 * 60_000,
      },
    )
    .toContain("firing");
  const firing = received(path).find((a) => a.labels.consumer_group === group);
  expect(firing?.labels).toMatchObject({ alertname: "ConsumerGroupLagHigh", tenant: a.id, topic });

  // The group catches up, and the alert clears in the console and at the
  // webhook.
  kafka(a.id, properties, "kafka-console-consumer.sh", [
    ...connect(a.id),
    "--topic",
    topic,
    "--group",
    group,
    "--max-messages",
    "100",
    "--timeout-ms",
    "60000",
  ]);
  await expect(alert).toBeHidden({ timeout: 4 * 60_000 });
  await expect(groups.getByRole("row").filter({ hasText: group })).not.toContainText("Behind");
  await expect
    .poll(
      () =>
        received(path)
          .filter((a) => a.labels.consumer_group === group)
          .map((a) => a.status),
      {
        timeout: 2 * 60_000,
      },
    )
    .toContain("resolved");
});

test("a customer sees only its own alerts, and members cannot change them", async ({ page }) => {
  await openOverview(page, fixtures.users.memberB);
  await openAlerts(page);
  const main = page.getByRole("main");
  await expect(main.getByRole("heading", { name: "Active" })).toBeVisible();
  await expect(main).not.toContainText(fixtures.customers.a.id);
  await expect(main).not.toContainText(`stalled-${fixtures.run}`);
  await expect(main).toContainText("Ask an admin of your organization to change these.");
  await expect(main.getByRole("button", { name: "Save" })).toHaveCount(0);
  await expect(main.getByRole("button", { name: "Set up" })).toHaveCount(0);
  await expect(main.getByRole("checkbox").first()).toBeDisabled();
  await expectNoSidewaysScroll(page);
});

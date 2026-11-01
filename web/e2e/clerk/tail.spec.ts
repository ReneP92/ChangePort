import { expect, test, type Page } from "@playwright/test";
import { kubectl, readFixtures } from "./fixtures";
import {
  connect,
  downloadClientProperties,
  expectNoSidewaysScroll,
  kafka,
  openOverview,
} from "./helpers";

// A topic's messages on its page (docs/console.md): an admin sees the
// recent ones, then new ones as they arrive, decoded; members are refused;
// every tail is audited, without what the messages hold.

let fixtures: ReturnType<typeof readFixtures>;
test.beforeAll(() => {
  fixtures = readFixtures();
});

/** The console's audit log entries of tails of topic. */
function audited(topic: string): string[] {
  return kubectl(["-n", "changeport", "logs", "deploy/changeport-console"])
    .split("\n")
    .filter((line) => line.includes("topic tail") && line.includes(`topic=${topic}`));
}

/** Asks the console's API as the signed-in user, from the page. */
async function status(page: Page, path: string): Promise<number> {
  return page.evaluate(async (path) => {
    const clerk = (
      globalThis as unknown as { Clerk: { session: { getToken: () => Promise<string> } } }
    ).Clerk;
    const token = await clerk.session.getToken();
    const response = await fetch(path, { headers: { Authorization: `Bearer ${token}` } });
    return response.status;
  }, path);
}

test("an admin sees a topic's messages as they arrive, decoded", async ({ page }, testInfo) => {
  test.setTimeout(4 * 60_000);
  const a = fixtures.customers.a;
  const topic = `tail-${fixtures.run}`;
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
  // A message before the tail starts, which it shows among the recent ones.
  const produce = (lines: string) =>
    kafka(
      a.id,
      properties,
      "kafka-console-producer.sh",
      [
        ...connect(a.id),
        "--topic",
        topic,
        "--reader-property",
        "parse.key=true",
        "--reader-property",
        "parse.headers=true",
      ],
      lines,
    );
  produce('source:checkout\torder-1\t{"id":1,"total":12.5}\n');

  await page.goto(`/topics/${topic}`);
  const card = page
    .getByRole("main")
    .locator('[data-slot="card"]')
    .filter({ has: page.getByRole("heading", { name: "Messages" }) });
  await card.getByRole("button", { name: "Show messages" }).click();
  const messages = card.getByRole("list", { name: "Messages" });
  await expect(messages.getByRole("listitem")).toHaveCount(1, { timeout: 30_000 });
  await expect(card.getByRole("status")).toContainText("Live");

  // New messages arrive while it runs, without a reload.
  produce("source:checkout\torder-2\tsecond, as plain text\n");
  await expect(messages.getByRole("listitem")).toHaveCount(2, { timeout: 30_000 });
  const newest = messages.getByRole("listitem").first();
  await expect(newest).toContainText("Partition 0, offset 1");
  await expect(newest).toContainText("second, as plain text");
  await expect(newest).toContainText("Text");

  // The first, opened: its JSON indented, and its header.
  const first = messages.getByRole("listitem").nth(1);
  await expect(first).toContainText("JSON");
  await first.getByRole("button").click();
  await expect(first.locator("pre").nth(1)).toHaveText('{\n  "id": 1,\n  "total": 12.5\n}');
  await expect(first).toContainText("Header source");
  await expect(first.locator("pre").nth(2)).toHaveText("checkout");
  await expectNoSidewaysScroll(page);

  await card.getByRole("button", { name: "Stop" }).click();
  await expect(card.getByRole("status")).toHaveText("Stopped.");

  // Each tail is audited, with who and which topic, and never what the
  // messages hold.
  await expect.poll(() => audited(topic).length, { timeout: 30_000 }).toBeGreaterThanOrEqual(2);
  const entries = audited(topic);
  expect(
    entries.some(
      (line) => line.includes('msg="topic tail started"') && line.includes(`tenant=${a.id}`),
    ),
  ).toBe(true);
  expect(entries.some((line) => line.includes('msg="topic tail ended"'))).toBe(true);
  for (const line of kubectl(["-n", "changeport", "logs", "deploy/changeport-console"]).split(
    "\n",
  )) {
    expect(line).not.toContain("plain text");
    expect(line).not.toContain("order-1");
  }
});

test("a member is refused a topic's messages", async ({ page }) => {
  await openOverview(page, fixtures.users.memberA);
  await page.goto(`/topics/tail-${fixtures.run}`);
  const card = page
    .getByRole("main")
    .locator('[data-slot="card"]')
    .filter({ has: page.getByRole("heading", { name: "Messages" }) });
  await expect(card).toContainText("Only your organization's admins can read the topic's messages");
  await expect(card.getByRole("button", { name: "Show messages" })).toHaveCount(0);
  expect(await status(page, `/api/customer/topics/tail-${fixtures.run}/tail`)).toBe(403);
});

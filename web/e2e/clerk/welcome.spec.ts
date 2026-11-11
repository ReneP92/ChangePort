import { clerk } from "@clerk/testing/playwright";
import { expect, test, type Page } from "@playwright/test";
import { readFixtures, type UserFixture } from "./fixtures";
import { connect, expectNoSidewaysScroll, kafka } from "./helpers";

// The welcome page (docs/console.md): from a topic to the customer's own
// first message, which shows as it arrives, then the consumer group that
// read it back. Customer A's cluster may have topics from other tests, so
// these open the page directly; where a newcomer lands is in the unit tests
// (web/src/lib/onboarding.test.ts) and the screenshot tour.

let fixtures: ReturnType<typeof readFixtures>;
test.beforeAll(() => {
  fixtures = readFixtures();
});

/** Signs user in and opens the welcome page. */
async function openWelcome(page: Page, user: UserFixture) {
  await page.goto("/");
  await clerk.signIn({ page, emailAddress: user.email });
  await page.goto("/welcome");
  await expect(page.getByRole("heading", { name: "Welcome to Changeport" })).toBeVisible({
    timeout: 20_000,
  });
  const main = page.getByRole("main");
  await expect(main.getByText("Your cluster is ready.")).toBeVisible({ timeout: 20_000 });
  return main;
}

test("an admin creates a topic, connects, and sees their first message arrive", async ({
  page,
}, testInfo) => {
  test.setTimeout(5 * 60_000);
  const a = fixtures.customers.a;
  const topic = `welcome-${fixtures.run}`;
  const group = `welcome-readers-${fixtures.run}`;
  const main = await openWelcome(page, fixtures.users.adminA);
  const steps = page.getByRole("list", { name: "Steps" });
  await expect(steps.getByRole("listitem").first().getByLabel("Done")).toBeVisible();
  await main.getByRole("button", { name: "Next", exact: true }).click();

  // A topic, in the form with the guardrails.
  await expect(main.getByRole("button", { name: "Next once you have a topic" })).toBeDisabled();
  await main.getByLabel("Name").fill(topic);
  await main.getByLabel("Partitions").fill("1");
  await main.getByRole("button", { name: "Create topic" }).click();
  await expect(main.getByRole("status")).toContainText(
    `Your apps send their messages to ${topic}.`,
  );
  await main.getByRole("button", { name: "Next", exact: true }).click();

  // The examples in Python, Go and JavaScript, with the variables they read.
  await expect(main.getByRole("tab")).toHaveText(["Python", "Go", "JavaScript (Node.js)"]);
  await expect(main.getByText(`export TOPIC=${topic}`)).toBeVisible();
  await main.getByRole("tab", { name: "Go" }).click();
  await expect(main.getByText("github.com/twmb/franz-go").first()).toBeVisible();
  await expectNoSidewaysScroll(page);
  const downloading = page.waitForEvent("download");
  await main.getByRole("button", { name: "Download client.properties" }).click();
  const download = await downloading;
  // Under the name the examples read it by.
  expect(download.suggestedFilename()).toBe("client.properties");
  const properties = testInfo.outputPath("client.properties");
  await download.saveAs(properties);
  await main.getByRole("button", { name: "Next", exact: true }).click();

  // The message shows as it arrives, then the group that read it.
  await expect(main.getByRole("status").first()).toHaveText(`Waiting for a message on ${topic}`);
  kafka(
    a.id,
    properties,
    "kafka-console-producer.sh",
    [...connect(a.id), "--topic", topic],
    "hello from the welcome page\n",
  );
  const messages = main.getByRole("list", { name: "Messages" });
  await expect(messages.getByRole("listitem")).toHaveCount(1, { timeout: 30_000 });
  await expect(messages).toContainText("hello from the welcome page");
  await expect(main.getByText(`Your message arrived on ${topic}.`)).toBeVisible();
  await expect(steps.getByRole("listitem").last().getByLabel("Done")).toBeVisible();
  kafka(a.id, properties, "kafka-console-consumer.sh", [
    ...connect(a.id),
    "--topic",
    topic,
    "--group",
    group,
    "--from-beginning",
    "--max-messages",
    "1",
    "--timeout-ms",
    "30000",
  ]);
  // Once the agent has measured the group's lag.
  await expect(main.getByText(`Consumer group ${group} read it.`)).toBeVisible({
    timeout: 2 * 60_000,
  });

  // Done, the customer goes to their cluster, and stays there.
  await main.getByRole("button", { name: "Go to your cluster" }).click();
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole("heading", { name: "Connection" })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("heading", { name: "Connection" })).toBeVisible({ timeout: 20_000 });
});

test("members follow the steps, and ask an admin for the credentials", async ({ page }) => {
  const main = await openWelcome(page, fixtures.users.memberA);
  await main.getByRole("button", { name: "Next", exact: true }).click();
  await expect(main.getByRole("button", { name: "Create topic" })).toHaveCount(0);
});

test("the welcome page can be skipped, and the Connection card opens it again", async ({
  page,
}) => {
  const main = await openWelcome(page, fixtures.users.adminA);
  await main.getByRole("button", { name: "Skip to the overview" }).click();
  await expect(page).toHaveURL(/\/$/);
  const connection = page.getByRole("heading", { name: "Connection" });
  await expect(connection).toBeVisible();
  await page.getByRole("link", { name: "Open the getting started guide" }).click();
  await expect(page).toHaveURL(/\/welcome$/);
  await expect(page.getByRole("heading", { name: "Welcome to Changeport" })).toBeVisible();
});

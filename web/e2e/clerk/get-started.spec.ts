import { clerk } from "@clerk/testing/playwright";
import { expect, test, type Page } from "@playwright/test";
import { kubectl, readFixtures, type UserFixture } from "./fixtures";
import {
  connect,
  downloadClientProperties,
  expectNoSidewaysScroll,
  kafka,
  kafkaInBackground,
} from "./helpers";

// The guided first ten minutes on the overview (docs/console.md): the
// sample app runs in customer A's cluster and shows on the data flow map,
// its topic's messages and its consumer group; the customer's own topic and
// app tick off the steps; the guide hides, and comes back.

let fixtures: ReturnType<typeof readFixtures>;
test.beforeAll(() => {
  fixtures = readFixtures();
});

/**
 * Opens the overview with the guide shown, as a viewer who chose to see it:
 * other tests give customer A topics and traffic of its own, after which
 * the guide no longer shows by itself.
 */
async function openGuide(page: Page, user: UserFixture) {
  const key = `changeport.get-started.${fixtures.customers.a.id}`;
  await page.addInitScript((key) => {
    if (sessionStorage.getItem("guide-reset") === null) {
      localStorage.setItem(key, "shown");
      sessionStorage.setItem("guide-reset", "done");
    }
  }, key);
  await page.goto("/");
  await clerk.signIn({ page, emailAddress: user.email });
  await page.goto("/");
  const guide = page.getByRole("region", { name: "Get started" });
  await expect(guide).toBeVisible({ timeout: 20_000 });
  return guide;
}

function sampleObjects(namespace: string): string {
  return kubectl([
    "-n",
    namespace,
    "get",
    "kafkauser/sample",
    "kafkatopic/sample.orders",
    "deployment/changeport-sample",
    "--ignore-not-found",
    "-o",
    "name",
  ]).trim();
}

test("an admin runs the sample app and watches it on the map, its messages and its group", async ({
  page,
}) => {
  test.setTimeout(8 * 60_000);
  const a = fixtures.customers.a;
  const namespace = `customer-${a.id}`;
  const guide = await openGuide(page, fixtures.users.adminA);
  const watch = guide
    .getByRole("listitem")
    .filter({ has: page.getByRole("heading", { name: "Watch Kafka at work" }) });
  try {
    await watch.getByRole("button", { name: "Start the sample" }).click();
    await expect(watch.getByRole("status")).toHaveText(/Starting the sample/);
    await expect(watch.getByRole("status")).toHaveText(/The sample runs until/, {
      timeout: 4 * 60_000,
    });

    // The map shows its producer, as the sample user, once the brokers have
    // its metrics, and the group that reads its topic.
    const map = page.getByRole("group", { name: "Topology map" });
    await expect(map.getByTitle(/^sample-shop: sample, /)).toBeVisible({ timeout: 4 * 60_000 });
    await expect(map.getByRole("link", { name: /^sample\.order-emails/ })).toBeVisible({
      timeout: 2 * 60_000,
    });
    const topicRow = page.getByRole("main").getByRole("row").filter({ hasText: "sample.orders" });
    await expect(topicRow.getByText("Sample", { exact: true })).toBeVisible();
    await expect(topicRow.getByRole("button", { name: /^Delete / })).toHaveCount(0);

    // Keeping it running moves its stop time to an hour from now.
    const stopAt = () =>
      kubectl(["-n", namespace, "get", "sampleapp", "sample", "-o", "jsonpath={.spec.stopAt}"]);
    const before = stopAt();
    await page.waitForTimeout(1_500);
    await watch.getByRole("button", { name: "Keep it running" }).click();
    await expect(() => {
      expect(Date.parse(stopAt())).toBeGreaterThan(Date.parse(before));
    }).toPass({ timeout: 10_000 });

    // Its messages are order events, keyed by order ID.
    await watch.getByRole("link", { name: "its messages" }).click();
    await expect(page).toHaveURL(/\/topics\/sample\.orders$/);
    const messages = page
      .getByRole("main")
      .locator('[data-slot="card"]')
      .filter({ has: page.getByRole("heading", { name: "Messages" }) });
    await messages.getByRole("button", { name: "Show messages" }).click();
    await expect(
      messages.getByRole("list", { name: "Messages" }).getByRole("listitem").first(),
    ).toContainText('"orderId":"ord-', { timeout: 30_000 });

    // Its consumer group keeps up.
    await page.goBack();
    await guide.getByRole("link", { name: "the email service" }).click();
    await expect(page).toHaveURL(/\/consumer-groups\/sample\.order-emails$/);
    await expect(page.getByRole("main").getByText("sample.orders").first()).toBeVisible();
    await page.goBack();

    await watch.getByRole("button", { name: "Stop the sample" }).click();
    await expect(watch.getByRole("button", { name: "Start the sample" })).toBeVisible();
    await expect(() => {
      expect(sampleObjects(namespace)).toBe("");
    }).toPass({ timeout: 2 * 60_000 });
  } finally {
    kubectl(["-n", namespace, "delete", "sampleapp", "sample", "--ignore-not-found"]);
  }
});

test("members see the guide, and are told to ask an admin", async ({ page }) => {
  const guide = await openGuide(page, fixtures.users.memberA);
  await expect(guide.getByText("Ask an admin of your organization to start it.")).toBeVisible();
  await expect(guide.getByRole("button", { name: "Start the sample" })).toHaveCount(0);
  await expect(guide.getByRole("button", { name: "Download client.properties" })).toHaveCount(0);
});

test("the customer's own topic and app tick off the guide's steps", async ({ page }, testInfo) => {
  test.setTimeout(6 * 60_000);
  const a = fixtures.customers.a;
  const guide = await openGuide(page, fixtures.users.adminA);
  const topic = `guide-${fixtures.run}`;
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
  const producing = kafkaInBackground(a.id, properties, "kafka-producer-perf-test.sh", [
    "--topic",
    topic,
    "--num-records",
    "9000",
    "--record-size",
    "100",
    "--throughput",
    "50",
    "--producer.config",
    "/config/client.properties",
    "--producer-props",
    `bootstrap.servers=${connect(a.id)[1] ?? ""}`,
  ]);

  const step = (title: string) =>
    guide.getByRole("listitem").filter({ has: page.getByRole("heading", { name: title }) });
  await expect(step("Create a topic for your data").getByLabel("Done")).toBeVisible({
    timeout: 60_000,
  });
  const connectStep = step("Connect your app");
  // Each language's example, from the connection guide.
  for (const language of ["Python", "Go", "Node.js", "Command line", "Java"]) {
    await connectStep.getByRole("tab", { name: language }).click();
    await expect(connectStep.getByRole("tabpanel")).toBeVisible();
  }
  await expect(connectStep.getByText(/^export KAFKA_BOOTSTRAP=bootstrap\./)).toBeVisible();
  await expect(connectStep.getByLabel("Done")).toBeVisible({ timeout: 4 * 60_000 });
  // Other tests' apps on customer A may be what ticks it off first.
  await expect(connectStep.getByRole("status")).toHaveText(
    /^(Data arrives on |Consumer group .* reads )/,
  );
  await producing.catch(() => "");
});

test("the guide hides, and the Connection card brings it back", async ({ page }) => {
  const guide = await openGuide(page, fixtures.users.adminA);
  await expectNoSidewaysScroll(page);
  await guide.getByRole("button", { name: "Hide" }).click();
  await expect(guide).toHaveCount(0);
  await page.reload();
  const connection = page
    .getByRole("main")
    .locator('[data-slot="card"]')
    .filter({
      has: page.getByRole("heading", { name: "Connection" }),
    });
  await expect(connection.getByRole("heading", { name: "Connection" })).toBeVisible();
  await expect(guide).toHaveCount(0);
  await connection.getByRole("button", { name: "Show the getting started guide" }).click();
  await expect(guide).toBeVisible();
});

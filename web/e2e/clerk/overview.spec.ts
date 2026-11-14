import { expect, test } from "@playwright/test";
import { readFixtures } from "./fixtures";
import { card, connect, kafka, openOverview } from "./helpers";

// The overview of customer A's cluster, which the setup waits to be ready.

let fixtures: ReturnType<typeof readFixtures>;
test.beforeAll(() => {
  fixtures = readFixtures();
});

test("an admin downloads client.properties and produces with it", async ({ page }, testInfo) => {
  test.setTimeout(4 * 60_000);
  await openOverview(page, fixtures.users.adminA);
  const a = fixtures.customers.a;
  const bootstrap = `bootstrap.${a.id}.kafka.changeport.test:9094`;
  const main = page.getByRole("main");
  await expect(main.getByText(bootstrap)).toBeVisible();
  await expect(main.getByText("The broker is ready; every partition is in sync")).toBeVisible();

  const downloading = page.waitForEvent("download");
  await card(page, "Connection")
    .getByRole("button", { name: "Download client.properties" })
    .click();
  const download = await downloading;
  expect(download.suggestedFilename()).toBe(`${a.id}-client.properties`);
  const properties = testInfo.outputPath("client.properties");
  await download.saveAs(properties);

  // A client outside the cluster connects with nothing but the file.
  const topic = `overview-${fixtures.run}`;
  const config = connect(a.id);
  kafka(a.id, properties, "kafka-topics.sh", [
    ...config,
    "--create",
    "--topic",
    topic,
    "--partitions",
    "3",
  ]);
  kafka(
    a.id,
    properties,
    "kafka-console-producer.sh",
    [...config, "--topic", topic],
    "one\ntwo\nthree\n",
  );

  // The topic shows, with its size once the brokers' metrics have it.
  const row = main.getByRole("row").filter({ hasText: topic });
  await expect(async () => {
    await page.reload();
    await expect(row).toBeVisible();
    await expect(row.getByRole("cell").nth(4)).not.toHaveText("–");
  }).toPass({ timeout: 150_000, intervals: [10_000] });
  await expect(row.getByRole("cell").nth(1)).toHaveText("3");
  await expect(row.getByText("Tiered")).toBeVisible();
});

// A user belongs to one organization, their company's: the sidebar names
// it and leads to its team, with no other organization to switch to.
test("the sidebar names the user's one organization", async ({ page }) => {
  await openOverview(page, fixtures.users.adminA);
  const organization = page.getByRole("link", { name: `E2E ${fixtures.run} A` });
  await expect(organization).toBeVisible();
  await expect(page.locator(".cl-organizationSwitcherTrigger")).toHaveCount(0);
  await organization.click();
  await expect(page).toHaveURL(/\/team$/);
  await expect(page.getByRole("heading", { name: "Team", level: 1 })).toBeVisible();
});

test("a member sees how to connect, but not the credentials", async ({ page }) => {
  await openOverview(page, fixtures.users.memberA);
  const main = page.getByRole("main");
  const connection = card(page, "Connection");
  await expect(
    connection.getByText(`bootstrap.${fixtures.customers.a.id}.kafka.changeport.test:9094`),
  ).toBeVisible();
  await expect(main.getByRole("button", { name: "Download client.properties" })).toHaveCount(0);
  await expect(connection.getByText("Ask an admin of your organization")).toBeVisible();
});

test("the throughput chart switches ranges", async ({ page }) => {
  await openOverview(page, fixtures.users.memberA);
  const ranges = page.getByRole("group", { name: "Range" });
  await expect(ranges.getByRole("button", { name: "1h" })).toHaveAttribute("aria-pressed", "true");
  await ranges.getByRole("button", { name: "24h" }).click();
  await expect(ranges.getByRole("button", { name: "24h" })).toHaveAttribute("aria-pressed", "true");
  await expect(ranges.getByRole("button", { name: "1h" })).toHaveAttribute("aria-pressed", "false");
});

import { clerk } from "@clerk/testing/playwright";
import { expect, test } from "@playwright/test";
import { kubectl, readFixtures } from "./fixtures";
import { card, openOverview } from "./helpers";

// Environments (docs/console.md): an operator adds a staging environment to
// customer A's organization, a cluster of its own; the organization's admin
// switches to it, and its topics are its own.

let fixtures: ReturnType<typeof readFixtures>;
test.beforeAll(() => {
  fixtures = readFixtures();
});

test("an operator adds an environment, and its topics are its own", async ({ page, browser }) => {
  // Creating a cluster takes a minute or two.
  test.setTimeout(8 * 60_000);
  const a = fixtures.customers.a;
  let staging = "";
  try {
    // The operator adds staging on customer A's page.
    const operator = await (await browser.newContext()).newPage();
    await operator.goto("/");
    await clerk.signIn({ page: operator, emailAddress: fixtures.users.staff.email });
    await operator.goto(`/operator/customers/${a.id}`);
    const environments = card(operator, "Environments");
    await expect(environments.getByRole("list", { name: "Environments" })).toContainText(
      "production",
    );
    await environments.getByRole("button", { name: "Add environment" }).click();
    const dialog = operator.getByRole("dialog", { name: /^Add an environment to / });
    // Production is taken, so staging is suggested.
    await expect(dialog.getByLabel("Name")).toHaveValue("staging");
    await dialog.getByRole("button", { name: "Add environment" }).click();
    await expect(operator).toHaveURL(/\/operator\/customers\/c-[a-km-z2-9]{6}$/, {
      timeout: 30_000,
    });
    staging = new URL(operator.url()).pathname.split("/").at(-1) ?? "";
    await expect(
      operator.getByRole("main").getByText(`Customer ${staging}, the staging environment`),
    ).toBeVisible();
    await expect(operator.getByRole("main").getByText("Ready", { exact: true })).toBeVisible({
      timeout: 6 * 60_000,
    });

    // The admin switches to it: a new cluster, so its welcome page first.
    await openOverview(page, fixtures.users.adminA);
    await expect(page).toHaveURL(/\/e\/production$/);
    const switcher = page.getByRole("banner").getByRole("combobox", { name: "Environment" });
    await expect(switcher).toHaveText("production");
    await switcher.click();
    await page.getByRole("option", { name: "staging" }).click();
    await expect(page).toHaveURL(/\/e\/staging\/welcome$/);
    await page.getByRole("button", { name: "Skip to the overview" }).click();
    await expect(page).toHaveURL(/\/e\/staging$/);
    const main = page.getByRole("main");
    await expect(main.getByText(`Customer ${staging}, the staging environment`)).toBeVisible();
    await expect(card(page, "Connection")).toContainText(
      `bootstrap.${staging}.kafka.changeport.test:9094`,
    );

    // A topic in staging is staging's alone.
    const topic = `staging-${fixtures.run}`;
    await card(page, "Topics").getByRole("button", { name: "Create topic" }).click();
    const create = page.getByRole("dialog", { name: "Create a topic" });
    await create.getByLabel("Name").fill(topic);
    await create.getByLabel("Partitions").fill("1");
    await create.getByRole("button", { name: "Create topic" }).click();
    await expect(create).toBeHidden();
    await expect(card(page, "Topics").getByRole("link", { name: topic })).toBeVisible();
    await switcher.click();
    await page.getByRole("option", { name: "production" }).click();
    await expect(page).toHaveURL(/\/e\/production$/);
    await expect(card(page, "Topics").getByRole("heading", { name: "Topics" })).toBeVisible();
    await expect(card(page, "Topics").getByRole("link", { name: topic })).toHaveCount(0);

    // A link into staging opens staging.
    await page.goto(`/e/staging/topics/${topic}`);
    await expect(page.getByRole("heading", { level: 1, name: topic })).toBeVisible({
      timeout: 20_000,
    });
    await expect(switcher).toHaveText("staging");
  } finally {
    // The cluster goes again, to spare the kind cluster's memory.
    if (staging !== "") {
      kubectl(["delete", "customer", staging, "--wait=false"]);
    }
  }
});

test("an environment the organization does not have says so", async ({ page }) => {
  await openOverview(page, fixtures.users.memberA);
  await page.goto("/e/qa");
  await expect(page.getByRole("heading", { name: "No such environment" })).toBeVisible({
    timeout: 20_000,
  });
  await page.getByRole("link", { name: "production" }).click();
  await expect(page).toHaveURL(/\/e\/production$/);
});

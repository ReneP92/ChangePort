import { clerk } from "@clerk/testing/playwright";
import { expect, test, type Page } from "@playwright/test";
import {
  clerkClient,
  kubectl,
  readFixtures,
  runLabel,
  testEmail,
  unprotectTestCustomer,
} from "./fixtures";
import { card, expectNoSidewaysScroll, openOverview } from "./helpers";

// Environments (docs/console.md): an operator adds a staging environment to
// customer A's organization, a cluster of its own; the organization's admin
// switches to it, and its topics are its own. An organization's admins
// manage its environments themselves, and its members see them.

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

/** Signs user in and opens the organization's environments. */
async function openEnvironments(page: Page, email: string) {
  await page.goto("/");
  await clerk.signIn({ page, emailAddress: email });
  await page.goto("/environments");
  await expect(page.getByRole("heading", { level: 1, name: "Environments" })).toBeVisible({
    timeout: 20_000,
  });
}

test("an admin adds, renames, protects and removes environments, and picks the default", async ({
  page,
  browser,
}) => {
  test.setTimeout(5 * 60_000);
  // An organization of the test's own, as choosing the default changes
  // where every member of it lands.
  const clerkAPI = clerkClient();
  const run = fixtures.run;
  const organization = await clerkAPI.organizations.createOrganization({ name: `E2E ${run} E` });
  const users: string[] = [];
  try {
    const user = async (role: string, membership: "org:admin" | "org:member") => {
      const email = testEmail(run, role);
      const created = await clerkAPI.users.createUser({
        emailAddress: [email],
        firstName: "E2E",
        lastName: role,
        skipPasswordRequirement: true,
      });
      users.push(created.id);
      await clerkAPI.organizations.createOrganizationMembership({
        organizationId: organization.id,
        userId: created.id,
        role: membership,
      });
      return email;
    };
    const admin = await user("admin-e", "org:admin");
    const member = await user("member-e", "org:member");
    // Its production environment, which needs not be ready for this.
    kubectl(
      ["apply", "--server-side", "--field-manager=changeport-e2e", "-f", "-"],
      JSON.stringify({
        apiVersion: "changeport.io/v1alpha1",
        kind: "Customer",
        metadata: { name: `${run}-e`, labels: { [runLabel]: run } },
        spec: {
          displayName: `E2E ${run} E`,
          profile: "dev",
          clerkOrganizationID: organization.id,
        },
      }),
    );

    await openEnvironments(page, admin);
    const environments = card(page, "Your environments");
    const list = environments.getByRole("list", { name: "Environments" });
    const row = (name: string) => list.getByRole("listitem", { name, exact: true });
    await expect(row("production")).toContainText("Default");
    await expect(environments).toContainText("1 of at most 5");

    // Adding staging, which production leaves to suggest.
    await environments.getByRole("button", { name: "Add environment" }).click();
    const add = page.getByRole("dialog", { name: "Add an environment" });
    await expect(add.getByLabel("Name")).toHaveValue("staging");
    await add.getByRole("button", { name: "Add environment" }).click();
    await expect(add).toBeHidden({ timeout: 20_000 });
    await expect(row("staging")).toContainText("dev: One broker");
    await expectNoSidewaysScroll(page);

    // Renaming it to qa.
    await row("staging").getByRole("button", { name: "Rename staging" }).click();
    const rename = page.getByRole("dialog", { name: "Rename staging" });
    await rename.getByLabel("New name").fill("production");
    await rename.getByRole("button", { name: "Rename" }).click();
    await expect(rename).toContainText("has an environment production already");
    await rename.getByLabel("New name").fill("qa");
    await rename.getByRole("button", { name: "Rename" }).click();
    await expect(rename).toBeHidden();
    await expect(row("qa")).toBeVisible();
    await expect(row("staging")).toHaveCount(0);

    // Making qa the default: the first, where members land.
    await row("qa").getByRole("button", { name: "Make qa the default" }).click();
    await expect(row("qa")).toContainText("Default");
    await expect(list.getByRole("listitem").first()).toHaveAccessibleName("qa");
    await expect(row("production")).not.toContainText("Default");
    const memberPage = await (await browser.newContext()).newPage();
    await openEnvironments(memberPage, member);
    await memberPage.goto("/");
    await expect(memberPage).toHaveURL(/\/e\/qa(\/welcome)?$/, { timeout: 20_000 });
    // Members see the environments, and change nothing.
    await memberPage.goto("/environments");
    const memberList = card(memberPage, "Your environments");
    await expect(memberList).toContainText("Ask an admin of your organization");
    await expect(memberList.getByRole("listitem", { name: "qa", exact: true })).toBeVisible();
    await expect(memberList.getByRole("button")).toHaveCount(0);

    // Protected, qa cannot be removed until protection is off again.
    await row("qa").getByRole("button", { name: "Protect qa" }).click();
    await expect(row("qa")).toContainText("Protected");
    await row("qa").getByRole("button", { name: "Remove qa" }).click();
    const refused = page.getByRole("dialog", { name: "qa cannot be removed" });
    await expect(refused).toContainText("deletion protection is on");
    await refused.getByRole("button", { name: "OK" }).click();
    await row("qa").getByRole("button", { name: "Turn off deletion protection of qa" }).click();
    const unprotect = page.getByRole("dialog", { name: "Turn off deletion protection of qa?" });
    await expect(unprotect.getByRole("button", { name: "Turn off" })).toBeDisabled();
    await unprotect.getByLabel("Type qa to confirm").fill("qa");
    await unprotect.getByRole("button", { name: "Turn off" }).click();
    await expect(unprotect).toBeHidden();
    await expect(row("qa")).not.toContainText("Protected");

    // Removing qa; production, the last left, stays.
    await row("qa").getByRole("button", { name: "Remove qa" }).click();
    const remove = page.getByRole("dialog", { name: "Remove qa?" });
    await remove.getByLabel("Type qa to confirm").fill("qa");
    await remove.getByRole("button", { name: "Remove" }).click();
    await expect(remove).toBeHidden();
    await expect(row("qa")).toContainText("Being removed");
    await expect(row("production")).toContainText("Default");
    await row("production").getByRole("button", { name: "Remove production" }).click();
    await expect(page.getByRole("dialog", { name: "production cannot be removed" })).toContainText(
      "keeps at least one environment",
    );
  } finally {
    // The clusters go again, to spare the kind cluster's memory, protected
    // or not.
    const selector = `changeport.io/clerk-organization=${organization.id}`;
    for (const name of kubectl([
      "get",
      "customers",
      "-l",
      selector,
      "-o",
      "jsonpath={.items[*].metadata.name}",
    ])
      .split(" ")
      .filter(Boolean)) {
      unprotectTestCustomer(name);
    }
    kubectl(["delete", "customers", "-l", selector, "--wait=false"]);
    for (const id of users) {
      await clerkAPI.users.deleteUser(id);
    }
    await clerkAPI.organizations.deleteOrganization(organization.id);
  }
});

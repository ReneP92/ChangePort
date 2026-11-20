import { clerk, setupClerkTestingToken } from "@clerk/testing/playwright";
import { expect, test, type APIRequestContext, type Browser, type Page } from "@playwright/test";
import { readFixtures, skipWelcome, type UserFixture } from "./fixtures";
import { openWorkspaceMenu } from "./helpers";

// Signed in as this run's test users (clerk.setup.ts), against the console on
// kind with a real Clerk development instance. Customers are looked for by
// their IDs in the page's main content: the sidebar names the organization.

// Playwright loads this file before the setup project has run, so the
// fixtures are read in the tests.
let fixtures: ReturnType<typeof readFixtures>;
let a: (typeof fixtures)["customers"]["a"];
let b: (typeof fixtures)["customers"]["b"];
test.beforeAll(() => {
  fixtures = readFixtures();
  ({ a, b } = fixtures.customers);
});

/** Signs in as user through a Clerk sign-in ticket, and opens the console. */
async function signIn(page: Page, user: UserFixture): Promise<void> {
  await skipWelcome(page);
  await page.goto("/");
  await clerk.signIn({ page, emailAddress: user.email });
  await page.goto("/");
}

/**
 * Signs user in, in a browser context of their own, and returns their session
 * token as the web app sends it.
 */
async function sessionToken(browser: Browser, user: UserFixture): Promise<string> {
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    await signIn(page, user);
    // The app has loaded the session once it shows the signed-in sidebar.
    await expect(page.getByRole("button", { name: "Workspace menu" })).toBeVisible();
    const token = await page.evaluate(async () => {
      const window = globalThis as unknown as {
        Clerk: { session: { getToken(): Promise<string | null> } | null };
      };
      return (await window.Clerk.session?.getToken()) ?? null;
    });
    if (!token) {
      throw new Error(`${user.email} has no session token`);
    }
    return token;
  } finally {
    await context.close();
  }
}

/** Calls the console's API with token, returning the status and body. */
/**
 * The line of the main content that names the customer whose cluster the
 * page shows, by its ID: the sidebar names the organization.
 */
function cluster(page: Page, customer: { id: string }) {
  return page.getByRole("main").getByText(`Customer ${customer.id}, `);
}

async function call(request: APIRequestContext, path: string, token: string) {
  const response = await request.get(path, { headers: { Authorization: `Bearer ${token}` } });
  return { status: response.status(), body: (await response.json()) as Record<string, unknown> };
}

test("a customer signs in with the form and sees their cluster", async ({ page }) => {
  await skipWelcome(page);
  await setupClerkTestingToken({ page });
  await page.goto("/");
  await page.getByLabel(/email address/i).fill(fixtures.users.formA.email);
  // Clerk shows the code step before it has sent the code; a code entered
  // sooner, which no person manages, is refused.
  const codeSent = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      response.url().includes("/prepare_first_factor") &&
      response.ok(),
  );
  await page.getByRole("button", { name: "Continue", exact: true }).click();
  await codeSent;
  await expect(page.getByRole("heading", { name: "Check your email" })).toBeVisible();
  // Clerk's test addresses take this code instead of an email.
  await page.getByRole("textbox", { name: "Enter verification code" }).pressSequentially("424242");
  await expect(
    page
      .getByRole("main")
      .getByText(`Customer ${a.id}, the production environment on a dev cluster`),
  ).toBeVisible({ timeout: 20_000 });
  await expect(page.getByRole("button", { name: "Workspace menu" })).toHaveText(a.displayName);
});

test("a customer only ever sees their own cluster", async ({ page }) => {
  await signIn(page, fixtures.users.memberB);
  await expect(cluster(page, b)).toBeVisible();
  await expect(cluster(page, a)).toHaveCount(0);
});

test("a customer cannot open the operator console", async ({ page }) => {
  await signIn(page, fixtures.users.adminA);
  await expect(cluster(page, a)).toBeVisible();
  await page.goto("/operator");
  await expect(page.getByRole("heading", { name: "Operators only" })).toBeVisible();
  await expect(cluster(page, b)).toHaveCount(0);
});

test("an operator lands on the fleet, and sees every customer", async ({ page }) => {
  await signIn(page, fixtures.users.staff);
  await expect(page.getByRole("heading", { name: "Fleet", level: 1 })).toBeVisible();
  await expect(page).toHaveURL(/\/operator\/fleet$/);
  // By ID: environments.spec.ts may add an environment to A meanwhile, a
  // customer of the same name.
  for (const customer of [a, b]) {
    await expect(
      page.getByRole("main").getByRole("row").filter({ hasText: customer.id }),
    ).toContainText(customer.displayName);
  }
  await page
    .getByRole("navigation", { name: "Operator console" })
    .getByRole("link", { name: "Customers" })
    .click();
  await expect(page.getByRole("heading", { name: "Customers", level: 1 })).toBeVisible();
  for (const customer of [a, b]) {
    await expect(
      page.getByRole("main").getByRole("row").filter({ hasText: customer.id }),
    ).toContainText(customer.displayName);
  }
});

test("the API scopes customers to their organization and operators to staff", async ({
  browser,
  request,
}) => {
  // Four users sign in, one after the other.
  test.setTimeout(120_000);
  const memberA = await sessionToken(browser, fixtures.users.memberA);
  expect(await call(request, "/api/me", memberA)).toMatchObject({
    status: 200,
    body: { access: "customer", customerID: a.id, admin: false },
  });
  expect(await call(request, "/api/customer", memberA)).toMatchObject({
    status: 200,
    body: { id: a.id },
  });
  for (const path of ["/api/operator/customers", `/api/operator/customers/${b.id}`]) {
    expect((await call(request, path, memberA)).status).toBe(403);
  }
  // Only an organization's admins may download its credentials.
  expect((await call(request, "/api/customer/client-properties", memberA)).status).toBe(403);

  const adminA = await sessionToken(browser, fixtures.users.adminA);
  expect(await call(request, "/api/me", adminA)).toMatchObject({
    status: 200,
    body: { access: "customer", customerID: a.id, admin: true },
  });

  const memberB = await sessionToken(browser, fixtures.users.memberB);
  expect(await call(request, "/api/customer", memberB)).toMatchObject({
    status: 200,
    body: { id: b.id },
  });

  const staff = await sessionToken(browser, fixtures.users.staff);
  expect(await call(request, "/api/me", staff)).toMatchObject({
    status: 200,
    body: { access: "operator" },
  });
  const list = await call(request, "/api/operator/customers", staff);
  expect(list.status).toBe(200);
  const ids = (list.body.customers as { id: string }[]).map((c) => c.id);
  expect(ids).toEqual(expect.arrayContaining([a.id, b.id]));
  expect(await call(request, `/api/operator/customers/${a.id}`, staff)).toMatchObject({
    status: 200,
    body: { id: a.id },
  });
  // The staff organization is no customer.
  expect((await call(request, "/api/customer", staff)).status).toBe(403);
});

test("signing out returns to the sign-in form", async ({ page }) => {
  await signIn(page, fixtures.users.signOutA);
  await expect(cluster(page, a)).toBeVisible();
  const menu = await openWorkspaceMenu(page);
  await menu.getByRole("menuitem", { name: "Sign out" }).click();
  // Signing out reloads Clerk before the form shows again.
  await expect(page.getByLabel(/email address/i)).toBeVisible({ timeout: 20_000 });
  await expect(cluster(page, a)).toHaveCount(0);
});

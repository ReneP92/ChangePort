import { clerk, setupClerkTestingToken } from "@clerk/testing/playwright";
import { expect, test, type Browser, type Page } from "@playwright/test";
import { clerkClient, kubectl, readFixtures, testEmail } from "./fixtures";
import { expectNoSidewaysScroll } from "./helpers";

// Trial requests (docs/console.md): a visitor signs up and requests a trial,
// and an operator approves it, which takes the visitor to their new cluster,
// or declines it, whose reason the visitor sees. The companies are named like
// the run's organizations, so that the teardown and the janitor remove their
// requests, organizations and customers; the visitors are test users, which
// these tests delete themselves.

let fixtures: ReturnType<typeof readFixtures>;
const visitors: string[] = [];
test.beforeAll(() => {
  fixtures = readFixtures();
});
test.afterAll(async () => {
  const clerkAPI = clerkClient();
  for (const id of visitors) {
    kubectl(["delete", "trialrequests", "-l", `changeport.io/clerk-user=${id}`, "--wait=false"]);
    await clerkAPI.users.deleteUser(id);
  }
});

/** Fills in and sends the trial request form for company. */
async function requestTrial(page: Page, company: string): Promise<void> {
  const main = page.getByRole("main");
  await expect(main.getByRole("heading", { name: "Request a trial" })).toBeVisible({
    timeout: 20_000,
  });
  await main.getByLabel("Company").fill(company);
  await main.getByLabel("Your role").fill("Staff engineer");
  await main.getByLabel("What will you use Kafka for?").fill("Order events between services");
  await main.getByRole("radio", { name: "Under 1 MB/s" }).check();
  await main.getByRole("radio", { name: "Up to a week" }).check();
  await expectNoSidewaysScroll(page);
  await main.getByRole("button", { name: "Request a trial" }).click();
  await expect(main.getByRole("heading", { name: "Your request is in" })).toBeVisible();
  await expect(main.getByText(company)).toBeVisible();
}

/**
 * Signs the operator in, in a browser context of their own, and opens the
 * request of company in the operator console.
 */
async function openRequest(browser: Browser, company: string) {
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.goto("/");
  await clerk.signIn({ page, emailAddress: fixtures.users.staff.email });
  await page.goto("/operator");
  await page
    .getByRole("navigation", { name: "Operator console" })
    .getByRole("link", { name: /^Trial requests/ })
    .click();
  await expect(page.getByRole("heading", { name: "Trial requests" })).toBeVisible();
  const item = page.getByRole("listitem", { name: new RegExp(`^${company}, tr-`) });
  await expect(item).toBeVisible();
  await expectNoSidewaysScroll(page);
  return { context, page, item };
}

test("a visitor signs up, requests a trial, an operator approves, and the visitor lands in their cluster", async ({
  page,
  browser,
}) => {
  // Creating the cluster takes a minute or two.
  test.setTimeout(8 * 60_000);
  const company = `E2E ${fixtures.run} T`;
  const email = testEmail(fixtures.run, "visitor");

  // Signing up, with the code Clerk's test addresses take; the testing
  // token lets the browser past Clerk's bot protection.
  await setupClerkTestingToken({ page });
  await page.goto("/trial");
  await expect(page.getByRole("heading", { name: "Try Changeport" })).toBeVisible({
    timeout: 20_000,
  });
  await page.getByLabel("Email address").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(`Trial-${fixtures.run}-password`);
  // Clerk refuses a code typed before it has sent one.
  const codeSent = page.waitForResponse(
    (response) => response.url().includes("/prepare_verification") && response.ok(),
  );
  await page.getByRole("button", { name: "Continue", exact: true }).click();
  await expect(page.getByText("Verify your email")).toBeVisible({ timeout: 20_000 });
  await codeSent;
  await page.getByRole("textbox", { name: "Enter verification code" }).pressSequentially("424242");
  // Signed up once the trial page asks for the request.
  await expect(page.getByRole("heading", { name: "Request a trial" })).toBeVisible({
    timeout: 20_000,
  });
  const visitor = (await clerkClient().users.getUserList({ emailAddress: [email] })).data.at(0);
  expect(visitor).toBeDefined();
  visitors.push(visitor?.id ?? "");

  await requestTrial(page, company);

  // The operator approves it on the dev profile.
  const operator = await openRequest(browser, company);
  await expect(operator.item.getByText("Pending")).toBeVisible();
  await operator.item.getByRole("button", { name: "Approve" }).click();
  const dialog = operator.page.getByRole("dialog", { name: `Approve ${company}'s trial?` });
  await expect(dialog.getByRole("radio", { name: /^dev One broker/ })).toBeChecked();
  await dialog.getByRole("button", { name: "Approve" }).click();
  await expect(dialog).toBeHidden({ timeout: 30_000 });
  const decided = operator.page
    .getByRole("region", { name: "Decided" })
    .getByRole("listitem", { name: new RegExp(`^${company}, tr-`) });
  await expect(decided.getByText(/^Approved/).first()).toBeVisible();
  await expect(decided.getByRole("link", { name: /^c-/ })).toBeVisible();
  await operator.context.close();

  // The visitor's page moves on by itself, into their new organization.
  await expect(page.getByRole("main").getByText(company).first()).toBeVisible({ timeout: 30_000 });
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole("heading", { name: "Connection" })).toBeVisible({
    timeout: 4 * 60_000,
  });
});

test("a declined visitor sees the reason", async ({ page, browser }) => {
  test.setTimeout(3 * 60_000);
  const company = `E2E ${fixtures.run} U`;
  const email = testEmail(fixtures.run, "declined");
  const visitor = await clerkClient().users.createUser({
    emailAddress: [email],
    firstName: "E2E",
    lastName: "declined",
    skipPasswordRequirement: true,
  });
  visitors.push(visitor.id);
  await page.goto("/");
  await clerk.signIn({ page, emailAddress: email });
  // A visitor without an organization finds the trial from home.
  await page.goto("/");
  await page.getByRole("link", { name: "Request a trial" }).click();
  await requestTrial(page, company);

  const reason = "We start with teams in the EU for now.";
  const operator = await openRequest(browser, company);
  await operator.item.getByRole("button", { name: "Decline" }).click();
  const dialog = operator.page.getByRole("dialog", { name: `Decline ${company}'s request?` });
  await expect(dialog.getByRole("button", { name: "Decline" })).toBeDisabled();
  await dialog.getByLabel("Reason").fill(reason);
  await dialog.getByRole("button", { name: "Decline" }).click();
  await expect(dialog).toBeHidden();
  await operator.context.close();

  const main = page.getByRole("main");
  await expect(main.getByRole("heading", { name: "Your request was declined" })).toBeVisible({
    timeout: 20_000,
  });
  await expect(main.getByText(reason)).toBeVisible();
  await expect(main.getByText(/You may ask again from/)).toBeVisible();
  await expect(main.getByRole("button", { name: "Request a trial" })).toHaveCount(0);
});

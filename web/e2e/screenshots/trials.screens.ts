import path from "node:path";
import { clerk, setupClerkTestingToken } from "@clerk/testing/playwright";
import { expect, test, type Browser, type Page } from "@playwright/test";
import { clerkClient, kubectl, readFixtures, testEmail } from "../clerk/fixtures";

// Captures the screens of trial requests, on a desktop and a phone, into
// CHANGEPORT_SCREENSHOTS_DIR (just ui-screenshots): sign-up, the request
// form, a pending and a declined request, and the operator's review. Two
// visitors ask, Initech and Hooli; the operator looks at approving Initech
// and declines Hooli. Everything it creates goes again at the end.

const out = process.env.CHANGEPORT_SCREENSHOTS_DIR ?? "";
const devices = [
  ["desktop", { width: 1280, height: 900 }],
  ["phone", { width: 390, height: 844 }],
] as const;

/** As in console.screens.ts: the whole page, or the viewport with a dialog. */
async function shot(page: Page, name: string): Promise<void> {
  if ((await page.getByRole("link", { name: "Changeport" }).count()) > 0) {
    await expect(page.getByRole("button", { name: "Open user menu" })).toBeVisible({
      timeout: 20_000,
    });
  }
  // Images, such as the logos of Clerk's sign-in buttons, load late.
  await page.waitForFunction(() => {
    const { document } = globalThis as unknown as {
      document: { images: ArrayLike<{ complete: boolean }> };
    };
    return Array.from(document.images).every((image) => image.complete);
  });
  const dialog = (await page.getByRole("dialog").count()) > 0;
  const size = page.viewportSize();
  for (const [device, viewport] of devices) {
    await page.setViewportSize(viewport);
    await page.waitForTimeout(600);
    await page.screenshot({ path: path.join(out, `${name}-${device}.png`), fullPage: !dialog });
  }
  if (size) {
    await page.setViewportSize(size);
  }
}

let fixtures: ReturnType<typeof readFixtures>;
const visitors: string[] = [];

test.beforeAll(() => {
  fixtures = readFixtures();
  expect(out, "CHANGEPORT_SCREENSHOTS_DIR names where the screenshots go").not.toBe("");
});

test.afterAll(async () => {
  const clerkAPI = clerkClient();
  for (const id of visitors) {
    kubectl(["delete", "trialrequests", "-l", `changeport.io/clerk-user=${id}`, "--wait=false"]);
    await clerkAPI.users.deleteUser(id).catch(() => undefined);
  }
});

/** Creates a visitor without an organization and signs them in at path. */
async function visitor(browser: Browser, role: string, at: string): Promise<Page> {
  const user = await clerkClient().users.createUser({
    emailAddress: [testEmail(fixtures.run, role)],
    firstName: "Grace",
    lastName: "Hopper",
    skipPasswordRequirement: true,
  });
  visitors.push(user.id);
  const page = await (await browser.newContext({ viewport: devices[0][1] })).newPage();
  await page.goto("/");
  await clerk.signIn({ page, emailAddress: testEmail(fixtures.run, role) });
  await page.goto(at);
  return page;
}

/** Fills the request form in for company, without sending it. */
async function fillIn(page: Page, company: string, useCase: string): Promise<void> {
  const main = page.getByRole("main");
  await main.getByLabel("Company").fill(company);
  await main.getByLabel("Your role").fill("Head of platform");
  await main.getByLabel("What will you use Kafka for?").fill(useCase);
  await main.getByRole("radio", { name: "1 to 10 MB/s" }).check();
  await main.getByRole("radio", { name: "Up to a week" }).check();
}

test("the screens of trial requests", async ({ browser }) => {
  test.setTimeout(10 * 60_000);

  // Signed out, /trial shows Clerk's sign-up.
  const stranger = await (await browser.newContext({ viewport: devices[0][1] })).newPage();
  await setupClerkTestingToken({ page: stranger });
  await stranger.goto("/trial");
  await expect(stranger.getByLabel(/email address/i)).toBeVisible({ timeout: 20_000 });
  await shot(stranger, "32-trial-sign-up");

  // Signed in without an organization, home points to the trial.
  const initech = await visitor(browser, "initech", "/");
  await expect(initech.getByRole("heading", { name: "Choose your organization" })).toBeVisible({
    timeout: 20_000,
  });
  await shot(initech, "33-no-organization");

  // The form, its problems, and the request once sent.
  await initech.getByRole("link", { name: "Request a trial" }).click();
  const main = initech.getByRole("main");
  await expect(main.getByRole("heading", { name: "Request a trial" })).toBeVisible();
  await shot(initech, "34-trial-request");
  await main.getByRole("button", { name: "Request a trial" }).click();
  await shot(initech, "35-trial-request-problems");
  await fillIn(
    initech,
    `E2E ${fixtures.run} I`,
    "Order and payment events between our checkout, fulfilment and billing services, " +
      "replacing a queue that loses messages when a consumer restarts.",
  );
  await main.getByLabel("When you would like to start (optional)").fill("2026-12-01");
  await main.getByRole("button", { name: "Request a trial" }).click();
  await expect(main.getByRole("heading", { name: "Your request is in" })).toBeVisible();
  await shot(initech, "36-trial-pending");

  const hooli = await visitor(browser, "hooli", "/trial");
  await fillIn(hooli, `E2E ${fixtures.run} H`, "Clickstream events for our analytics team.");
  await hooli.getByRole("main").getByRole("button", { name: "Request a trial" }).click();
  await expect(
    hooli.getByRole("main").getByRole("heading", { name: "Your request is in" }),
  ).toBeVisible();

  // The operator reviews both.
  const operator = await (await browser.newContext({ viewport: devices[0][1] })).newPage();
  await operator.goto("/");
  await clerk.signIn({ page: operator, emailAddress: fixtures.users.staff.email });
  await operator.goto("/operator/trials");
  await expect(operator.getByRole("heading", { name: "Trial requests" })).toBeVisible({
    timeout: 20_000,
  });
  const item = (company: string) =>
    operator.getByRole("listitem", { name: new RegExp(`^E2E ${fixtures.run} ${company}, tr-`) });
  await expect(item("I")).toBeVisible();
  await expect(item("H")).toBeVisible();
  await shot(operator, "37-operator-trial-requests");
  await item("I").getByRole("button", { name: "Approve" }).click();
  await expect(operator.getByRole("dialog")).toBeVisible();
  await shot(operator, "38-operator-trial-approve");
  await operator.keyboard.press("Escape");
  await item("H").getByRole("button", { name: "Decline" }).click();
  await operator
    .getByRole("dialog")
    .getByLabel("Reason")
    .fill("We start with teams that run their services on AWS in Europe for now.");
  await shot(operator, "39-operator-trial-decline");
  await operator.getByRole("dialog").getByRole("button", { name: "Decline" }).click();
  await expect(operator.getByRole("dialog")).toBeHidden();
  await expect(operator.getByRole("region", { name: "Decided" })).toBeVisible();
  await shot(operator, "40-operator-trial-decided");

  // Hooli sees the reason.
  await expect(
    hooli.getByRole("main").getByRole("heading", { name: "Your request was declined" }),
  ).toBeVisible({ timeout: 20_000 });
  await shot(hooli, "41-trial-declined");
});

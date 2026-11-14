import { clerk } from "@clerk/testing/playwright";
import { expect, test, type Page } from "@playwright/test";
import { readFixtures } from "./fixtures";
import { openOverview } from "./helpers";

// The sidebar's help menu and the keyboard shortcuts (docs/console.md), as
// Linear has them: G then a page's key goes there, and Mod+/ lists them.

let fixtures: ReturnType<typeof readFixtures>;
test.beforeAll(() => {
  fixtures = readFixtures();
});

async function openHelp(page: Page) {
  await page.getByRole("button", { name: "Help" }).click();
  const menu = page.getByRole("menu");
  await expect(menu).toBeVisible();
  return menu;
}

test("the help menu leads to the guide, support and the shortcuts", async ({ page }) => {
  await openOverview(page, fixtures.users.adminA);
  let menu = await openHelp(page);
  // On kind the console names support@changeport.test (deploy/overlays/kind).
  await expect(menu.getByRole("menuitem", { name: "Contact support" })).toHaveAttribute(
    "href",
    "mailto:support@changeport.test",
  );
  await menu.getByRole("menuitem", { name: "Getting started" }).click();
  await expect(page).toHaveURL(/\/e\/production\/welcome$/);

  menu = await openHelp(page);
  await menu.getByRole("menuitem", { name: /^Keyboard shortcuts/ }).click();
  const shortcuts = page.getByRole("dialog", { name: "Keyboard shortcuts" });
  await expect(shortcuts).toContainText("Go to the team");
  await page.keyboard.press("Escape");
  await expect(shortcuts).toBeHidden();
});

test("G then a key goes to a page, except while typing", async ({ page }) => {
  await openOverview(page, fixtures.users.adminA);
  await page.keyboard.press("g");
  await page.keyboard.press("t");
  await expect(page).toHaveURL(/\/team$/);
  await expect(page.getByRole("heading", { name: "Team", level: 1 })).toBeVisible();

  // Typed into a field, the keys are the field's.
  const email = page.getByRole("main").getByLabel("Email address");
  await email.pressSequentially("go");
  await expect(email).toHaveValue("go");
  await expect(page).toHaveURL(/\/team$/);
  await email.blur();

  await page.keyboard.press("g");
  await page.keyboard.press("o");
  await expect(page).toHaveURL(/\/e\/production$/);

  await page.keyboard.press("ControlOrMeta+/");
  await expect(page.getByRole("dialog", { name: "Keyboard shortcuts" })).toBeVisible();
});

test("operators get the operator console's shortcuts, and no guide", async ({ page }) => {
  await page.goto("/");
  await clerk.signIn({ page, emailAddress: fixtures.users.staff.email });
  await page.goto("/operator");
  await expect(page.getByRole("heading", { name: "Customers", level: 1 })).toBeVisible({
    timeout: 20_000,
  });
  const menu = await openHelp(page);
  await expect(menu.getByRole("menuitem")).toHaveText([/^Keyboard shortcuts/]);
  await page.keyboard.press("Escape");
  await page.keyboard.press("g");
  await page.keyboard.press("r");
  await expect(page).toHaveURL(/\/operator\/trials$/);
});

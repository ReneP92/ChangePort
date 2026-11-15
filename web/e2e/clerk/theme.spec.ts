import { expect, test, type Page } from "@playwright/test";
import { readFixtures } from "./fixtures";
import { openOverview } from "./helpers";

// The theme (docs/console.md): light unless dark mode is switched on in the
// help menu, which the browser keeps.

let fixtures: ReturnType<typeof readFixtures>;
test.beforeAll(() => {
  fixtures = readFixtures();
});

/** The help menu's dark mode switch, the menu open. */
async function darkMode(page: Page) {
  await page.getByRole("button", { name: "Help" }).click();
  return page.getByRole("menuitemcheckbox", { name: "Dark mode" });
}

test("dark mode switches on and off, and is kept", async ({ page }) => {
  // Light by default, whatever the system prefers.
  await page.emulateMedia({ colorScheme: "dark" });
  await openOverview(page, fixtures.users.memberA);
  const html = page.locator("html");
  await expect(html).not.toHaveClass(/\bdark\b/);

  // Switched on, the menu stays open to show it.
  const toggle = await darkMode(page);
  await expect(toggle).toHaveAttribute("aria-checked", "false");
  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-checked", "true");
  await expect(html).toHaveClass(/\bdark\b/);
  await page.keyboard.press("Escape");

  // Kept, and dark from before the page draws: already dark when its body
  // first appears, never a white flash. This runs in the page, before its
  // own scripts.
  await page.addInitScript({
    content: `new MutationObserver((_, observer) => {
      if (document.body) {
        window.darkAtBody = document.documentElement.classList.contains("dark");
        observer.disconnect();
      }
    }).observe(document, { childList: true, subtree: true });`,
  });
  await page.reload();
  await expect(page.getByRole("heading", { name: "Connection" })).toBeVisible();
  await expect(html).toHaveClass(/\bdark\b/);
  expect(
    await page.evaluate(() => (globalThis as unknown as { darkAtBody?: boolean }).darkAtBody),
  ).toBe(true);

  // Switched off again.
  await (await darkMode(page)).click();
  await expect(html).not.toHaveClass(/\bdark\b/);
});

test("another tab follows the switch, at phone width with its menu closed", async ({ page }) => {
  await openOverview(page, fixtures.users.memberA);
  const phone = await page.context().newPage();
  try {
    await phone.setViewportSize({ width: 390, height: 844 });
    await phone.goto("/");
    await expect(phone.getByRole("heading", { name: "Connection" })).toBeVisible();

    // Switched from the keyboard, the menu staying open.
    const toggle = await darkMode(page);
    await toggle.focus();
    await page.keyboard.press("Enter");
    await expect(toggle).toHaveAttribute("aria-checked", "true");
    await page.keyboard.press("Space");
    await expect(toggle).toHaveAttribute("aria-checked", "false");
    await page.keyboard.press("Enter");
    await expect(toggle).toHaveAttribute("aria-checked", "true");

    // The other tab is dark, and its switch says so.
    await expect(phone.locator("html")).toHaveClass(/\bdark\b/);
    await phone.getByRole("button", { name: "Open the menu" }).click();
    const phoneToggle = await darkMode(phone);
    await expect(phoneToggle).toHaveAttribute("aria-checked", "true");

    // Switched off there, and here too.
    await phoneToggle.click();
    await expect(page.locator("html")).not.toHaveClass(/\bdark\b/);
    await expect(toggle).toHaveAttribute("aria-checked", "false");
  } finally {
    await phone.close();
  }
});

test("the sign-in page is dark too", async ({ browser }) => {
  const context = await browser.newContext();
  try {
    const page = await context.newPage();
    await page.addInitScript({ content: `localStorage.setItem("changeport-theme", "dark");` });
    await page.goto("/");
    await expect(page.getByLabel(/email address/i)).toBeVisible({ timeout: 20_000 });
    await expect(page.locator("html")).toHaveClass(/\bdark\b/);
  } finally {
    await context.close();
  }
});

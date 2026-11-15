import { expect, test, type Page } from "@playwright/test";
import { readFixtures } from "./fixtures";
import { openOverview } from "./helpers";

// The theme (docs/console.md): light, dark, or the system's, chosen in the
// help menu and kept by the browser.

let fixtures: ReturnType<typeof readFixtures>;
test.beforeAll(() => {
  fixtures = readFixtures();
});

async function choose(page: Page, theme: "System" | "Light" | "Dark") {
  await page.getByRole("button", { name: "Help" }).click();
  await page.getByRole("menuitemradio", { name: theme }).click();
}

test("the theme follows the system unless chosen, and is kept", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "light" });
  await openOverview(page, fixtures.users.memberA);
  const html = page.locator("html");
  await expect(html).not.toHaveClass(/\bdark\b/);
  // System is the default, and follows the system as it changes.
  await page.getByRole("button", { name: "Help" }).click();
  await expect(page.getByRole("menuitemradio", { name: "System" })).toHaveAttribute(
    "aria-checked",
    "true",
  );
  await page.keyboard.press("Escape");
  await page.emulateMedia({ colorScheme: "dark" });
  await expect(html).toHaveClass(/\bdark\b/);

  // Light, chosen, holds against a dark system, and is kept on reload.
  await choose(page, "Light");
  await expect(html).not.toHaveClass(/\bdark\b/);
  await page.reload();
  await expect(page.getByRole("heading", { name: "Connection" })).toBeVisible();
  await expect(html).not.toHaveClass(/\bdark\b/);

  // Dark, likewise against a light one, and from before the page draws:
  // dark already when its body first appears, never a white flash.
  await page.emulateMedia({ colorScheme: "light" });
  await choose(page, "Dark");
  await expect(html).toHaveClass(/\bdark\b/);
  // Runs in the page, before its own scripts: notes whether the root is
  // dark when the body first appears.
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
  expect(
    await page.evaluate(() => (globalThis as unknown as { darkAtBody?: boolean }).darkAtBody),
  ).toBe(true);

  await choose(page, "System");
  await expect(html).not.toHaveClass(/\bdark\b/);
});

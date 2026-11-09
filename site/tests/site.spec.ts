import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

const wide = (page: Page) => (page.viewportSize()?.width ?? 0) >= 768;

test("the hero's diagram draws white nodes on thin lines", async ({ page }) => {
  await page.goto("/");
  const diagram = page.locator("svg[data-flow]:visible");
  await expect(diagram).toHaveAttribute("role", "img");
  const styles = await diagram.evaluate((svg) => {
    const node = svg.querySelector("rect.node");
    const line = svg.querySelector("use.line");
    return node && line
      ? { node: getComputedStyle(node).fill, line: getComputedStyle(line).fill }
      : null;
  });
  expect(styles).toEqual({ node: "rgb(255, 255, 255)", line: "none" });
});

test("the hero fills the first screen, so the next section starts at the fold", async ({
  page,
}) => {
  test.skip((page.viewportSize()?.width ?? 0) < 1024, "Phones scroll on through the hero");
  for (const height of [800, 900, 1000]) {
    await page.setViewportSize({ width: page.viewportSize()?.width ?? 0, height });
    await page.goto("/");
    // Measured in the site's own font, which lays the hero out.
    await page.evaluate(() => document.fonts.ready);
    const bottom = await page
      .locator("main > section")
      .first()
      .evaluate((hero) => hero.getBoundingClientRect().bottom);
    expect(Math.round(bottom)).toBe(height);
  }
});

test("the lag simulator alerts past a minute behind, and resolves", async ({ page }) => {
  await page.goto("/#lag");
  const simulator = page.locator("[data-lag]");
  const alert = simulator.locator("[data-lag-alert]");
  await expect(alert).toHaveText(/more than 1 minute behind/);

  await simulator.getByLabel("How fast search-indexer reads").fill("50");
  await expect(simulator.locator("[data-lag-speed]")).toHaveText("50% of writes");
  await expect(simulator.locator("[data-lag-status]")).toHaveText("Falling behind");
  // Half the write rate, at 20 times real time, is a minute behind in 6 s.
  await expect(alert).toContainText("Consumer group search-indexer is", { timeout: 15_000 });
  await expect(alert).toContainText(/is \dm \d+s behind on orders/);
  await expect(simulator.locator("[data-lag-time]")).toHaveText(/^\d+ min \d+ s$/);
  await expect(simulator.locator("[data-lag-messages]")).toHaveText(/^\d{2},\d{3}$/);

  await simulator.getByLabel("How fast search-indexer reads").fill("150");
  await expect(simulator.locator("[data-lag-status]")).toHaveText("Catching up");
  await expect(alert).toHaveText(/Resolved/, { timeout: 15_000 });
  await expect(simulator.locator("[data-lag-status]")).toHaveText("Caught up", { timeout: 15_000 });
  await expect(simulator.locator("[data-lag-messages]")).toHaveText("0");
});

test("each change in the table lands in the topic, and nothing moves", async ({ page }) => {
  await page.goto("/#how");
  const demo = page.locator("[data-cdc]");
  const newest = demo.locator("[data-cdc-messages] > li").first();
  await expect(newest).toContainText("41");
  const height = (await demo.boundingBox())?.height;
  // The next change lands as the newest message, at the next offset.
  await expect(newest).toContainText("42", { timeout: 8_000 });
  await expect(newest).toContainText(/^(insert|update) 104\d /);
  await expect(demo.locator("[data-cdc-messages] > li")).toHaveCount(4);
  await expect(demo.locator("[data-cdc-rows] > div")).toHaveCount(4);
  expect((await demo.boundingBox())?.height).toBe(height);
});

test("the console tour switches screens by click and arrow keys", async ({ page }) => {
  await page.goto("/#tour");
  const tour = page.locator("[data-tour]");
  const tab = (name: string) => tour.getByRole("tab", { name });
  await expect(tab("Overview")).toHaveAttribute("aria-selected", "true");
  await expect(page.locator("#panel-overview")).toBeVisible();
  await expect(page.locator("#panel-topology")).toBeHidden();

  await tab("Data flow").click();
  await expect(tab("Data flow")).toHaveAttribute("aria-selected", "true");
  await expect(page.locator("#panel-topology")).toBeVisible();
  await expect(page.locator("#panel-overview")).toBeHidden();

  await page.keyboard.press("ArrowLeft");
  await expect(tab("What changed")).toBeFocused();
  await expect(page.locator("#panel-timeline")).toBeVisible();
  await page.keyboard.press("Home");
  await expect(tab("Overview")).toBeFocused();
  await page.keyboard.press("End");
  await expect(tab("Data flow")).toHaveAttribute("aria-selected", "true");
});

test("the console tour moves on by itself on wide screens", async ({ page }) => {
  test.skip(!wide(page), "Phones show one screen until someone picks another");
  await page.goto("/");
  await page.locator("[data-tour]").scrollIntoViewIfNeeded();
  const lag = page.locator("[data-tour]").getByRole("tab", { name: "Lag in time" });
  await expect(lag).toHaveAttribute("aria-selected", "true", { timeout: 12_000 });
});

test("the questions open and close", async ({ page }) => {
  await page.goto("/#faq");
  const question = page.locator(".faq details").filter({ hasText: "What runs under the hood?" });
  await question.locator("summary").click();
  await expect(question.getByText("There is no Changeport SDK")).toBeVisible();
  await question.locator("summary").click();
  await expect(question.getByText("There is no Changeport SDK")).toBeHidden();
});

test("phones get a menu, and nothing scrolls sideways", async ({ page }) => {
  await page.goto("/");
  const width = await page.evaluate(() => document.documentElement.scrollWidth);
  expect(width).toBeLessThanOrEqual(page.viewportSize()?.width ?? 0);
  test.skip(wide(page), "Wide screens show the navigation itself");
  await page.locator("summary", { hasText: "Menu" }).click();
  await page
    .getByRole("navigation", { name: "Site" })
    .getByRole("link", { name: "Questions" })
    .click();
  await expect(page).toHaveURL(/#faq$/);
  // The menu closes, so that it does not cover where it led.
  await expect(page.getByRole("navigation", { name: "Site" })).toBeHidden();
});

test.describe("with reduced motion", () => {
  test.use({ reducedMotion: "reduce" });

  test("the hero keeps still, and every section shows", async ({ page }) => {
    await page.goto("/");
    const paused = await page
      .locator("svg[data-flow]")
      .evaluateAll((svgs) => svgs.every((svg) => (svg as SVGSVGElement).animationsPaused()));
    expect(paused).toBe(true);
    await page.locator("[data-cdc]").scrollIntoViewIfNeeded();
    await page.waitForTimeout(3_500);
    await expect(page.locator("[data-cdc-messages] > li").first()).toContainText("41");
    await page.locator("#faq").scrollIntoViewIfNeeded();
    await expect(page.getByRole("heading", { name: "Good to know" })).toHaveCSS("opacity", "1");
  });
});

test.describe("every page, fully shown", () => {
  // With reduced motion nothing waits to rise into view, so that axe sees
  // every element, which Lighthouse's audit of the first screen does not.
  test.use({ reducedMotion: "reduce" });

  for (const path of ["/", "/privacy.html", "/terms.html"]) {
    test(`${path} has no accessibility violations`, async ({ page }) => {
      await page.goto(path);
      const results = await new AxeBuilder({ page }).analyze();
      expect(
        results.violations.map(
          (v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`,
        ),
      ).toEqual([]);
    });
  }
});

test.describe("without JavaScript", () => {
  test.use({ javaScriptEnabled: false });

  test("every section and every console screen shows", async ({ page }) => {
    await page.goto("/");
    for (const heading of ["Know how far behind every reader is. In minutes.", "Good to know"]) {
      await expect(page.getByRole("heading", { name: heading })).toHaveCSS("opacity", "1");
    }
    for (const panel of ["overview", "lag", "timeline", "topology"]) {
      await expect(page.locator(`#panel-${panel}`)).toBeVisible();
    }
    const boxes = await Promise.all(
      ["overview", "lag"].map((panel) => page.locator(`#panel-${panel}`).boundingBox()),
    );
    // Stacked, not on top of each other.
    expect((boxes[1]?.y ?? 0) >= (boxes[0]?.y ?? 0) + (boxes[0]?.height ?? 0)).toBe(true);
  });
});

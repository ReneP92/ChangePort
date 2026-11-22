import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";

const wide = (page: Page) => (page.viewportSize()?.width ?? 0) >= 768;

/** The docs' pages the tests visit: each kind of page once. */
const docs = [
  "/docs/",
  "/docs/connecting.html",
  "/docs/topics.html",
  "/docs/troubleshooting/",
  "/docs/troubleshooting/consumer-too-slow.html",
];

/** Every page of the site; 404.html is what the host serves for an unknown address. */
const pages = ["/", "/privacy.html", "/terms.html", "/404.html", ...docs];

// Each page's Content-Security-Policy (vite.config.ts) lets its own scripts
// and styles run, and nothing it does is refused.
for (const path of pages) {
  test(`${path} runs under its Content-Security-Policy`, async ({ page }) => {
    await page.addInitScript(() => {
      const window = globalThis as unknown as {
        violations: string[];
        addEventListener(type: string, listener: (event: Event) => void): void;
      };
      window.violations = [];
      window.addEventListener("securitypolicyviolation", (event) => {
        const violation = event as unknown as { violatedDirective: string; blockedURI: string };
        window.violations.push(`${violation.violatedDirective} ${violation.blockedURI}`);
      });
    });
    await page.goto(path);
    await page.waitForLoadState("networkidle");
    const policy = page.locator('meta[http-equiv="Content-Security-Policy"]');
    await expect(policy).toHaveAttribute("content", /script-src 'self' 'sha256-/);
    // The inline script ran: it marks the page for the animations.
    await expect(page.locator("html")).toHaveClass(/\bjs\b/);
    expect(
      await page.evaluate(() => (globalThis as unknown as { violations: string[] }).violations),
    ).toEqual([]);
  });
}

test("the 404 page leads home, and keeps out of search engines", async ({ page }) => {
  await page.goto("/404.html");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("There is no such page");
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute("content", "noindex");
  await page.getByRole("main").getByRole("link", { name: "Go to the home page" }).click();
  await expect(page).toHaveURL(/\/$/);
});

test("the hero's model draws white blocks, with changes moving between them", async ({ page }) => {
  await page.goto("/");
  test.skip((page.viewportSize()?.width ?? 0) < 640, "Phones get the streams as a column");
  const diagram = page.locator("svg[data-flow]:visible");
  await expect(diagram).toHaveAttribute("role", "img");
  const drawn = await diagram.evaluate((svg) => {
    const node = svg.querySelector("rect.node");
    return node
      ? {
          node: getComputedStyle(node).fill,
          moving: svg.querySelectorAll("animateMotion").length > 0,
        }
      : null;
  });
  expect(drawn).toEqual({ node: "rgb(255, 255, 255)", moving: true });
});

test("phones get the hero's streams as a column, with every reader", async ({ page }) => {
  await page.goto("/");
  test.skip((page.viewportSize()?.width ?? 0) >= 640, "Wider screens get the model");
  const column = page.locator("[data-scene] [role=img]:visible");
  await expect(column).toHaveAccessibleName(/search-indexer, which is 3 minutes behind\.$/);
  await expect(column.locator(".stream-label")).toHaveText([
    "Postgres: shop",
    "checkout-api",
    "payments-api",
    "billing",
    "fraud-checks",
    "search-indexer 3 min behind",
    "warehouse-sync",
  ]);
});

test("the hero tells its streams in plain words, and back", async ({ page }) => {
  await page.goto("/");
  const figure = page.getByRole("figure", { name: "Your streams, right now", exact: true });
  const flow = figure.getByRole("button", { name: "Flow" });
  const words = figure.getByRole("button", { name: "In plain words" });
  await expect(page.locator("[data-clock]")).toHaveText(/^\d\d:\d\d:\d\d$/);
  await expect(flow).toHaveAttribute("aria-pressed", "true");
  await expect(figure.locator("[data-plain]")).toBeHidden();

  await words.click();
  await expect(words).toHaveAttribute("aria-pressed", "true");
  await expect(flow).toHaveAttribute("aria-pressed", "false");
  await expect(figure.locator("[data-plain]")).toBeVisible();
  await expect(figure.locator("[data-scene]")).toBeHidden();
  await expect(figure.locator("[data-scene]")).toHaveJSProperty("inert", true);
  const results = await new AxeBuilder({ page }).include("[data-streams]").analyze();
  expect(results.violations.map((v) => v.id)).toEqual([]);

  await flow.focus();
  await page.keyboard.press("Enter");
  await expect(flow).toHaveAttribute("aria-pressed", "true");
  await expect(figure.locator("[data-scene]")).toBeVisible();
  await expect(figure.locator("[data-plain]")).toHaveJSProperty("inert", true);
});

test("the hero's plain words never leave its streams with a gap below them", async ({ page }) => {
  await page.goto("/");
  // The two views share one place, as tall as the taller of them.
  for (const width of [320, 360, 412, 480, 640, 1024, 1280]) {
    await page.setViewportSize({ width, height: 900 });
    const [scene, plain] = await page.evaluate(() => [
      Math.max(
        ...[...document.querySelectorAll("[data-scene] > *")].map(
          (view) => view.getBoundingClientRect().height,
        ),
      ),
      document.querySelector("[data-plain]")!.getBoundingClientRect().height,
    ]);
    expect(plain, `at ${width}px`).toBeLessThanOrEqual(scene + 1.5);
  }
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

test("the docs lead from page to page, and nothing scrolls sideways", async ({ page }) => {
  await page.goto("/");
  if (wide(page)) {
    await page
      .getByRole("navigation", { name: "Site" })
      .getByRole("link", { name: "Docs" })
      .click();
  } else {
    await page.locator("summary", { hasText: "Menu" }).click();
    await page
      .getByRole("navigation", { name: "Site" })
      .getByRole("link", { name: "Docs" })
      .click();
  }
  await expect(page).toHaveURL(/\/docs\/$/);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Changeport docs");
  await expect(page).toHaveTitle("Changeport docs");

  // The navigation is beside the page on wide screens, and behind a menu on
  // phones; either way it marks the page shown.
  if (!wide(page)) {
    await page.locator("summary", { hasText: "All docs" }).click();
  }
  const navigation = page.getByRole("navigation", { name: "Docs" }).locator("visible=true");
  await expect(navigation.getByRole("link", { name: "Changeport docs" })).toHaveAttribute(
    "aria-current",
    "page",
  );
  // The troubleshooting pages show only among themselves.
  await expect(navigation.getByRole("link", { name: /can't keep up/ })).toHaveCount(0);
  await navigation.getByRole("link", { name: "Topics, partitions and keys" }).click();
  await expect(page).toHaveURL(/\/docs\/topics\.html$/);
  await expect(page).toHaveTitle("Topics, partitions and keys: Changeport docs");

  // A link to a section of another page lands on that section.
  await page
    .getByRole("main")
    .getByRole("link", { name: "one partition gets most of the messages" })
    .click();
  await expect(page).toHaveURL(/\/docs\/troubleshooting\/hot-partition\.html$/);
  await page.getByRole("link", { name: /^Next/ }).click();
  await expect(page.getByRole("heading", { level: 1 })).toContainText("more partitions");

  for (const path of docs) {
    await page.goto(path);
    const width = await page.evaluate(() => document.documentElement.scrollWidth);
    expect(width, path).toBeLessThanOrEqual(page.viewportSize()?.width ?? 0);
  }
});

test.describe("with reduced motion", () => {
  test.use({ reducedMotion: "reduce" });

  test("the hero keeps still, and every section shows", async ({ page }) => {
    await page.goto("/");
    const paused = await page
      .locator("svg[data-flow]")
      .evaluateAll((svgs) => svgs.every((svg) => (svg as SVGSVGElement).animationsPaused()));
    expect(paused).toBe(true);
    const running = await page
      .locator(".stream-link")
      .evaluateAll((links) =>
        links.filter((link) => getComputedStyle(link, "::after").animationName !== "none"),
      );
    expect(running).toHaveLength(0);
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

  for (const path of pages) {
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
    // Stacked, not on top of each other: the next panel starts where the
    // first ends. Both are measured at once, as the page above them may
    // still move by a pixel while its fonts load, and the edges meet at
    // fractions of a pixel.
    const [overview, lag] = await page.evaluate(() =>
      ["overview", "lag"].map((panel) =>
        document.getElementById(`panel-${panel}`)!.getBoundingClientRect().toJSON(),
      ),
    );
    expect(lag.top).toBeGreaterThanOrEqual(overview.bottom - 1);
  });

  test("the hero shows its streams, and no toggle or clock that need the script", async ({
    page,
  }) => {
    await page.goto("/");
    await expect(page.locator("[data-scene]")).toBeVisible();
    await expect(page.locator("[data-view-toggle]")).toBeHidden();
    await expect(page.locator("[data-clock]")).toBeHidden();
  });
});

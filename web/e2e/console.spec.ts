import { expect, test, type Page, type Request } from "@playwright/test";

// Against the preview build there is no console behind /api, so these tests
// answer /api/config themselves; against a real console
// (PLAYWRIGHT_BASE_URL) they check what it really serves.
const realConsole = Boolean(process.env.PLAYWRIGHT_BASE_URL);

/**
 * Waits for the page's requests to settle: half a second with none open, as
 * networkidle does, but for Clerk's bot protection, which probes its hosts on
 * ports a network may never answer, so that networkidle may never come. A
 * policy refuses a request as it starts, so its violation shows all the same.
 */
function requests(page: Page): { settled: () => Promise<void> } {
  const open = new Set<Request>();
  let changed = Date.now();
  const probe = (url: string) => new URL(url).hostname.endsWith(".protect.clerk.com");
  // A probe starting counts as activity, but never as open.
  page.on("request", (r) => {
    changed = Date.now();
    if (!probe(r.url())) {
      open.add(r);
    }
  });
  const done = (r: Request) => {
    if (open.delete(r)) {
      changed = Date.now();
    }
  };
  page.on("requestfinished", done);
  page.on("requestfailed", done);
  return {
    settled: () =>
      expect
        .poll(() => open.size === 0 && Date.now() - changed >= 500, {
          intervals: [100],
          timeout: 15_000,
        })
        .toBe(true),
  };
}

/** Collects the Content-Security-Policy violations the page reports. */
function cspViolations(page: Page): string[] {
  const violations: string[] = [];
  page.on("console", (message) => {
    if (message.text().includes("Content Security Policy")) {
      violations.push(message.text());
    }
  });
  return violations;
}

test("an unconfigured console says so instead of offering to sign in", async ({
  page,
  request,
}) => {
  if (realConsole) {
    const config = await request.get("/api/config");
    test.skip(config.status() !== 503, "this console is configured");
  } else {
    await page.route("/api/config", (route) =>
      route.fulfill({
        status: 503,
        contentType: "application/problem+json",
        body: JSON.stringify({ status: 503, title: "The console is not configured" }),
      }),
    );
  }
  await page.goto("/");
  await expect(page).toHaveTitle("Changeport");
  await expect(page.getByRole("heading", { name: "The console is not configured" })).toBeVisible();
});

test("a configured console offers Clerk's sign-in", async ({ page, request }) => {
  test.skip(!realConsole, "needs a real console and Clerk instance");
  const config = await request.get("/api/config");
  test.skip(config.status() !== 200, "this console is not configured");
  const violations = cspViolations(page);
  const network = requests(page);
  // Clerk's components are bundled with the app (@clerk/ui), not loaded from
  // Clerk's CDN, where one hung request once left the form blank.
  const cdnComponents: string[] = [];
  page.on("request", (request) => {
    if (request.url().includes("/npm/@clerk/ui")) {
      cdnComponents.push(request.url());
    }
  });
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Changeport", exact: true })).toBeVisible();
  // Clerk's sign-in form, talking to the instance's Frontend API.
  await expect(page.getByLabel(/email address/i)).toBeVisible({ timeout: 20_000 });
  // Clerk's bot protection reaches out after the form shows.
  await network.settled();
  await page.getByLabel(/email address/i).fill("someone@example.com");
  await network.settled();
  expect(violations).toEqual([]);
  expect(cdnComponents).toEqual([]);
});

test("a console that cannot reach Clerk says so", async ({ page, request }) => {
  test.skip(!realConsole, "needs a real console and Clerk instance");
  const config = await request.get("/api/config");
  test.skip(config.status() !== 200, "this console is not configured");
  // Everything but the console itself fails, as on a broken connection.
  await page.route(
    (url) => url.hostname !== "localhost",
    (route) => route.abort("internetdisconnected"),
  );
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Sign-in could not load" })).toBeVisible({
    timeout: 30_000,
  });
});

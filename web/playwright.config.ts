import { defineConfig, devices, type Project } from "@playwright/test";

// End-to-end tests. They run against the built app served by vite preview;
// PLAYWRIGHT_BASE_URL points them at a running console instead, such as the
// one on kind. With a Clerk secret key too (just ui-e2e), the signed-in tests
// run as well: a setup project creates this run's Clerk test users,
// organizations and customers, and a teardown project removes them.
const baseURL = process.env.PLAYWRIGHT_BASE_URL ?? "http://localhost:4173";
const ci = Boolean(process.env.CI);
const signedIn = Boolean(process.env.PLAYWRIGHT_BASE_URL && process.env.CLERK_SECRET_KEY);
const chrome = { ...devices["Desktop Chrome"] };

// With CHANGEPORT_SCREENSHOTS_DIR (just ui-screenshots), the signed-in setup
// serves a tour of every screen instead of the tests.
const screenshots = Boolean(process.env.CHANGEPORT_SCREENSHOTS_DIR);

const projects: Project[] = [];
if (!screenshots) {
  projects.push({ name: "console", testMatch: "console.spec.ts", use: chrome });
}
if (signedIn) {
  projects.push(
    { name: "clerk setup", testMatch: "clerk/clerk.setup.ts", teardown: "clerk teardown" },
    { name: "clerk teardown", testMatch: "clerk/clerk.teardown.ts" },
    screenshots
      ? {
          name: "screenshots",
          testMatch: "screenshots/*.screens.ts",
          dependencies: ["clerk setup"],
          use: chrome,
        }
      : {
          name: "signed in",
          testMatch: "clerk/*.spec.ts",
          dependencies: ["clerk setup"],
          use: chrome,
        },
  );
}

export default defineConfig({
  testDir: "./e2e",
  forbidOnly: ci,
  // Flaky tests get fixed, not retried.
  retries: 0,
  // The signed-in tests share one kind cluster, and each starts Kafka
  // clients of its own, a JVM apiece, next to the customers' clusters they
  // provision: at half of a 12-core machine, 6 at once, the cluster ran out
  // of CPU (CPU pressure over 80% in the Docker VM), and its API and
  // provisioning slowed past the tests' timeouts. CI's runners run 2 per
  // shard, Playwright's default for their cores.
  workers: ci ? undefined : 3,
  reporter: ci ? [["list"], ["html", { open: "never" }]] : "list",
  use: { baseURL, trace: "retain-on-failure" },
  projects,
  webServer: process.env.PLAYWRIGHT_BASE_URL
    ? undefined
    : {
        // The Bun running this config, so no bun has to be on PATH.
        command: `${process.execPath} run build && ${process.execPath} run preview --port 4173 --strictPort`,
        url: baseURL,
        reuseExistingServer: !ci,
      },
});

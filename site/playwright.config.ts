import { defineConfig, devices } from "@playwright/test";

// The site's interactive parts, tested against the production build (dist/,
// from bun run build) as vite preview serves it.
const ci = Boolean(process.env.CI);

export default defineConfig({
  testDir: "./tests",
  forbidOnly: ci,
  // Flaky tests get fixed, not retried.
  retries: 0,
  reporter: ci ? [["list"], ["html", { open: "never" }]] : "list",
  use: { baseURL: "http://localhost:4176", trace: "retain-on-failure" },
  projects: [
    {
      name: "desktop",
      use: { ...devices["Desktop Chrome"], viewport: { width: 1440, height: 900 } },
    },
    { name: "phone", use: { ...devices["Pixel 7"] } },
  ],
  webServer: {
    // The Bun running this config, so no bun has to be on PATH.
    command: `${process.execPath} x vite preview --port 4176 --strictPort`,
    url: "http://localhost:4176",
    reuseExistingServer: !ci,
  },
});

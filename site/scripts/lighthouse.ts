// Audits the built site (dist/, from bun run build) with Lighthouse, on a
// throttled phone and on a desktop, and fails unless every page scores at
// least 95 for performance, accessibility, best practices and SEO. Chromium
// is Playwright's, which the web app's tests install too.
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { launch } from "chrome-launcher";
import lighthouse, { desktopConfig, type Config } from "lighthouse";
import { chromium } from "playwright-core";
import { preview } from "vite";

const pages = ["/", "/privacy.html", "/terms.html"];
const categories = ["performance", "accessibility", "best-practices", "seo"];
const minimum = 95;
const reports = path.join(import.meta.dirname, "..", "lighthouse");

const server = await preview({ preview: { port: 4175, strictPort: true }, logLevel: "warn" });
const chrome = await launch({
  chromePath: chromium.executablePath(),
  chromeFlags: ["--headless=new", "--no-sandbox"],
});
mkdirSync(reports, { recursive: true });

const failures: string[] = [];
try {
  for (const [formFactor, config] of [
    ["phone", undefined],
    ["desktop", desktopConfig],
  ] as [string, Config | undefined][]) {
    for (const page of pages) {
      const result = await lighthouse(
        `http://localhost:4175${page}`,
        { port: chrome.port, onlyCategories: categories, output: "html", logLevel: "error" },
        config,
      );
      if (!result) {
        throw new Error(`Lighthouse returned nothing for ${page}`);
      }
      const name = `${formFactor}${page === "/" ? "-index" : `-${path.basename(page, ".html")}`}`;
      writeFileSync(path.join(reports, `${name}.html`), result.report as string);
      const scores = categories.map((id) => {
        const category = result.lhr.categories[id];
        const score = Math.round((category?.score ?? 0) * 100);
        if (score < minimum) {
          // The audits that count towards the score, and pulled it down.
          const audits = (category?.auditRefs ?? [])
            .filter((ref) => ref.weight > 0)
            .map((ref) => result.lhr.audits[ref.id])
            .filter((audit) => audit?.score != null && audit.score < 1)
            .map(
              (audit) => `${audit?.id ?? ""} (${String(Math.round((audit?.score ?? 0) * 100))})`,
            );
          failures.push(
            `${formFactor} ${page}: ${id} ${String(score)}; weak audits: ${audits.join(", ")}`,
          );
        }
        return `${id} ${String(score)}`;
      });
      console.log(`${formFactor.padEnd(7)} ${page.padEnd(14)} ${scores.join("  ")}`);
    }
  }
} finally {
  chrome.kill();
  await server.close();
}

if (failures.length > 0) {
  console.error(`\nBelow ${String(minimum)}, see lighthouse/*.html:\n${failures.join("\n")}`);
  process.exit(1);
}

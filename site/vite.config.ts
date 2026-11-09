import { readFileSync } from "node:fs";
import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig, type Plugin } from "vite";

// The landing site at the domain's root (docs/site.md): plain HTML pages
// with Tailwind's CSS, one small script and no cookies. The console's address,
// where the call to action leads, and the site's own, for its canonical
// links, come from the environment; the publishing workflow (#138) sets
// them for the domain.
const consoleURL = (process.env.CHANGEPORT_CONSOLE_URL ?? "http://localhost:8088").replace(
  /\/$/,
  "",
);
const siteURL = (process.env.CHANGEPORT_SITE_URL ?? "http://localhost:4174").replace(/\/$/, "");

/**
 * Puts the shared partials into each page, where it says
 * <!-- include:header -->, and fills in %CONSOLE_URL%, %SITE_URL% and
 * %YEAR%.
 */
function pages(): Plugin {
  const partial = (name: string) =>
    readFileSync(path.join(import.meta.dirname, "src", "partials", `${name}.html`), "utf8");
  return {
    name: "changeport-pages",
    transformIndexHtml: {
      order: "pre",
      handler: (html) =>
        html
          .replace(/<!-- include:(\w+) -->/g, (_, name: string) => partial(name))
          .replaceAll("%CONSOLE_URL%", consoleURL)
          .replaceAll("%SITE_URL%", siteURL)
          .replaceAll("%YEAR%", String(new Date().getFullYear())),
    },
  };
}

/**
 * Puts the built stylesheet into each page as a <style>, rather than a file
 * the browser must fetch before it can draw anything or find the font: the
 * site's CSS is small, and a visitor seldom sees a second page.
 */
function inlineStyles(): Plugin {
  return {
    name: "changeport-inline-styles",
    apply: "build",
    transformIndexHtml: {
      order: "post",
      handler: (html, { bundle }) =>
        html.replace(
          /<link rel="stylesheet"[^>]* href="\/(assets\/[^"]+\.css)">/g,
          (link, file: string) => {
            const asset = bundle?.[file];
            if (asset?.type !== "asset") {
              throw new Error(`no built stylesheet for ${link}`);
            }
            return `<style>${String(asset.source)}</style>`;
          },
        ),
    },
    generateBundle: {
      order: "post",
      handler(_, bundle) {
        for (const file of Object.keys(bundle).filter((file) => file.endsWith(".css"))) {
          delete bundle[file];
        }
      },
    },
  };
}

export default defineConfig({
  plugins: [pages(), tailwindcss(), inlineStyles()],
  build: {
    rolldownOptions: {
      input: { index: "index.html", privacy: "privacy.html", terms: "terms.html" },
    },
  },
  server: { port: 5174, strictPort: true },
});

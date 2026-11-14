import { createHash } from "node:crypto";
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

/**
 * Gives each built page a Content-Security-Policy: only the site's own
 * files, and of inline scripts only those the page was built with, by their
 * hashes. Styles may be inline, as the stylesheet and a few style attributes
 * are; the site has no content of anyone else's to inject them. CloudFront
 * adds what a page cannot say for itself, such as frame-ancestors
 * (terraform/modules/static-site).
 */
function contentSecurityPolicy(): Plugin {
  return {
    name: "changeport-content-security-policy",
    apply: "build",
    transformIndexHtml: {
      order: "post",
      handler: (html) => {
        const hashes = [...html.matchAll(/<script(?![^>]*\ssrc=)[^>]*>([\s\S]*?)<\/script>/g)].map(
          ([, script]) =>
            `'sha256-${createHash("sha256")
              .update(script ?? "")
              .digest("base64")}'`,
        );
        const policy = [
          "default-src 'self'",
          `script-src 'self' ${hashes.join(" ")}`,
          "style-src 'self' 'unsafe-inline'",
          "img-src 'self' data:",
          "font-src 'self' data:",
          "object-src 'none'",
          "base-uri 'none'",
          "form-action 'none'",
        ].join("; ");
        const meta = `<meta http-equiv="Content-Security-Policy" content="${policy}" />`;
        if (!html.includes('<meta charset="utf-8" />')) {
          throw new Error("a page without <meta charset> gets no Content-Security-Policy");
        }
        // Before any script, which it would not cover otherwise.
        return html.replace('<meta charset="utf-8" />', `<meta charset="utf-8" />\n    ${meta}`);
      },
    },
  };
}

export default defineConfig({
  plugins: [pages(), tailwindcss(), inlineStyles(), contentSecurityPolicy()],
  build: {
    rolldownOptions: {
      // 404.html is what the host serves for an address it has no page for.
      input: {
        index: "index.html",
        privacy: "privacy.html",
        terms: "terms.html",
        notFound: "404.html",
      },
    },
  },
  server: {
    // IPv4, so that http://127.0.0.1:5174 works as well as localhost, which
    // browsers fall back to IPv4 for.
    host: "127.0.0.1",
    port: 5174,
    strictPort: true,
    // The checks' reports are written here; they are not the site.
    watch: { ignored: ["**/lighthouse/**", "**/test-results/**", "**/playwright-report/**"] },
  },
});

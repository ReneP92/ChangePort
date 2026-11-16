// Renders the customer docs, content/docs/**/*.md, into the site's pages at
// docs/**/*.html, which Vite then builds like the site's other pages
// (docs/site.md). The pages are generated rather than committed, so that the
// Markdown is the one copy. The build fails on a page without a title or a
// first paragraph, a page missing from the docs' navigation, a link to a
// page or a section that does not exist, and code where it is not tested.
import { mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { Marked, type Tokens } from "marked";
import { createHighlighter } from "shiki";

const root = path.join(import.meta.dirname, "..");
export const content = path.join(root, "content", "docs");
const out = path.join(root, "docs");

/**
 * The docs' navigation, in order: every page is in it once. Paths are
 * relative to content/docs, without .md.
 */
export const navigation: { title: string; pages: string[] }[] = [
  { title: "Start here", pages: ["index", "your-cluster", "connecting"] },
  {
    title: "Using Kafka",
    pages: ["topics", "consumer-groups", "delivery-guarantees", "postgres"],
  },
  { title: "When something is wrong", pages: ["alerts", "troubleshooting/index"] },
  {
    title: "Troubleshooting",
    // In the order of the diagnosis rules (internal/diagnosis).
    pages: [
      "consumer-too-slow",
      "nobody-consuming",
      "behind-retention",
      "idle-consumers",
      "frequent-rebalances",
      "hot-partition",
      "partitions-added",
      "oversized-messages",
      "producer-errors",
      "producers-without-telemetry",
      "hanging-transaction",
      "open-transaction",
      "transactions-aborted",
      "producers-not-idempotent",
    ].map((rule) => `troubleshooting/${rule}`),
  },
];

/**
 * Every block of code the docs have, by page, the heading it is under and
 * its language. hack/connecting-guide-test.sh runs or checks each tested
 * one on kind, taking it by the same heading. The two AWS commands of
 * client.rack need an AWS account, and go untested. A block not listed
 * fails the build, as nothing would test it, and so does a listed one the
 * page lacks.
 */
const code: { page: string; heading: string; language: string; tested: boolean }[] = [
  { page: "connecting", heading: "Your connection details", language: "properties", tested: true },
  { page: "connecting", heading: "Set client.rack on consumers", language: "bash", tested: false },
  { page: "connecting", heading: "Set client.rack on consumers", language: "bash", tested: false },
  { page: "connecting", heading: "Java", language: "java", tested: true },
  { page: "connecting", heading: "Python", language: "python", tested: true },
  { page: "connecting", heading: "Go", language: "go", tested: true },
  { page: "connecting", heading: "Node.js", language: "js", tested: true },
  { page: "connecting", heading: "Command line", language: "bash", tested: true },
  { page: "connecting", heading: "Read, process, write in Java", language: "java", tested: true },
];

/** The languages code in the docs may be in. */
const languages = ["bash", "go", "java", "js", "properties", "python"] as const;

interface Heading {
  depth: number;
  id: string;
  text: string;
}

interface Page {
  name: string;
  url: string;
  title: string;
  description: string;
  html: string;
  headings: Heading[];
  links: string[];
}

/** A page's address on the site: index pages by their directory. */
export function url(name: string): string {
  return name === "index" || name.endsWith("/index")
    ? `/docs/${name.slice(0, -"index".length)}`
    : `/docs/${name}.html`;
}

/**
 * The anchor GitHub gives a heading, so that links to sections work alike
 * on the site and on GitHub: lowercase, punctuation but dashes and
 * underscores dropped, spaces as dashes.
 */
export function slug(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s_-]/gu, "")
    .replace(/\s/g, "-");
}

const entities: Record<string, string> = {
  "&amp;": "&",
  "&lt;": "<",
  "&gt;": ">",
  "&quot;": '"',
  "&#39;": "'",
};

/** The text of a piece of HTML, for headings' anchors and descriptions. */
function plain(html: string): string {
  return html.replace(/<[^>]+>/g, "").replace(/&(amp|lt|gt|quot|#39);/g, (e) => entities[e] ?? e);
}

function escape(text: string): string {
  return text.replace(/[&<>"]/g, (c) => `&#${String(c.charCodeAt(0))};`);
}

function names(dir: string, prefix = ""): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) =>
    entry.isDirectory()
      ? names(path.join(dir, entry.name), `${prefix}${entry.name}/`)
      : entry.name.endsWith(".md")
        ? [`${prefix}${entry.name.slice(0, -3)}`]
        : [],
  );
}

async function parse(name: string): Promise<Page> {
  const highlighter = await highlighting;
  const source = readFileSync(path.join(content, `${name}.md`), "utf8");
  const headings: Heading[] = [];
  const seen = new Map<string, number>();
  const links: string[] = [];
  const problems: string[] = [];
  const blocks = code.filter((block) => block.page === name);
  const marked = new Marked({
    gfm: true,
    renderer: {
      heading({ tokens, depth }) {
        const html = this.parser.parseInline(tokens);
        // GitHub numbers a heading's anchor when an earlier one has it.
        const base = slug(plain(html));
        const taken = seen.get(base) ?? 0;
        seen.set(base, taken + 1);
        const id = taken > 0 ? `${base}-${String(taken)}` : base;
        headings.push({ depth, id, text: plain(html) });
        if (depth === 1) {
          return `<h1>${html}</h1>\n`;
        }
        return `<h${String(depth)} id="${id}">${html}<a class="anchor" href="#${id}" aria-label="Link to this section">#</a></h${String(depth)}>\n`;
      },
      link({ href, title, tokens }) {
        const text = this.parser.parseInline(tokens);
        const titled = title ? ` title="${escape(title)}"` : "";
        if (/^(https?:|mailto:|%CONSOLE_URL%|%SITE_URL%)/.test(href)) {
          return `<a href="${escape(href)}"${titled}>${text}</a>`;
        }
        // A link to another page of the docs, as GitHub resolves it: a
        // path relative to this page's file, and perhaps a section.
        const [file = "", anchor] = href.split("#");
        const target = file
          ? path.posix
              .normalize(path.posix.join(path.posix.dirname(name), file))
              .replace(/\.md$/, "")
          : name;
        if (file && !file.endsWith(".md")) {
          problems.push(`links to ${href}, which is not a page of the docs`);
        }
        links.push(anchor ? `${target}#${anchor}` : target);
        const resolved = `${target === name && !file ? "" : url(target)}${anchor ? `#${anchor}` : ""}`;
        return `<a href="${escape(resolved)}"${titled}>${text}</a>`;
      },
      code({ text, lang }) {
        const heading = headings.at(-1)?.text ?? "";
        const listed = blocks.findIndex((b) => b.heading === heading && b.language === lang);
        if (listed < 0) {
          problems.push(
            `has ${lang ?? "unlabelled"} code under "${heading}", which nothing tests: code goes where hack/connecting-guide-test.sh runs it, and into code in scripts/docs.ts`,
          );
        } else {
          blocks.splice(listed, 1);
        }
        const language = languages.find((l) => l === lang);
        if (!language) {
          problems.push(`has code in ${lang ?? "no language"}, not one of ${languages.join(", ")}`);
          return "";
        }
        return highlighter.codeToHtml(text, {
          lang: language,
          theme: "github-light-high-contrast",
        });
      },
      html({ text }) {
        // Links, images and code in HTML would escape the checks above.
        if (!/^\s*<!--[\s\S]*-->\s*$/.test(text)) {
          problems.push(`has HTML, ${text.trim().slice(0, 40)}: write Markdown instead`);
        }
        return "";
      },
      table(token: Tokens.Table) {
        // Wide tables scroll on their own rather than the page sideways.
        const cell = (c: Tokens.TableCell, tag: string) =>
          `<${tag}${c.align ? ` style="text-align:${c.align}"` : ""}>${this.parser.parseInline(c.tokens)}</${tag}>`;
        const head = token.header.map((c) => cell(c, "th")).join("");
        const rows = token.rows.map((row) => `<tr>${row.map((c) => cell(c, "td")).join("")}</tr>`);
        return `<div class="table"><table><thead><tr>${head}</tr></thead><tbody>${rows.join("")}</tbody></table></div>\n`;
      },
    },
  });
  const tokens = marked.lexer(source);
  const html = marked.parser(tokens);
  const first = tokens.find((t) => t.type !== "space");
  const paragraph = tokens.find((t): t is Tokens.Paragraph => t.type === "paragraph");
  if (first?.type !== "heading" || first.depth !== 1) {
    problems.push("does not start with its title, a # heading");
  }
  if (headings.filter((h) => h.depth === 1).length !== 1) {
    problems.push("has more than one # heading");
  }
  if (!paragraph) {
    problems.push("has no paragraph to describe it");
  }
  for (const block of blocks) {
    problems.push(
      `lacks the ${block.language} code under "${block.heading}" that scripts/docs.ts lists`,
    );
  }
  if (problems.length > 0) {
    throw new Error(problems.map((p) => `content/docs/${name}.md ${p}`).join("\n"));
  }
  return {
    name,
    url: url(name),
    title: headings[0]?.text ?? "",
    description: plain(marked.parseInline(paragraph?.text ?? "") as string).replace(/\s+/g, " "),
    html,
    headings,
    links,
  };
}

const highlighting = createHighlighter({
  themes: ["github-light-high-contrast"],
  langs: [...languages],
});

/** The links between pages that lead nowhere. */
function brokenLinks(pages: Map<string, Page>): string[] {
  return [...pages.values()].flatMap((page) =>
    page.links.flatMap((link) => {
      const [target = "", anchor] = link.split("#");
      const linked = pages.get(target);
      if (!linked) {
        return [`content/docs/${page.name}.md links to ${target}.md, which does not exist`];
      }
      // The title has no anchor: a link to it is a link to the page.
      if (anchor && !linked.headings.some((h) => h.depth > 1 && h.id === anchor)) {
        return [`content/docs/${page.name}.md links to ${link}, a section ${target}.md lacks`];
      }
      return [];
    }),
  );
}

function sidebar(current: Page, pages: Map<string, Page>): string {
  // The troubleshooting pages show only among themselves: elsewhere their
  // index stands for them, and keeps the navigation short.
  const troubleshooting = current.name.startsWith("troubleshooting/");
  return navigation
    .filter((section) => troubleshooting || section.title !== "Troubleshooting")
    .map(
      (section) => `<div>
        <h2 class="docs-nav-title">${section.title}</h2>
        <ul>${section.pages
          .map((name) => {
            const page = pages.get(name);
            const here = name === current.name ? ' aria-current="page"' : "";
            return `<li><a href="${url(name)}"${here}>${escape(page?.title ?? name)}</a></li>`;
          })
          .join("")}</ul>
      </div>`,
    )
    .join("");
}

function layout(page: Page, pages: Map<string, Page>): string {
  const order = navigation.flatMap((s) => s.pages);
  const index = order.indexOf(page.name);
  const previous = pages.get(order[index - 1] ?? "");
  const next = pages.get(order[index + 1] ?? "");
  const sections = page.headings.filter((h) => h.depth === 2);
  const title = page.name === "index" ? "Changeport docs" : `${page.title}: Changeport docs`;
  return `<!doctype html>
<html lang="en">
  <head>
    <!-- include:head -->
    <title>${escape(title)}</title>
    <meta name="description" content="${escape(page.description)}" />
    <link rel="canonical" href="%SITE_URL%${page.url}" />
  </head>
  <body class="bg-white font-sans text-ink antialiased">
    <!-- include:header -->
    <div class="docs mx-auto max-w-6xl px-6">
      <nav aria-label="Docs" class="docs-nav max-lg:hidden">${sidebar(page, pages)}</nav>
      <main id="main" class="min-w-0 pt-8 pb-20 lg:pt-12">
        <details class="docs-menu lg:hidden">
          <summary>All docs</summary>
          <nav aria-label="Docs">${sidebar(page, pages)}</nav>
        </details>
        <article class="doc">${page.html}</article>
        <nav aria-label="Previous and next" class="docs-pager">
          ${previous ? `<a href="${previous.url}" rel="prev"><span>Previous</span>${escape(previous.title)}</a>` : "<span></span>"}
          ${next ? `<a href="${next.url}" rel="next"><span>Next</span>${escape(next.title)}</a>` : ""}
        </nav>
      </main>
      ${
        sections.length > 2
          ? `<nav aria-label="On this page" class="docs-toc max-xl:hidden">
        <h2 class="docs-nav-title">On this page</h2>
        <ul>${sections.map((h) => `<li><a href="#${h.id}">${escape(h.text)}</a></li>`).join("")}</ul>
      </nav>`
          : ""
      }
    </div>
    <!-- include:footer -->
  </body>
</html>
`;
}

/**
 * Renders every page of the docs into docs/, and returns their paths
 * relative to the site, for Vite to build.
 */
export async function renderDocs(): Promise<string[]> {
  const found = names(content).sort();
  const listed = navigation.flatMap((s) => s.pages);
  const problems = [
    ...found
      .filter((n) => !listed.includes(n))
      .map((n) => `content/docs/${n}.md is not in the docs' navigation (scripts/docs.ts)`),
    ...listed
      .filter((n) => !found.includes(n))
      .map((n) => `the docs' navigation lists ${n}, but content/docs/${n}.md does not exist`),
  ];
  if (problems.length > 0) {
    throw new Error(problems.join("\n"));
  }
  const pages = new Map((await Promise.all(found.map(parse))).map((p) => [p.name, p]));
  const broken = brokenLinks(pages);
  if (broken.length > 0) {
    throw new Error(broken.join("\n"));
  }
  rmSync(out, { recursive: true, force: true });
  return [...pages.values()].map((page) => {
    const file = path.join(out, `${page.name}.html`);
    mkdirSync(path.dirname(file), { recursive: true });
    writeFileSync(file, layout(page, pages));
    return path.relative(root, file);
  });
}

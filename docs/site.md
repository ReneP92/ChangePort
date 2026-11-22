# The landing site

`site/` is the public site at the domain's root: what Changeport is, how a
trial works, questions and answers, and the call to action that leads to
the console's trial request (`<console>/trial`). It also holds the
customer docs, under `/docs/` (see [The customer docs](#the-customer-docs)),
and the privacy policy and the terms, as plain-language drafts for legal
review before launch.

It is plain HTML with Tailwind's CSS in the console's brand
([docs/brand](brand)), and one small script with no framework,
`site/src/main.ts`, for its animations and interactive parts: the hero's
model of data streaming through Changeport, which plays a story of an app
and then a source falling behind and the alerts Changeport raises (a column
of labels on phones, whose text keeps its size), and also tells the same in
plain words, the Postgres change data capture demo,
the lag simulator and the console tour. There are no cookies, no analytics and no
third-party requests. Vite builds it into `site/dist`, a folder of static
files, which CloudFront serves from a private bucket in the prod account
(see [Publishing](#publishing)).

The page is complete without the script: sections show, every console
screen shows, and the demos show a still state. The script only adds to it,
and keeps still for visitors who ask their system for reduced motion: the
diagrams pause, nothing rises into view, and the tour waits to be clicked.

## Working on it

```bash
just site-dev     # hot reload on http://localhost:5174
just site-build   # site/dist
just site-check   # type-check, format-check, build, test, then Lighthouse
```

- **Pages** are `site/index.html`, `site/privacy.html`, `site/terms.html`
  and `site/404.html`, which the host serves for an address it has no page
  for, asking search engines not to index it.
  A new page also goes into `build.rolldownOptions.input` in
  `site/vite.config.ts`, and into the pages that `site/scripts/lighthouse.ts`
  audits.
- **Shared parts**, such as the header and footer, are in
  `site/src/partials`. A page includes one where it says
  `<!-- include:header -->`.
- **The hero's model** is drawn by `site/scripts/streams.ts`, from its
  blocks, lanes and which apps subscribe to which source, where the page
  says `<!-- include:streams -->`; `site/src/main.ts` plays its story, at
  the levels the script draws.
- **Addresses** come from the environment when the site is built:
  `CHANGEPORT_CONSOLE_URL`, where Sign in and every call to action lead
  (`http://localhost:8088` by default, the console on kind), and
  `CHANGEPORT_SITE_URL`, for the canonical links (`http://localhost:4174`,
  `vite preview`'s address). Pages write them as `%CONSOLE_URL%` and
  `%SITE_URL%`.
- **The stylesheet** is put into each page as a `<style>` when the site is
  built, so the browser can draw it without a second request.
- **Fonts** are preloaded, the Latin Geist and Geist Mono, and optional
  rather than swapped in (`steadyFonts` in `site/vite.config.ts`): a page
  uses them if they arrive in its first moment, and otherwise keeps the
  system font until the next page, which finds them cached. Swapping them in
  late moves text that is already drawn, which Lighthouse counts as layout
  shift, and Linux's fallback fonts, as CI has, differ from Geist enough to
  fail the check.
- **Animations** are CSS transitions and keyframes, and SMIL for the moving
  dots of the SVG diagrams. Anything that hides content until it animates
  sits behind the `js` class, which an inline script puts on the root
  element, so that the page never depends on the script to show. Elements
  with `data-reveal` rise into view; `--delay` staggers them.

## The customer docs

The docs customers build with are on the site, under `/docs/`: public, so
that a link from an alert or a diagnosis works without signing in, a
prospect can read them, and search engines find them. They are written in
Markdown, in `site/content/docs`, which GitHub shows as it is, and
`site/scripts/docs.ts` renders them into pages when the site is built, or
again as they change under `just site-dev`. The pages are written into
`site/docs`, which git ignores, so that the Markdown is the one copy.

- **Plain language first, Kafka's terms second.** Each page starts with its
  title and a paragraph that says what it is about, which is its
  description for search engines.
- **One page per diagnosis rule,** under `troubleshooting/`, named by the
  rule. A test in `internal/diagnosis` fails a rule without its page.
- **Links between pages** are relative links to the Markdown files, as
  GitHub follows them, with the sections' anchors as GitHub makes them. The
  build turns them into the pages' addresses, and fails on a link to a page
  or a section that does not exist.
- **The navigation** is `navigation` in `site/scripts/docs.ts`; the build
  fails on a page it lacks, and on one it names that does not exist. The
  troubleshooting pages show in it only among themselves.
- **Code** is allowed only in the connection guide, whose examples
  `hack/connecting-guide-test.sh` runs on kind; elsewhere it fails the
  build, as nothing would test it.
- **Code is highlighted** when the site is built (Shiki, its high-contrast
  light theme), so the pages need no script for it.
- **Folders' index pages,** such as `/docs/`, are served by the CloudFront
  function that redirects www (`terraform/modules/static-site`), as the
  bucket has no folders.

The console links each diagnosis, alert and topic change warning to its
page. It and the provisioner learn where the docs are from
`CHANGEPORT_DOCS_URL`; on kind that is `http://localhost:5174/docs`, so the
links work while `just site-dev` runs.

## Tests

`site/tests` checks the interactive parts in Chromium against the build, on
a desktop and a phone: that the lag simulator alerts past a minute behind
and resolves, that the tour switches screens by click, arrow keys and by
itself, that the questions open, that phones get a menu and nothing scrolls
sideways, that the hero tells its streams in plain words and back, that
reduced motion keeps the hero still, that without the script every section
and console screen shows, and that the docs lead from
page to page with nothing scrolling sideways. axe checks every page, and a
page of each kind of the docs, for accessibility violations, such as text
or code without enough contrast, with
reduced motion so that every section is on show: Lighthouse only audits
what is visible on load, and misses what waits to rise into view.

## Colors and copy

The site leads with real-time data streams and their observability, and
mentions Kafka as what runs under the hood. Its palette is crisp white with
near-black text (`--color-ink`), neutral greys, `--color-mist` to set every
other section apart, and the brand's teal as the one accent, kept apart from
the navy and blues of other streaming products. Small grey text is
`neutral-600` or darker wherever it may sit on mist.

On wide screens the hero fills the screen under the announcement and the
header, so the next section starts exactly at the fold, whatever the
screen's height, instead of peeking in cut off.

## Console screens

The console screens the site shows are WebP images in
`site/public/screens`, committed: four screens, the overview, lag, the
timeline and the topology map, each at its full size and at the widths the
pages' `srcset` attributes name. They were cropped from the real console on
kind at twice the pixels. Phones get crops of the console's phone layout,
through a `<picture>` `<source>`, since the desktop's would be too small to
read. When the console changes enough that a screen looks dated, capture it
again by hand and replace its files at the same sizes.

## Lighthouse

`just site-check` serves the build with `vite preview` and audits every
page, and the docs' index, connection guide and a troubleshooting page,
with Lighthouse, on a throttled phone and on a desktop, in the Chromium
that Playwright installs. It fails unless every page scores at least 95 for
performance, accessibility, best practices and SEO, and names the audits
that fell short. Its reports are in `site/lighthouse`; CI runs it, after the tests, in the
`Site tests and Lighthouse` job, and uploads both reports as the
`site-reports` artifact when it fails.

## Publishing

Once prod has a domain (#8), `.github/workflows/publish-site.yml` builds the
site for it after every green CI run for main's newest commit, with the console at
`console.<domain>`, and publishes it to its bucket behind CloudFront
(`hack/publish-site.sh`, [terraform/README.md](../terraform/README.md#landing-site-modulesstatic-site-prod-only)).
A merge reaches visitors a few minutes after CI passes.

Each page carries a Content-Security-Policy, which the build writes into it:
the site's own files only, and of inline scripts only those the page was
built with, by their hashes. A new inline script is covered on the next
build, and a test fails if anything on a page is refused. CloudFront adds
the headers a page cannot set for itself, such as HSTS and the refusal to be
framed.

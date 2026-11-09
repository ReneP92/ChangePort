# The landing site

`site/` is the public site at the domain's root: what Changeport is, how a
trial works, questions and answers, and the call to action that leads to
the console's trial request (`<console>/trial`). It also holds the privacy
policy and the terms, as plain-language drafts for legal review before
launch.

It is plain HTML with Tailwind's CSS in the console's brand
([docs/brand](brand)): no JavaScript at runtime, no cookies, no analytics
and no third-party requests. Vite builds it into `site/dist`, a folder of
static files that any static host can serve; publishing it is #138.

## Working on it

```bash
just site-dev     # hot reload on http://localhost:5174
just site-build   # site/dist
just site-check   # type-check, format-check, build, then Lighthouse
```

- **Pages** are `site/index.html`, `site/privacy.html` and `site/terms.html`.
  A new page also goes into `build.rolldownOptions.input` in
  `site/vite.config.ts`, and into the pages that `site/scripts/lighthouse.ts`
  audits.
- **Shared parts**, such as the header and footer, are in
  `site/src/partials`. A page includes one where it says
  `<!-- include:header -->`.
- **Addresses** come from the environment when the site is built:
  `CHANGEPORT_CONSOLE_URL`, where Sign in and every call to action lead
  (`http://localhost:8088` by default, the console on kind), and
  `CHANGEPORT_SITE_URL`, for the canonical links (`http://localhost:4174`,
  `vite preview`'s address). Pages write them as `%CONSOLE_URL%` and
  `%SITE_URL%`.
- **The stylesheet** is put into each page as a `<style>` when the site is
  built, so the browser can draw it without a second request.

## Screenshots

The console screenshots are crops of `docs/screenshots`, which
`just ui-screenshots` captures from the real console on kind at twice the
pixels. `site/scripts/screens.ts` crops them into `site/public/screens` on
every build, at the widths the pages' `srcset` attributes name, so they are
not committed twice and new screenshots reach the site on the next build.
Phones get crops of the console's phone layout, through a `<picture>`
`<source>`, since the desktop's would be too small to read. When a capture
changes its layout, check the crop regions in `screens.ts` against
`docs/screenshots`; they are in the console's CSS pixels.

## Lighthouse

`just site-check` serves the build with `vite preview` and audits every
page with Lighthouse, on a throttled phone and on a desktop, in the Chromium
that Playwright installs. It fails unless every page scores at least 95 for
performance, accessibility, best practices and SEO, and names the audits
that fell short. Its reports are in `site/lighthouse`; CI runs it in the
`Site build and Lighthouse` job, and uploads them as the `site-lighthouse`
artifact when it fails.

// Writes the console screenshots the site shows into public/screens, as
// WebP crops of docs/screenshots, which just ui-screenshots captures from
// the real console on kind at twice the pixels. Each crop is written at its
// full size and at the narrower widths index.html's srcset attributes name,
// so that every screen gets the pixels it shows. The crops are generated
// rather than committed, so that new screenshots reach the site without a
// second copy.
import { mkdirSync } from "node:fs";
import path from "node:path";
import sharp from "sharp";

const root = path.join(import.meta.dirname, "..");
const shots = path.join(root, "..", "docs", "screenshots");
const out = path.join(root, "public", "screens");

// Each crop: its source, and the region of it, in the console's CSS pixels:
// the desktop screenshots are 1280 wide, and the phone's 390.
// The phone crops come from the console's own phone layout, since the
// desktop's would be too small to read on a phone.
const crops = [
  // The overview: an alert, how to connect, and the brokers' health.
  {
    name: "overview",
    from: "03-overview-desktop.png",
    left: 130,
    top: 0,
    width: 1020,
    height: 820,
  },
  // The alert, in the phone's header.
  {
    name: "overview-phone",
    from: "03-overview-phone.png",
    left: 0,
    top: 0,
    width: 390,
    height: 372,
  },
  // What changed, under the throughput chart.
  {
    name: "timeline",
    from: "03-overview-desktop.png",
    left: 140,
    top: 815,
    width: 1000,
    height: 680,
  },
  {
    name: "timeline-phone",
    from: "03-overview-phone.png",
    left: 0,
    top: 1250,
    width: 390,
    height: 742,
  },
  // Producers, topics and consumer groups, with their traffic and lag.
  {
    name: "topology",
    from: "03-overview-desktop.png",
    left: 140,
    top: 1500,
    width: 1000,
    height: 315,
  },
  {
    name: "topology-phone",
    from: "03-overview-phone.png",
    left: 0,
    top: 1994,
    width: 390,
    height: 374,
  },
  // A consumer group's lag in time, and what changed meanwhile.
  {
    name: "lag",
    from: "18-consumer-group-desktop.png",
    left: 140,
    top: 60,
    width: 1000,
    height: 760,
  },
  {
    name: "lag-phone",
    from: "18-consumer-group-phone.png",
    left: 0,
    top: 100,
    width: 390,
    height: 950,
  },
];

// The narrower widths each crop is written at, as name-width.webp.
const widths = { desktop: [640, 960, 1280], phone: [390] };

mkdirSync(out, { recursive: true });
for (const { name, from, ...region } of crops) {
  const source = sharp(path.join(shots, from));
  const device = from.endsWith("-phone.png") ? "phone" : "desktop";
  const scale = ((await source.metadata()).width ?? 0) / (device === "phone" ? 390 : 1280);
  const pixels = Object.fromEntries(
    Object.entries(region).map(([key, value]) => [key, Math.round(value * scale)]),
  ) as typeof region;
  const crop = source.extract(pixels);
  await crop
    .clone()
    .webp({ quality: 82 })
    .toFile(path.join(out, `${name}.webp`));
  for (const width of widths[device].filter((width) => width < pixels.width)) {
    await crop
      .clone()
      .resize({ width })
      .webp({ quality: 82 })
      .toFile(path.join(out, `${name}-${String(width)}.webp`));
  }
}
console.log(`Wrote ${String(crops.length)} screens into public/screens`);

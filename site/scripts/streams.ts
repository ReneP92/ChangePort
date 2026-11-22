/**
 * Draws the hero's model of a customer's streams (docs/site.md): three
 * sources send their changes into Changeport, which routes each change only
 * to the apps that subscribe to its source, and watches every source and
 * every app. Each block carries a stack of the changes waiting there; past
 * its dashed line, Changeport raises an alert. src/main.ts plays the story:
 * which stacks grow, and which alerts fire.
 *
 * The model lies on a 480 by 480 plane, z up, drawn in a 2:1 isometric
 * projection; the names are HTML labels over it, which keep their size as
 * the drawing scales. The pages plugin in vite.config.ts puts the result
 * where index.html says <!-- include:streams -->.
 */

type Point = readonly [number, number];
/** A rectangle on the plane: x0, y0, x1, y1. */
type Rect = readonly [number, number, number, number];

const C = Math.SQRT1_2; // x and y along the screen
const K = 0.35355339; // x and y down the screen
const H = 0.8660254; // z up the screen

/** The screen point of plane point (x, y) at height z. */
const P = (x: number, y: number, z = 0): Point => [
  380 + C * (x - y),
  320 + K * (x + y - 480) - H * z,
];

/** The part of the screen the model takes, with the plate's corners outside. */
const view = { x: 60, y: 112, width: 640, height: 384 };

const n = (v: number) => String(Math.round(v * 10) / 10);
const pts = (...points: Point[]) => points.map(([x, y]) => `${n(x)},${n(y)}`).join(" ");
const path = (points: Point[]) => "M" + points.map(([x, y]) => `${n(x)} ${n(y)}`).join(" L");

/** The transform that draws plane coordinates at height z. */
const plane = (z: number) => {
  const [x, y] = P(0, 0, z);
  return `matrix(0.7071 0.3536 -0.7071 0.3536 ${n(x)} ${n(y)})`;
};

/** The transform that draws (u, v) on an upright face along x at y, v down from top. */
const upright = (x: number, y: number, top: number) => {
  const [sx, sy] = P(x, y, top);
  return `matrix(0.7071 0.3536 0 0.866 ${n(sx)} ${n(sy)})`;
};

/** Mixes a color with white (amount > 0) or black (amount < 0). */
const shade = (hex: string, amount: number) => {
  const to = amount > 0 ? 255 : 0;
  const a = Math.abs(amount);
  return (
    "#" +
    [1, 3, 5]
      .map((i) => Math.round(parseInt(hex.slice(i, i + 2), 16) * (1 - a) + to * a))
      .map((v) => v.toString(16).padStart(2, "0"))
      .join("")
  );
};

/** A box's visible faces: its front (y = y1), its right (x = x1), then its top. */
function box(
  [x0, y0, x1, y1]: Rect,
  z0: number,
  z1: number,
  [top, front, side]: readonly [string, string, string],
  extra = "",
) {
  return (
    `<polygon fill="${front}" points="${pts(P(x0, y1, z0), P(x1, y1, z0), P(x1, y1, z1), P(x0, y1, z1))}" />` +
    `<polygon fill="${side}" points="${pts(P(x1, y0, z0), P(x1, y1, z0), P(x1, y1, z1), P(x1, y0, z1))}" />` +
    `<polygon fill="${top}" points="${pts(P(x0, y0, z1), P(x1, y0, z1), P(x1, y1, z1), P(x0, y1, z1))}"${extra} />`
  );
}

/** A cube of edge 2h standing on (0, 0, 0), its faces colored by the classes cl, cr and ct. */
function cube(h: number) {
  const d = (x: number, y: number, z: number): Point => [C * (x - y), K * (x + y) - H * z];
  return (
    `<polygon class="cl" points="${pts(d(-h, h, 0), d(h, h, 0), d(h, h, 2 * h), d(-h, h, 2 * h))}" />` +
    `<polygon class="cr" points="${pts(d(h, -h, 0), d(h, h, 0), d(h, h, 2 * h), d(h, -h, 2 * h))}" />` +
    `<polygon class="ct" points="${pts(d(-h, -h, 2 * h), d(h, -h, 2 * h), d(h, h, 2 * h), d(-h, h, 2 * h))}" />`
  );
}

/** The outline of a cube of edge 2h standing on (0, 0, 0). */
function outline(h: number) {
  const d = (x: number, y: number, z: number): Point => [C * (x - y), K * (x + y) - H * z];
  return pts(
    d(-h, h, 0),
    d(h, h, 0),
    d(h, -h, 0),
    d(h, -h, 2 * h),
    d(-h, -h, 2 * h),
    d(-h, h, 2 * h),
  );
}

const ICONS = {
  database:
    "M4 6c0-1.7 3.6-3 8-3s8 1.3 8 3-3.6 3-8 3-8-1.3-8-3zM4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3",
  cart: "M3 4h2l2.2 10.4a1 1 0 0 0 1 .8h8.6a1 1 0 0 0 1-.8L19.5 8H6M9 20h.01M17 20h.01",
  card: "M3 6h18v12H3zM3 10h18M7 15h4",
  receipt: "M6 3h12v18l-3-2-3 2-3-2-3 2zM9 8h6M9 12h6M9 16h3",
  shield: "M12 3l8 3v6c0 4.5-3.4 8-8 9-4.6-1-8-4.5-8-9V6zM9 12l2 2 4-4",
  search: "M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14zM20 20l-4-4",
  warehouse: "M3 9l9-5 9 5v11H3zM7 20v-7h10v7M7 16.5h10",
};

type Source = "pg" | "checkout" | "payments";
type App = "billing" | "fraud" | "search" | "warehouse";

/** Each source's color, as the labels and the console show it too. */
export const colors: Record<Source, string> = {
  pg: "#2f9e8f",
  checkout: "#3b6fd8",
  payments: "#9457e0",
};

/** The sources each app subscribes to. */
export const subscriptions: Record<App, Source[]> = {
  billing: ["pg", "payments"],
  fraud: ["checkout", "payments"],
  search: ["checkout"],
  warehouse: ["pg", "checkout"],
};

/** The blocks, each by the corner of its 84 by 84 base on the plane. */
const stations: Record<Source | App, { at: Point; icon: keyof typeof ICONS }> = {
  pg: { at: [18, 48], icon: "database" },
  checkout: { at: [18, 198], icon: "cart" },
  payments: { at: [18, 348], icon: "card" },
  billing: { at: [378, 18], icon: "receipt" },
  fraud: { at: [378, 138], icon: "shield" },
  search: { at: [378, 258], icon: "search" },
  warehouse: { at: [378, 378], icon: "warehouse" },
};

/** The levels a stack has, and the level past which Changeport alerts. */
export const levels = 8;
export const threshold = 4;

/** The classes of a block, or its label, at a level: behind past the line, and far behind. */
const state = (level: number) =>
  level > threshold ? (level >= 7 ? " is-behind is-severe" : " is-behind") : "";

/**
 * The stack of changes waiting at a block, with its dashed line, and the
 * ring that ripples out while it alerts. Its levels show as main.ts sets
 * data-level, or the level the page starts at.
 */
function stack(id: Source | App, cx: number, cy: number, start: number) {
  const z = 24.5;
  const [rx, ry] = P(cx, cy, z);
  const rings = `<ellipse class="stack-ring" cx="${n(rx)}" cy="${n(ry)}" rx="26" ry="13" />`;
  const disc = `<ellipse class="stack-disc" cx="${n(rx)}" cy="${n(ry)}" rx="17" ry="8.5" />`;
  const cubes = Array.from({ length: levels }, (_, i) => {
    const [x, y] = P(cx, cy, z + i * 11);
    return `<g class="level${i < start ? " on" : ""}" transform="translate(${n(x)} ${n(y)})">${cube(5.5)}</g>`;
  }).join("");
  const [lx, ly] = P(cx, cy, z + threshold * 11);
  const line = `<polygon class="stack-line" transform="translate(${n(lx)} ${n(ly)})" points="${outline(5.5)}" />`;
  return `<g class="stack${state(start)}" data-node="${id}">${rings}${disc}${cubes}${line}</g>`;
}

/** A source's or an app's block: a base, an inner tile, its stack and an upright icon. */
function station(id: Source | App, start: number) {
  const {
    at: [x0, y0],
    icon,
  } = stations[id];
  const color = id in colors ? colors[id as Source] : "#8c8c86";
  return (
    `<g style="--c:${color};--ct:${shade(color, 0.5)};--cr:${shade(color, -0.38)}">` +
    box([x0, y0, x0 + 84, y0 + 84], 0, 16, ["white", "#141414", "#30353a"], ' class="node"') +
    box(
      [x0 + 12, y0 + 12, x0 + 72, y0 + 72],
      16,
      24,
      ["#eef0f2", "#3a3f45", "#575d64"],
      ' stroke="#dadde1" stroke-width="1"',
    ) +
    stack(id, x0 + 54, y0 + 30, start) +
    `<g transform="${upright(x0 + 16, y0 + 64, 62)}">` +
    `<rect width="34" height="34" rx="9" fill="#141414" stroke="#2c3035" stroke-width="1" />` +
    `<path transform="translate(7 7) scale(0.8333)" d="${ICONS[icon]}" fill="none" stroke="white" stroke-width="2.16" stroke-linecap="round" stroke-linejoin="round" /></g>` +
    `</g>`
  );
}

/** A lane on the plate, flat, with its belt running along it. */
function lane([x0, y0, x1, y1]: Rect) {
  return box([x0, y0, x1, y1], 0, 4, ["#dcdcd7", "#c4c4be", "#b8b8b2"]);
}

// The lanes: each source's into a trunk, which runs into Changeport, and one
// out of it into a trunk that branches to every app.
const lanes: Rect[] = [
  [102, 84, 136, 96],
  [102, 234, 160, 246],
  [102, 384, 136, 396],
  [124, 84, 136, 396],
  [320, 234, 356, 246],
  [344, 54, 356, 426],
  [344, 54, 378, 66],
  [344, 174, 378, 186],
  [344, 294, 378, 306],
  [344, 414, 378, 426],
];

// The way each source's changes travel into Changeport, and each app's out
// of it, along the middle of the lanes.
const into: Record<Source, Point[]> = {
  pg: [
    [102, 90],
    [130, 90],
    [130, 240],
    [160, 240],
  ],
  checkout: [
    [102, 240],
    [160, 240],
  ],
  payments: [
    [102, 390],
    [130, 390],
    [130, 240],
    [160, 240],
  ],
};
const out: Record<App, Point[]> = {
  billing: [
    [320, 240],
    [350, 240],
    [350, 60],
    [378, 60],
  ],
  fraud: [
    [320, 240],
    [350, 240],
    [350, 180],
    [378, 180],
  ],
  search: [
    [320, 240],
    [350, 240],
    [350, 300],
    [378, 300],
  ],
  warehouse: [
    [320, 240],
    [350, 240],
    [350, 420],
    [378, 420],
  ],
};

const length = (points: Point[]) =>
  points.reduce(
    (sum, [x, y], i) => (i ? sum + Math.hypot(x - points[i - 1]![0], y - points[i - 1]![1]) : 0),
    0,
  );

/**
 * Changes moving along a way, as cubes in the colors of their sources, at
 * one speed everywhere. The odd ones fade while their source falls behind,
 * as fewer of its changes get through (main.ts).
 */
function changes(way: Point[], from: Source[], count: number) {
  const d = path(way.map(([x, y]) => P(x, y, 4)));
  const seconds = length(way) / 46;
  return Array.from({ length: count }, (_, i) => {
    const source = from[i % from.length]!;
    const begin = i ? `-${n((seconds * i) / count)}s` : "0s";
    return (
      `<g class="change" data-from="${source}" style="--ct:${shade(colors[source], 0.5)};--cl:${colors[source]};--cr:${shade(colors[source], -0.38)}">` +
      cube(5.5) +
      `<animateMotion dur="${n(seconds)}s" begin="${begin}" repeatCount="indefinite" path="${d}" /></g>`
    );
  }).join("");
}

/** Changeport: a raised block, a dark tile, and the console standing on it, routing. */
function changeport() {
  const routes: [Source, string][] = [
    ["checkout", "fraud"],
    ["pg", "billing"],
    ["payments", "fraud"],
    ["checkout", "search"],
    ["pg", "warehouse"],
    ["payments", "billing"],
    ["checkout", "warehouse"],
  ];
  // Rows of the console's log, twice, so that it scrolls on without a seam.
  const rows = [...routes, ...routes]
    .map(
      ([source], i) =>
        `<circle cx="12" cy="${8 + 9 * i}" r="2" fill="${colors[source]}" />` +
        `<rect x="18" y="${6.5 + 9 * i}" width="${40 + ((i * 13) % 22)}" height="3" rx="1.5" fill="#d5d9dd" />` +
        `<rect x="82" y="${6.5 + 9 * i}" width="8" height="3" rx="1.5" fill="#0d9488" />`,
    )
    .join("");
  return (
    box([160, 160, 320, 320], 0, 20, ["white", "#141414", "#30353a"], ' class="node"') +
    box([176, 176, 304, 304], 20, 28, ["#111111", "#0b0b0b", "#1f2226"]) +
    `<g transform="${upright(188, 182, 102)}">` +
    `<rect width="104" height="74" rx="9" fill="#141414" />` +
    `<circle cx="10" cy="8" r="2" fill="#2dd4bf" /><circle cx="17" cy="8" r="2" fill="#3a3f45" /><circle cx="24" cy="8" r="2" fill="#3a3f45" />` +
    `<clipPath id="streams-log"><rect x="6" y="15" width="92" height="53" rx="4" /></clipPath>` +
    `<rect x="6" y="15" width="92" height="53" rx="4" fill="white" />` +
    `<g clip-path="url(#streams-log)"><g transform="translate(0 15)"><g class="console-log">${rows}</g></g></g>` +
    `</g>`
  );
}

/** The level each block starts at: search-indexer behind, as without the script. */
export const start: Record<Source | App, number> = {
  pg: 1,
  checkout: 2,
  payments: 1,
  billing: 1,
  fraud: 2,
  search: 6,
  warehouse: 1,
};

/** The screen position of a label, in percent of the model's box. */
const at = (x: number, y: number, z = 0) => {
  const [sx, sy] = P(x, y, z);
  const pct = (v: number) => Math.round(v * 100) / 100;
  return `left: ${pct(((sx - view.x) / view.width) * 100)}%; top: ${pct(((sy - view.y) / view.height) * 100)}%`;
};

const names: Record<Source | App, string> = {
  pg: "Postgres: shop",
  checkout: "checkout-api",
  payments: "payments-api",
  billing: "billing",
  fraud: "fraud-checks",
  search: "search-indexer",
  warehouse: "warehouse-sync",
};

/**
 * Each block's label, on the side of it that no other block is behind:
 * above it, above and to the left, below, or to its left.
 */
const labels: [Source | App, string, string][] = [
  ["pg", at(60, 90, 66), "iso-above-left"],
  ["checkout", at(60, 240, 66), "iso-above-left"],
  ["payments", at(102, 432), "iso-below"],
  ["billing", at(420, 60, 66), ""],
  ["fraud", at(462, 222), "iso-below"],
  ["search", at(462, 342), "iso-below"],
  ["warehouse", at(378, 462, 8), "iso-left"],
];

function label([id, style, place]: (typeof labels)[number]) {
  const marks =
    id in colors
      ? `<span class="size-1.5 rounded-xs" style="background:${colors[id as Source]}"></span>`
      : subscriptions[id as App]
          .map((s) => `<span class="size-1.5 rounded-full" style="background:${colors[s]}"></span>`)
          .join("");
  return (
    `<span class="stream-label iso-label ${place}${state(start[id])}" data-chip="${id}" style="${style}">` +
    `<span class="flex gap-0.5">${marks}</span>${names[id]}<span class="stream-alert"></span></span>`
  );
}

/** The model: the drawing, its labels, and the alert Changeport raises. */
export function renderStreams(): string {
  const plate =
    `<defs><pattern id="iso-dots" width="16" height="16" patternUnits="userSpaceOnUse">` +
    `<rect width="16" height="16" fill="rgb(255 255 255 / 0.75)" /><circle cx="8" cy="8" r="1.1" fill="#d6dadf" /></pattern></defs>` +
    `<rect transform="${plane(-1)}" width="480" height="480" rx="32" fill="url(#iso-dots)" stroke="#e1e4e7" stroke-width="1.2" />`;
  const belts = Object.values(into)
    .concat(Object.values(out))
    .map((way) => `<path class="belt" d="${path(way.map(([x, y]) => P(x, y, 4)))}" />`)
    .join("");
  const moving =
    (Object.keys(into) as Source[])
      .map((s) => `<g data-route="${s}">${changes(into[s], [s], 3)}</g>`)
      .join("") +
    (Object.keys(out) as App[])
      .map((a) => `<g data-route="${a}">${changes(out[a], subscriptions[a], 4)}</g>`)
      .join("");
  // Back to front: the apps at the back, the sources, Changeport, the apps in front.
  const blocks =
    station("billing", start.billing) +
    station("pg", start.pg) +
    station("fraud", start.fraud) +
    changeport() +
    station("checkout", start.checkout) +
    station("search", start.search) +
    station("payments", start.payments) +
    station("warehouse", start.warehouse);
  const title =
    "Postgres: shop, checkout-api and payments-api send their changes into Changeport, which delivers each change only to the apps subscribed to its source: billing, fraud-checks, search-indexer and warehouse-sync. A stack on each shows the changes waiting there; past its dashed line, Changeport raises an alert.";
  return `<div class="relative hidden sm:block">
<svg data-flow viewBox="${view.x} ${view.y} ${view.width} ${view.height}" class="flow streams h-auto w-full overflow-visible" role="img" aria-labelledby="flow-title">
<title id="flow-title">${title}</title>
${plate}${lanes.map(lane).join("")}${belts}${moving}${blocks}
</svg>
<div aria-hidden="true">
${labels.map(label).join("\n")}
<span class="stream-label stream-label-dark iso-label iso-center" style="${at(240, 266, 28)}">Changeport</span>
</div>
</div>`;
}

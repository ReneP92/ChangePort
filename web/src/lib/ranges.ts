import type { ThroughputRange } from "@/api/client";

/** The ranges the console's charts offer. */
export const ranges: ThroughputRange[] = ["1h", "6h", "24h", "7d"];

const hour = 60 * 60 * 1000;

/** How long each range is, in milliseconds. */
export const rangeMilliseconds: Record<ThroughputRange, number> = {
  "1h": hour,
  "6h": 6 * hour,
  "24h": 24 * hour,
  "7d": 7 * 24 * hour,
};

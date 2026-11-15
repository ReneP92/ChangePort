import { describe, expect, test } from "bun:test";
import type { HealthLevel } from "@/api/client";
import { historySummary, worstFirst } from "@/lib/health";

const day = (fill: HealthLevel | undefined): (HealthLevel | undefined)[] =>
  Array.from({ length: 96 }, () => fill);

describe("historySummary", () => {
  test("healthy throughout", () => {
    expect(historySummary(day("healthy"), 900)).toBe("Healthy throughout the last 24 hours.");
  });

  test("says for how long it needed attention, or was at risk", () => {
    const levels = day("healthy");
    levels.fill("needsAttention", 10, 19);
    levels.fill("atRisk", 19, 20);
    expect(historySummary(levels, 900)).toBe(
      "In the last 24 hours, at risk for 15 minutes, and needed attention for 2 hours 15 minutes.",
    );
  });

  test("says where Changeport did not look", () => {
    const levels = day("healthy");
    levels.fill(undefined, 0, 40);
    expect(historySummary(levels, 900)).toBe(
      "Healthy throughout the last 24 hours, where Changeport looked.",
    );
    expect(historySummary(day(undefined), 900)).toBe(
      "Not looked at yet: the history starts once Changeport has looked at the cluster.",
    );
  });
});

test("worstFirst puts the worst first, and the unknown last", () => {
  const levels: (HealthLevel | undefined)[] = ["healthy", undefined, "atRisk", "needsAttention"];
  expect(levels.sort(worstFirst)).toEqual(["atRisk", "needsAttention", "healthy", undefined]);
});

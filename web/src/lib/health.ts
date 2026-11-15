import type { HealthLevel } from "@/api/client";

/** The levels, from best to worst, so that sorting worst first is a compare. */
export const healthOrder: HealthLevel[] = ["healthy", "needsAttention", "atRisk"];

/** How a level reads. */
export const healthLabels: Record<HealthLevel, string> = {
  healthy: "Healthy",
  needsAttention: "Needs attention",
  atRisk: "At risk",
};

/** Compares two levels, worst first; an unknown level comes last. */
export function worstFirst(a: HealthLevel | undefined, b: HealthLevel | undefined): number {
  const rank = (level: HealthLevel | undefined) => (level ? healthOrder.indexOf(level) : -1);
  return rank(b) - rank(a);
}

/**
 * Says how healthy a subject was over the history's steps, each 15 minutes:
 * healthy throughout, or how long it needed attention or was at risk, and
 * whether the console looked all along.
 */
export function historySummary(levels: (HealthLevel | undefined)[], stepSeconds: number): string {
  const minutes = (level: HealthLevel) =>
    (levels.filter((l) => l === level).length * stepSeconds) / 60;
  const span = (total: number) => {
    const hours = Math.floor(total / 60);
    const rest = total % 60;
    const parts = [];
    if (hours > 0) {
      parts.push(`${String(hours)} hour${hours === 1 ? "" : "s"}`);
    }
    if (rest > 0 || hours === 0) {
      parts.push(`${String(rest)} minutes`);
    }
    return parts.join(" ");
  };
  const atRisk = minutes("atRisk");
  const attention = minutes("needsAttention");
  const unknown = levels.filter((l) => l === undefined).length;
  const parts: string[] = [];
  if (atRisk > 0) {
    parts.push(`at risk for ${span(atRisk)}`);
  }
  if (attention > 0) {
    parts.push(`needed attention for ${span(attention)}`);
  }
  let summary =
    parts.length === 0
      ? "Healthy throughout the last 24 hours"
      : `In the last 24 hours, ${parts.join(", and ")}`;
  if (unknown === levels.length) {
    return "Not looked at yet: the history starts once Changeport has looked at the cluster.";
  }
  if (unknown > 0) {
    summary += ", where Changeport looked";
  }
  return `${summary}.`;
}

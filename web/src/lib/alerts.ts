import type { Alert } from "@/api/client";

/** What each alert is about, in a word or two, for badges. */
const shortNames: Record<string, string> = {
  ConsumerGroupLagHigh: "Behind",
  ConsumerGroupStuck: "Stuck",
  ClientThrottled: "Throttled",
  TopicRequestErrors: "Failing requests",
  ClientRecordErrors: "Failing records",
};

/** When an alert started, for people. */
export function formatSince(startsAt: string): string {
  return new Date(startsAt).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

/** The alerts about a topic, or about a consumer group on a topic. */
export function alertsAbout(
  alerts: Alert[] | undefined,
  about: { topic: string; consumerGroup?: string },
): Alert[] {
  return (alerts ?? []).filter(
    (alert) =>
      alert.topic === about.topic &&
      (about.consumerGroup === undefined
        ? alert.consumerGroup === undefined
        : alert.consumerGroup === about.consumerGroup),
  );
}

/** What an alert is about, in a word or two, for badges. */
export function shortName(name: string): string {
  return shortNames[name] ?? "Alert";
}

import { describe, expect, test } from "bun:test";
import type { ConsumerGroupLag, Topic } from "@/api/client";
import { changeWarnings } from "@/lib/topicChange";

const hour = 60 * 60 * 1000;
const week = 7 * 24 * hour;

const orders: Topic = {
  name: "orders",
  partitions: 3,
  replicationFactor: 1,
  tiered: true,
  cleanupPolicy: "delete",
  retentionMs: week,
};

const reader = (group: string, lagSeconds: number, more: Partial<ConsumerGroupLag> = {}) => ({
  group,
  topic: "orders",
  lagSeconds,
  lagMessages: 10,
  capped: false,
  history: [],
  ...more,
});

const texts = (warnings: { text: string }[]) => warnings.map((w) => w.text);

describe("changeWarnings", () => {
  test("nothing changed, nothing to say", () => {
    expect(changeWarnings(orders, 3, week, [reader("billing", 7200)])).toEqual([]);
  });

  test("a longer retention deletes nothing", () => {
    expect(changeWarnings(orders, 3, 14 * 24 * hour, [reader("billing", 7200)])).toEqual([]);
  });

  test("groups further behind than a shorter retention lose messages", () => {
    const warnings = changeWarnings(orders, 3, hour, [
      reader("search", 40 * 60),
      reader("billing", 3 * 3600),
      reader("audit", 30 * 3600, { capped: true }),
      reader("caught-up", 9000, { lagMessages: 0 }),
      reader("elsewhere", 9000, { topic: "payments" }),
      reader("fresh", 10),
    ]);
    expect(warnings).toEqual([
      {
        loss: true,
        text: "audit is more than 30 hours behind. Keeping messages for 1 hour, orders deletes messages audit has not read yet.",
      },
      {
        loss: true,
        text: "billing is 3 hours behind. Keeping messages for 1 hour, orders deletes messages billing has not read yet.",
      },
      {
        loss: false,
        text: "search is 40 minutes behind, close to the 1 hour orders would keep messages. Unless it catches up, messages are deleted before it reads them.",
      },
      {
        loss: false,
        text: "Messages older than 1 hour are deleted for good within minutes of saving.",
      },
    ]);
  });

  test("a topic kept for ever gets a retention", () => {
    const forever: Topic = {
      name: "orders",
      partitions: 3,
      replicationFactor: 1,
      tiered: true,
      cleanupPolicy: "delete",
    };
    expect(texts(changeWarnings(forever, 3, week, []))).toEqual([
      "Messages older than 7 days are deleted for good within minutes of saving.",
    ]);
  });

  test("a compacted topic has no retention to warn about", () => {
    expect(
      changeWarnings({ ...orders, cleanupPolicy: "compact" }, 3, hour, [reader("billing", 7200)]),
    ).toEqual([]);
  });

  test("more partitions move keys", () => {
    const warnings = changeWarnings(orders, 6, week, []);
    expect(warnings.every((w) => !w.loss)).toBe(true);
    expect(texts(warnings)).toEqual([
      "Messages with the same key always go to the same partition. With more partitions, a key's new messages may go to another partition than its earlier ones, so for a while consumers may read them out of order.",
      "Consumers find the new partitions within 5 minutes (metadata.max.age.ms). Those with auto.offset.reset=latest, the Java client's default, skip the messages that reach a new partition before they do.",
      "Partitions can be added, never removed.",
    ]);
  });
});

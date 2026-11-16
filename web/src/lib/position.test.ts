import { describe, expect, test } from "bun:test";
import type { PositionPreview, TopicPosition } from "@/api/client";
import { formatDuration } from "@/lib/format";
import { listWords, replayName, tieredNote, toLocalInput, topicEffect } from "@/lib/position";

const orders: TopicPosition = {
  topic: "orders",
  partitions: 3,
  readAgain: 2100,
  skipped: 0,
  toRead: 2500,
  tieredMessages: 0,
};

describe("position", () => {
  test("a time for a datetime-local input is local, to the minute", () => {
    expect(toLocalInput(new Date(2026, 0, 5, 7, 3, 59))).toBe("2026-01-05T07:03");
  });

  test("a replay is named after its group and the day", () => {
    expect(replayName("billing", new Date(2026, 10, 16, 23, 59))).toBe("billing-replay-20261116");
  });

  test("a move says what it reads again and skips", () => {
    expect(topicEffect(orders, false)).toBe("reads 2,100 messages again");
    expect(topicEffect({ ...orders, readAgain: 0, skipped: 1 }, false)).toBe("skips 1 message");
    expect(topicEffect({ ...orders, readAgain: 0 }, false)).toBe("stays where it is");
    expect(topicEffect({ ...orders, readAgain: 5, skipped: 7 }, false)).toBe(
      "reads 5 messages again and skips 7 messages",
    );
    const from = topicEffect({ ...orders, from: "2026-10-03T14:05:00Z" }, false);
    expect(from).toStartWith("reads 2,100 messages again, from ");
  });

  test("a replay says what it reads", () => {
    expect(topicEffect(orders, true)).toBe("reads 2,500 messages");
    expect(topicEffect({ ...orders, toRead: 0 }, true)).toBe("only new messages");
  });

  test("messages from tiered storage come with how long they take at least", () => {
    const preview: PositionPreview = {
      group: "billing",
      members: [],
      topics: [
        { ...orders, tieredMessages: 1000 },
        { ...orders, topic: "payments", tieredMessages: 200 },
      ],
      tieredSeconds: 240,
    };
    expect(tieredNote(preview, formatDuration)).toBe(
      "1,200 messages are older than the brokers keep on disk, and come back from tiered storage, which takes at least 4 minutes.",
    );
    const unknown: PositionPreview = { group: preview.group, members: [], topics: preview.topics };
    expect(tieredNote(unknown, formatDuration)).toEndWith("from tiered storage.");
    expect(tieredNote({ ...preview, topics: [orders] }, formatDuration)).toBeNull();
  });

  test("lists read as a sentence", () => {
    expect(listWords([])).toBe("");
    expect(listWords(["a"])).toBe("a");
    expect(listWords(["a", "b", "c"])).toBe("a, b and c");
  });
});

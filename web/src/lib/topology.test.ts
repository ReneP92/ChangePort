import { describe, expect, test } from "bun:test";
import type { Topology } from "@/api/client";
import { groupID, layoutTopology, maxTopics, topicID, unreportedID } from "@/lib/topology";

const format = { rate: (b: number) => `${String(b)} B/s`, lag: (s: number) => `${String(s)}s` };

const topology: Topology = {
  producers: [
    {
      id: "User:admin/payments-api",
      clientId: "payments-api",
      principal: "User:admin",
      bytesPerSecond: 20,
    },
    {
      id: "User:admin/checkout",
      clientId: "checkout",
      principal: "User:admin",
      bytesPerSecond: 900,
    },
  ],
  topics: [
    {
      name: "orders",
      bytesInPerSecond: 900,
      bytesOutPerSecond: 800,
      unreportedBytesInPerSecond: 0,
    },
    {
      name: "payments",
      bytesInPerSecond: 500,
      bytesOutPerSecond: 0,
      unreportedBytesInPerSecond: 480,
    },
    { name: "idle", bytesInPerSecond: 0, bytesOutPerSecond: 0, unreportedBytesInPerSecond: 0 },
  ],
  groups: [
    { name: "billing", members: 2, lagSeconds: 300, lagMessages: 40 },
    { name: "fresh", members: 1 },
    { name: "gone", members: 0, lagSeconds: 3, lagMessages: 0 },
  ],
  writes: [
    { producer: "User:admin/checkout", topic: "orders", bytesPerSecond: 900 },
    { producer: "User:admin/payments-api", topic: "payments", bytesPerSecond: 20 },
  ],
  reads: [
    {
      group: "billing",
      topic: "orders",
      bytesPerSecond: 800,
      lagSeconds: 300,
      lagMessages: 40,
      capped: false,
    },
  ],
};

describe("layoutTopology", () => {
  test("places producers, topics and groups in columns, with edges between them", () => {
    const layout = layoutTopology(topology, 900, format);
    const column = (kind: string) =>
      layout.nodes
        .filter((node) => node.kind === kind)
        .map((node) => `${node.label}@${String(node.y)}`);
    // Producers and groups in the order of their topics, the busiest first.
    expect(column("topic")).toEqual(["orders@0", "payments@56"]);
    expect(column("producer")).toEqual(["checkout@0", "payments-api@56"]);
    expect(column("unreported")).toEqual(["Clients without telemetry@112"]);
    // A group with members shows before it commits; one without members
    // and offsets on no shown topic does not.
    expect(column("group")).toEqual(["billing@0", "fresh@56"]);
    expect(layout.nodes.find((node) => node.id === groupID("billing"))?.detail).toBe(
      "2 members, 300s",
    );
    expect(layout.nodes.find((node) => node.id === groupID("fresh"))?.detail).toBe(
      "1 member, no offsets yet",
    );
    expect(layout.edges.map((edge) => edge.id)).toEqual([
      `User:admin/checkout->${topicID("orders")}`,
      `User:admin/payments-api->${topicID("payments")}`,
      `${unreportedID}->${topicID("payments")}`,
      `${topicID("orders")}->${groupID("billing")}`,
    ]);
    expect(layout.idleTopics).toBe(1);
    expect(layout.moreTopics).toBe(0);
    // Three columns across the width, and rows down to the last node.
    const [producer, topic, group] = ["checkout", "orders", "billing"].map((label) =>
      layout.nodes.find((node) => node.label === label),
    );
    expect(producer?.x).toBe(0);
    expect((topic?.x ?? 0) + (topic?.width ?? 0) + 72).toBe(group?.x ?? -1);
    expect(layout.height).toBe(3 * 44 + 2 * 12);
  });

  test("shows the busiest topics up to its limit", () => {
    const many: Topology = {
      ...topology,
      topics: Array.from({ length: maxTopics + 5 }, (_, i) => ({
        name: `t${String(i)}`,
        bytesInPerSecond: i + 1,
        bytesOutPerSecond: 0,
        unreportedBytesInPerSecond: 0,
      })),
      writes: [],
      reads: [],
    };
    const layout = layoutTopology(many, 900, format);
    expect(layout.nodes.filter((node) => node.kind === "topic")).toHaveLength(maxTopics);
    expect(layout.nodes.some((node) => node.label === "t0")).toBe(false);
    expect(layout.moreTopics).toBe(5);
  });
});

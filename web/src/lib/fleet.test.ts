import { describe, expect, test } from "bun:test";
import type { FleetCluster } from "@/api/client";
import { brokersLine, countStates, formatShare, partitionProblems } from "@/lib/fleet";

const cluster = (more: Partial<FleetCluster> = {}): FleetCluster => ({
  customer: {} as FleetCluster["customer"],
  state: "healthy",
  reasons: [],
  brokers: 3,
  alerts: [],
  customerAlerts: 0,
  ...more,
});

describe("the fleet", () => {
  test("counts each state, the absent ones as 0", () => {
    const counts = countStates([
      cluster(),
      cluster({ state: "down" }),
      cluster(),
      cluster({ state: "starting" }),
    ]);
    expect(counts).toEqual({
      down: 1,
      degraded: 0,
      unknown: 0,
      starting: 1,
      deleting: 0,
      healthy: 2,
    });
  });

  test("says how many brokers are active, and which are fenced", () => {
    expect(brokersLine(cluster())).toBeUndefined();
    expect(brokersLine(cluster({ activeBrokers: 3, fencedBrokers: 0 }))).toBe("3 of 3");
    expect(brokersLine(cluster({ activeBrokers: 2, fencedBrokers: 1 }))).toBe("2 of 3, 1 fenced");
  });

  test("lists the partitions that are not in sync, the worst first", () => {
    expect(partitionProblems(cluster())).toBeUndefined();
    expect(
      partitionProblems(
        cluster({ offlinePartitions: 0, underMinIsrPartitions: 0, underReplicatedPartitions: 0 }),
      ),
    ).toEqual([]);
    expect(
      partitionProblems(
        cluster({ offlinePartitions: 2, underMinIsrPartitions: 0, underReplicatedPartitions: 12 }),
      ),
    ).toEqual([
      { text: "2 offline", serious: true },
      { text: "12 under-replicated", serious: false },
    ]);
  });

  test("shows a volume's share as a whole percentage", () => {
    expect(formatShare(0.8449)).toBe("84%");
    expect(formatShare(0)).toBe("0%");
  });
});

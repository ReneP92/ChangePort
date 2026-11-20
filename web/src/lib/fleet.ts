import type { FleetCluster } from "@/api/client";

export type FleetState = FleetCluster["state"];

/** The states, in the order the fleet view lists them: the worst first. */
export const fleetStates: FleetState[] = [
  "down",
  "degraded",
  "unknown",
  "starting",
  "deleting",
  "healthy",
];

/** How a state reads. */
export const fleetStateLabels: Record<FleetState, string> = {
  down: "Down",
  degraded: "Degraded",
  unknown: "Unknown",
  starting: "Starting",
  deleting: "Being removed",
  healthy: "Healthy",
};

/** How many clusters are in each state. */
export function countStates(clusters: FleetCluster[]): Record<FleetState, number> {
  const counts = Object.fromEntries(fleetStates.map((state) => [state, 0])) as Record<
    FleetState,
    number
  >;
  for (const cluster of clusters) {
    counts[cluster.state] += 1;
  }
  return counts;
}

/**
 * A cluster's brokers as the active controller counts them: "3 of 3", and
 * the fenced ones; undefined while the metrics store has none.
 */
export function brokersLine(cluster: FleetCluster): string | undefined {
  if (cluster.activeBrokers === undefined) {
    return undefined;
  }
  const line = `${String(cluster.activeBrokers)} of ${String(cluster.brokers)}`;
  return cluster.fencedBrokers ? `${line}, ${String(cluster.fencedBrokers)} fenced` : line;
}

/**
 * A cluster's partitions that are not as they should be, the worst first,
 * each with whether it stops clients; empty when all are in sync, and
 * undefined while the metrics store has none.
 */
export function partitionProblems(
  cluster: FleetCluster,
): { text: string; serious: boolean }[] | undefined {
  const { offlinePartitions, underMinIsrPartitions, underReplicatedPartitions } = cluster;
  if (
    offlinePartitions === undefined &&
    underMinIsrPartitions === undefined &&
    underReplicatedPartitions === undefined
  ) {
    return undefined;
  }
  const problems = [];
  if (offlinePartitions) {
    problems.push({ text: `${String(offlinePartitions)} offline`, serious: true });
  }
  if (underMinIsrPartitions) {
    problems.push({ text: `${String(underMinIsrPartitions)} under min ISR`, serious: true });
  }
  if (underReplicatedPartitions) {
    problems.push({
      text: `${String(underReplicatedPartitions)} under-replicated`,
      serious: false,
    });
  }
  return problems;
}

/** A share of a volume, as a whole percentage. */
export function formatShare(share: number): string {
  return `${String(Math.round(share * 100))}%`;
}

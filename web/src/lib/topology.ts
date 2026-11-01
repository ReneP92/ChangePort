import type { Topology } from "@/api/client";

/** What a node of the map stands for. */
export type NodeKind = "producer" | "unreported" | "topic" | "group";

/** A node of the map, placed: its column, and its box in pixels. */
export interface MapNode {
  id: string;
  kind: NodeKind;
  label: string;
  /** What the node's second line says, such as its traffic. */
  detail: string;
  x: number;
  y: number;
  width: number;
  /** The rate through the node, in bytes per second. */
  bytesPerSecond: number;
  /** A group's lag, in seconds, if known. */
  lagSeconds?: number;
}

/** An edge of the map, from a node's right side to another's left. */
export interface MapEdge {
  id: string;
  from: string;
  to: string;
  /** The bytes per second along it, if known. */
  bytesPerSecond?: number;
  /** A read's lag, in seconds. */
  lagSeconds?: number;
  path: string;
}

export interface MapLayout {
  nodes: MapNode[];
  edges: MapEdge[];
  width: number;
  height: number;
  /** How many topics without traffic or clients were left out. */
  idleTopics: number;
  /** How many topics with traffic were left out, past the map's limit. */
  moreTopics: number;
}

export const nodeHeight = 44;
const rowGap = 12;
const columnGap = 72;
/** The most topics the map shows, the busiest first. */
export const maxTopics = 40;
export const unreportedID = "unreported";

/** The id of a topic's or group's node, apart from producers'. */
export const topicID = (name: string) => `topic:${name}`;
export const groupID = (name: string) => `group:${name}`;

/**
 * Lays the topology out in three columns, producers, topics and consumer
 * groups, ordered so that edges cross little: the topics by traffic, the
 * producers and groups by where their topics are, then the topics again by
 * where their producers and groups are.
 */
export function layoutTopology(
  topology: Topology,
  width: number,
  format: { rate: (bytes: number) => string; lag: (seconds: number) => string },
): MapLayout {
  const columnWidth = (width - 2 * columnGap) / 3;
  const reads = topology.reads;
  const writes = topology.writes;
  const unreported = topology.topics.filter((t) => t.unreportedBytesInPerSecond > 0);

  // Topics with traffic or clients, the busiest first, up to the limit.
  const connected = new Set([...writes.map((w) => w.topic), ...reads.map((r) => r.topic)]);
  const active = topology.topics.filter(
    (t) => connected.has(t.name) || t.bytesInPerSecond > 0 || t.bytesOutPerSecond > 0,
  );
  active.sort(
    (a, b) =>
      b.bytesInPerSecond + b.bytesOutPerSecond - (a.bytesInPerSecond + a.bytesOutPerSecond) ||
      a.name.localeCompare(b.name),
  );
  const topics = active.slice(0, maxTopics);
  const shown = new Set(topics.map((t) => t.name));
  const position = (names: string[], order: string[]) => {
    const at = names.map((n) => order.indexOf(n)).filter((i) => i >= 0);
    return at.length === 0 ? Infinity : at.reduce((sum, i) => sum + i, 0) / at.length;
  };

  let topicOrder = topics.map((t) => t.name);
  const producerTopics = new Map<string, string[]>();
  for (const w of writes.filter((w) => shown.has(w.topic))) {
    producerTopics.set(w.producer, [...(producerTopics.get(w.producer) ?? []), w.topic]);
  }
  const groupTopics = new Map<string, string[]>();
  for (const r of reads.filter((r) => shown.has(r.topic))) {
    groupTopics.set(r.group, [...(groupTopics.get(r.group) ?? []), r.topic]);
  }
  const sortBy = (ids: string[], of: Map<string, string[]>) =>
    ids.sort(
      (a, b) =>
        position(of.get(a) ?? [], topicOrder) - position(of.get(b) ?? [], topicOrder) ||
        a.localeCompare(b),
    );
  const producers = sortBy(
    topology.producers.map((p) => p.id).filter((id) => producerTopics.has(id)),
    producerTopics,
  );
  // Groups with members show even before they commit, below the others.
  const groupByName = new Map(topology.groups.map((group) => [group.name, group]));
  const groups = sortBy(
    topology.groups
      .filter((group) => groupTopics.has(group.name) || group.members > 0)
      .map((group) => group.name),
    groupTopics,
  );
  // The topics again, by the average place of their producers and groups.
  const topicNeighbours = new Map<string, number[]>();
  for (const [producer, names] of producerTopics) {
    for (const name of names) {
      topicNeighbours.set(name, [
        ...(topicNeighbours.get(name) ?? []),
        producers.indexOf(producer),
      ]);
    }
  }
  for (const [group, names] of groupTopics) {
    for (const name of names) {
      topicNeighbours.set(name, [...(topicNeighbours.get(name) ?? []), groups.indexOf(group)]);
    }
  }
  const neighbourPosition = (name: string) => {
    const at = topicNeighbours.get(name) ?? [];
    return at.length === 0 ? Infinity : at.reduce((sum, i) => sum + i, 0) / at.length;
  };
  topicOrder = [...topicOrder].sort(
    (a, b) =>
      neighbourPosition(a) - neighbourPosition(b) || topicOrder.indexOf(a) - topicOrder.indexOf(b),
  );

  const nodes: MapNode[] = [];
  const place = (column: number, row: number) => ({
    x: column * (columnWidth + columnGap),
    y: row * (nodeHeight + rowGap),
    width: columnWidth,
  });
  producers.forEach((id, row) => {
    const p = topology.producers.find((producer) => producer.id === id);
    if (!p) {
      return;
    }
    nodes.push({
      id,
      kind: "producer",
      label: p.clientId || "(no client ID)",
      detail: `${p.principal.replace(/^User:/, "")}, ${format.rate(p.bytesPerSecond)}`,
      bytesPerSecond: p.bytesPerSecond,
      ...place(0, row),
    });
  });
  const shownUnreported = unreported.filter((t) => shown.has(t.name));
  if (shownUnreported.length > 0) {
    const total = shownUnreported.reduce((sum, t) => sum + t.unreportedBytesInPerSecond, 0);
    nodes.push({
      id: unreportedID,
      kind: "unreported",
      label: "Clients without telemetry",
      detail: format.rate(total),
      bytesPerSecond: total,
      ...place(0, producers.length),
    });
  }
  topicOrder.forEach((name, row) => {
    const t = topics.find((topic) => topic.name === name);
    if (!t) {
      return;
    }
    nodes.push({
      id: topicID(name),
      kind: "topic",
      label: name,
      detail: `${format.rate(t.bytesInPerSecond)} in, ${format.rate(t.bytesOutPerSecond)} out`,
      bytesPerSecond: t.bytesInPerSecond,
      ...place(1, row),
    });
  });
  groups.forEach((name, row) => {
    const group = groupByName.get(name) ?? { name, members: 0 };
    const lag = group.lagSeconds;
    const members = `${String(group.members)} member${group.members === 1 ? "" : "s"}`;
    nodes.push({
      id: groupID(name),
      kind: "group",
      label: name,
      detail: lag === undefined ? `${members}, no offsets yet` : `${members}, ${format.lag(lag)}`,
      bytesPerSecond: 0,
      ...(lag === undefined ? {} : { lagSeconds: lag }),
      ...place(2, row),
    });
  });

  const byID = new Map(nodes.map((node) => [node.id, node]));
  const path = (from: MapNode, to: MapNode) => {
    const x1 = from.x + from.width;
    const y1 = from.y + nodeHeight / 2;
    const x2 = to.x;
    const y2 = to.y + nodeHeight / 2;
    const mid = (x1 + x2) / 2;
    return `M${x1.toFixed(1)},${y1.toFixed(1)} C${mid.toFixed(1)},${y1.toFixed(1)} ${mid.toFixed(1)},${y2.toFixed(1)} ${x2.toFixed(1)},${y2.toFixed(1)}`;
  };
  const edges: MapEdge[] = [];
  const edge = (from: string, to: string, extra: Partial<MapEdge>) => {
    const a = byID.get(from);
    const b = byID.get(to);
    if (a && b) {
      edges.push({ id: `${from}->${to}`, from, to, path: path(a, b), ...extra });
    }
  };
  for (const w of writes) {
    edge(w.producer, topicID(w.topic), { bytesPerSecond: w.bytesPerSecond });
  }
  for (const t of shownUnreported) {
    edge(unreportedID, topicID(t.name), { bytesPerSecond: t.unreportedBytesInPerSecond });
  }
  for (const r of reads) {
    edge(topicID(r.topic), groupID(r.group), {
      ...(r.bytesPerSecond === undefined ? {} : { bytesPerSecond: r.bytesPerSecond }),
      lagSeconds: r.lagSeconds,
    });
  }
  const rows = Math.max(
    producers.length + (shownUnreported.length > 0 ? 1 : 0),
    topicOrder.length,
    groups.length,
    1,
  );
  return {
    nodes,
    edges,
    width,
    height: rows * nodeHeight + (rows - 1) * rowGap,
    idleTopics: topology.topics.length - active.length,
    moreTopics: active.length - topics.length,
  };
}

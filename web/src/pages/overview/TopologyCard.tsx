import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { useAlerts, useTopology } from "@/api/queries";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { formatDuration, formatRate } from "@/lib/format";
import {
  groupID,
  layoutTopology,
  nodeHeight,
  type MapEdge,
  type MapLayout,
  type MapNode,
} from "@/lib/topology";
import { cn } from "@/lib/utils";
import { useEnvironmentPath } from "@/lib/useEnvironment";

/** The narrowest the map draws; on phones it scrolls sideways within the card. */
const minWidth = 640;

const lagText = (seconds: number) =>
  seconds < 60 ? "caught up" : `${formatDuration(seconds)} behind`;

/**
 * The live topology map: producers, the topics they write and the consumer
 * groups that read them, with the bytes flowing between them and how far
 * behind each group is. Pointing at a node picks out its edges.
 */
export function TopologyCard() {
  const topology = useTopology();
  const alerts = useAlerts();
  const box = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  const [focused, setFocused] = useState<string | null>(null);
  useEffect(() => {
    const element = box.current;
    if (!element) {
      return;
    }
    const observer = new ResizeObserver(([entry]) => {
      setWidth(Math.max(minWidth, Math.floor(entry?.contentRect.width ?? 0)));
    });
    observer.observe(element);
    return () => {
      observer.disconnect();
    };
  }, []);
  const layout =
    topology.data && width > 0
      ? layoutTopology(topology.data, width, { rate: formatRate, lag: lagText })
      : undefined;
  const alerted = new Set(
    (alerts.data ?? []).flatMap((alert) =>
      alert.consumerGroup === undefined ? [] : [groupID(alert.consumerGroup)],
    ),
  );
  return (
    // The getting started guide links here.
    <Card id="data-flow" className="scroll-mt-6">
      <CardHeader>
        <CardTitle>
          <h2>Data flow</h2>
        </CardTitle>
        <CardDescription>
          Producers, the topics they write and the consumer groups that read them, over the last 5
          minutes. Producers show once they report their own metrics, as clients of Kafka 3.7 and
          newer do.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        {topology.isPending && <p className="text-muted-foreground">Loading the map</p>}
        {topology.isError && <p className="text-destructive">{topology.error.message}</p>}
        <div ref={box} className="relative -mx-1 overflow-x-auto px-1">
          {layout && layout.nodes.length === 0 && (
            <p className="text-muted-foreground">
              Nothing flows yet. Once clients produce and consume, they show here.
            </p>
          )}
          {layout && layout.nodes.length > 0 && (
            <TopologyMap layout={layout} focused={focused} alerted={alerted} onFocus={setFocused} />
          )}
        </div>
        {layout && (layout.idleTopics > 0 || layout.moreTopics > 0) && (
          <p className="text-xs text-muted-foreground">
            {[
              layout.moreTopics > 0 &&
                `${String(layout.moreTopics)} quieter topic${layout.moreTopics === 1 ? "" : "s"} left out`,
              layout.idleTopics > 0 &&
                `${String(layout.idleTopics)} topic${layout.idleTopics === 1 ? "" : "s"} without traffic left out`,
            ]
              .filter(Boolean)
              .join("; ")}
            .
          </p>
        )}
      </CardContent>
    </Card>
  );
}

function TopologyMap({
  layout,
  focused,
  alerted,
  onFocus,
}: {
  layout: MapLayout;
  focused: string | null;
  alerted: Set<string>;
  onFocus: (id: string | null) => void;
}) {
  const maxRate = Math.max(...layout.edges.map((edge) => edge.bytesPerSecond ?? 0), 1);
  const near = (edge: MapEdge) => focused === null || edge.from === focused || edge.to === focused;
  const linked = new Set(
    layout.edges
      .filter((edge) => focused !== null && near(edge))
      .flatMap((edge) => [edge.from, edge.to]),
  );
  return (
    <div
      role="group"
      aria-label="Topology map"
      className="relative"
      style={{ width: layout.width, height: layout.height }}
    >
      <svg
        aria-hidden
        className="absolute inset-0 overflow-visible"
        width={layout.width}
        height={layout.height}
      >
        {layout.edges.map((edge) => {
          const rate = edge.bytesPerSecond ?? 0;
          const share = Math.log10(1 + rate) / Math.log10(1 + maxRate);
          const behind = (edge.lagSeconds ?? 0) >= 60;
          return (
            <path
              key={edge.id}
              d={edge.path}
              fill="none"
              strokeLinecap="round"
              className={cn(
                "transition-opacity",
                behind ? "stroke-amber-500" : "stroke-[var(--chart-1)]",
                rate > 0 && "motion-safe:animate-flow",
                !near(edge) && "opacity-15",
              )}
              strokeWidth={1.5 + 3.5 * share}
              strokeDasharray={rate > 0 ? "6 6" : "2 6"}
              style={
                rate > 0 ? { animationDuration: `${(2.4 - 1.6 * share).toFixed(2)}s` } : undefined
              }
            />
          );
        })}
      </svg>
      {layout.nodes.map((node) => (
        <MapNodeBox
          key={node.id}
          node={node}
          dim={focused !== null && node.id !== focused && !linked.has(node.id)}
          alerted={alerted.has(node.id)}
          onFocus={onFocus}
        />
      ))}
    </div>
  );
}

function MapNodeBox({
  node,
  dim,
  alerted,
  onFocus,
}: {
  node: MapNode;
  dim: boolean;
  alerted: boolean;
  onFocus: (id: string | null) => void;
}) {
  const envPath = useEnvironmentPath();
  const behind = (node.lagSeconds ?? 0) >= 60;
  const className = cn(
    "absolute flex flex-col justify-center rounded-lg border bg-card px-2.5 text-left transition-opacity",
    node.kind === "unreported" && "border-dashed",
    node.kind === "group" && behind && "border-amber-500/60",
    alerted && "border-destructive/60 bg-destructive/5",
    dim && "opacity-40",
  );
  const style = { left: node.x, top: node.y, width: node.width, height: nodeHeight };
  const events = {
    onMouseEnter: () => {
      onFocus(node.id);
    },
    onMouseLeave: () => {
      onFocus(null);
    },
    onFocus: () => {
      onFocus(node.id);
    },
    onBlur: () => {
      onFocus(null);
    },
  };
  const content = (
    <>
      <span
        className={cn(
          "truncate text-xs font-medium",
          node.kind === "unreported" && "text-muted-foreground",
        )}
      >
        {node.label}
      </span>
      <span
        className={cn(
          "truncate text-[0.7rem] text-muted-foreground",
          node.kind === "group" && behind && "text-amber-600 dark:text-amber-500",
          alerted && "text-destructive",
        )}
      >
        {node.detail}
      </span>
    </>
  );
  const title = `${node.label}: ${node.detail}`;
  if (node.kind === "topic" || node.kind === "group") {
    const path =
      node.kind === "topic"
        ? envPath(`/topics/${encodeURIComponent(node.label)}`)
        : envPath(`/consumer-groups/${encodeURIComponent(node.label)}`);
    return (
      <Link
        to={path}
        title={title}
        className={cn(className, "hover:bg-muted/60")}
        style={style}
        {...events}
      >
        {content}
      </Link>
    );
  }
  return (
    <div
      title={title}
      className={className}
      style={style}
      onMouseEnter={events.onMouseEnter}
      onMouseLeave={events.onMouseLeave}
    >
      {content}
    </div>
  );
}

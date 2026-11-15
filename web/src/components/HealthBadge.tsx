import {
  ArrowDownWideNarrow,
  CircleAlert,
  CircleCheck,
  TriangleAlert,
  type LucideIcon,
} from "lucide-react";
import type { HealthStatus } from "@/api/client";
import { healthLabels } from "@/lib/health";
import { cn } from "@/lib/utils";

const looks: Record<HealthStatus["level"], { icon: LucideIcon; className: string }> = {
  healthy: { icon: CircleCheck, className: "text-emerald-700 dark:text-emerald-500" },
  needsAttention: { icon: TriangleAlert, className: "text-amber-700 dark:text-amber-500" },
  atRisk: { icon: CircleAlert, className: "text-destructive" },
};

/**
 * A topic's or a consumer group's health, as an icon and words, never color
 * alone; nothing while it is not known.
 */
export function HealthBadge({
  health,
  className,
}: {
  health: HealthStatus | undefined;
  className?: string;
}) {
  if (!health) {
    return null;
  }
  const look = looks[health.level];
  const Icon = look.icon;
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-sm", look.className, className)}>
      <Icon aria-hidden className="size-3.5 shrink-0" />
      {healthLabels[health.level]}
    </span>
  );
}

/**
 * A table's Health column header, which sorts the rows worst first and back
 * to their own order.
 */
export function HealthHeader({ sorted, onToggle }: { sorted: boolean; onToggle: () => void }) {
  return (
    <th className="py-2 font-medium" aria-sort={sorted ? "descending" : "none"}>
      <button
        type="button"
        onClick={onToggle}
        className="inline-flex items-center gap-1 rounded-sm hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
      >
        Health
        <ArrowDownWideNarrow
          aria-hidden
          className={cn("size-3.5", sorted ? "text-foreground" : "opacity-40")}
        />
        <span className="sr-only">{sorted ? ", worst first" : ", sort worst first"}</span>
      </button>
    </th>
  );
}

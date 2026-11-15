import { cn } from "@/lib/utils";

/**
 * Changeport's mark (docs/brand): a change log of three rows, the newest
 * change in teal. Its rows are ink on light pages and white on dark ones,
 * as the brand's dark logo has them.
 */
export function ChangeportMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 64 64" aria-hidden className={cn("shrink-0", className)}>
      <g className="fill-[#0B1F33] dark:fill-white">
        <rect x="8" y="12" width="36" height="10" rx="5" />
        <rect x="8" y="27" width="24" height="10" rx="5" />
        <rect x="8" y="42" width="48" height="10" rx="5" />
      </g>
      <rect x="37" y="27" width="10" height="10" rx="5" className="fill-[#0D9488]" />
    </svg>
  );
}

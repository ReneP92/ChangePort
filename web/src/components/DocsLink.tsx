import { ArrowUpRight } from "lucide-react";
import type { ReactNode } from "react";
import { useDocsUrl } from "@/api/config";
import { cn } from "@/lib/utils";

/**
 * A link to a page of the customer docs, such as
 * troubleshooting/consumer-too-slow.html, in a new tab so that what the
 * console shows, such as a dialog half filled in, stays. Nothing when the
 * console names no docs.
 */
export function DocsLink({
  page,
  children,
  className,
}: {
  page: string | undefined;
  children: ReactNode;
  className?: string;
}) {
  const docsUrl = useDocsUrl();
  if (!docsUrl || !page) {
    return null;
  }
  return (
    <a
      href={`${docsUrl}/${page}`}
      target="_blank"
      rel="noreferrer"
      className={cn(
        "inline-flex items-center gap-0.5 underline-offset-4 hover:text-foreground hover:underline",
        className,
      )}
    >
      {children}
      <span className="sr-only"> (opens the docs in a new tab)</span>
      <ArrowUpRight aria-hidden className="size-3.5 shrink-0" />
    </a>
  );
}

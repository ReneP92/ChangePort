import type { ReactNode } from "react";
import { Link } from "react-router";
import { cn } from "@/lib/utils";

/** A step of a page's breadcrumbs: a page above it, which it links to. */
export interface Crumb {
  label: string;
  to: string;
}

/**
 * A page of the console, after Linear's: a thin bar that names it, under
 * the pages above it, with its actions on the right; then what it says
 * about itself, and its content, as wide as the window.
 */
export function Page({
  title,
  crumbs = [],
  badge,
  actions,
  description,
  className,
  children,
}: {
  title: ReactNode;
  crumbs?: Crumb[];
  /** Beside the title, such as a cluster's state. */
  badge?: ReactNode;
  actions?: ReactNode;
  description?: ReactNode;
  className?: string;
  children?: ReactNode;
}) {
  return (
    <main className="flex min-w-0 flex-col">
      <header className="flex min-h-12 flex-wrap items-center gap-x-3 gap-y-1 border-b px-4 py-2 sm:px-6">
        <div className="flex min-w-0 items-center gap-1.5 text-sm">
          {crumbs.length > 0 && (
            <nav aria-label="Breadcrumb" className="flex min-w-0 items-center gap-1.5">
              {crumbs.map((crumb, index) => (
                <span key={index} className="flex min-w-0 items-center gap-1.5">
                  <Link
                    to={crumb.to}
                    className="truncate text-muted-foreground hover:text-foreground"
                  >
                    {crumb.label}
                  </Link>
                  <span aria-hidden className="text-muted-foreground/60">
                    /
                  </span>
                </span>
              ))}
            </nav>
          )}
          <h1 className="truncate font-medium">{title}</h1>
        </div>
        {badge}
        {actions && <div className="ml-auto flex items-center gap-2">{actions}</div>}
      </header>
      <div className={cn("flex flex-col gap-4 p-4 sm:p-6", className)}>
        {description && <p className="max-w-3xl text-sm text-muted-foreground">{description}</p>}
        {children}
      </div>
    </main>
  );
}

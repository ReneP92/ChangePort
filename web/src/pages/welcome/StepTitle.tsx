import type { ReactNode } from "react";

/** The heading of a step of the welcome page. */
export function StepTitle({ children }: { children: ReactNode }) {
  return <h2 className="text-lg font-semibold tracking-tight">{children}</h2>;
}

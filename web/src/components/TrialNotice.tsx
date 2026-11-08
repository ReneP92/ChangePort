import type { CustomerTrial } from "@/api/client";
import { formatDay, trialEnding, trialLeft } from "@/lib/trials";
import { cn } from "@/lib/utils";

/**
 * A strip under the header of every page of a customer on a trial: when it
 * ends, and in its last three days, that the cluster then goes.
 */
export function TrialNotice({ trial, now = new Date() }: { trial: CustomerTrial; now?: Date }) {
  const ending = trialEnding(trial.endsAt, now);
  return (
    <div
      role="status"
      className={cn(
        "border-b text-sm",
        ending ? "bg-destructive/10 text-destructive" : "bg-muted/50 text-muted-foreground",
      )}
    >
      <p className="mx-auto max-w-5xl px-6 py-2">
        {ending ? (
          <>
            <span className="font-medium">
              Your trial ends in {trialLeft(trial.endsAt, now)}, on{" "}
              {formatDay(new Date(trial.endsAt))}.
            </span>{" "}
            Your cluster and its data are then deleted, unless Changeport extends the trial or you
            become a customer.
          </>
        ) : (
          <>
            Trial: {trialLeft(trial.endsAt, now)} left, until {formatDay(new Date(trial.endsAt))}.
          </>
        )}
      </p>
    </div>
  );
}

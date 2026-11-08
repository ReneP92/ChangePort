import { useState } from "react";
import { errorText, type Customer, type CustomerTrial, type TrialChange } from "@/api/client";
import { useChangeTrial } from "@/api/queries";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { addDays, endOn, formatDay, localToday } from "@/lib/trials";

/** A button that opens a dialog to change a customer's trial, the form inside. */
function TrialDialog({
  label,
  variant,
  customer,
  form,
}: {
  label: string;
  variant: "default" | "outline";
  customer: Customer;
  form: (props: {
    pending: boolean;
    error: Error | null;
    onChange: (change: TrialChange) => void;
  }) => React.ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const change = useChangeTrial();
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        change.reset();
      }}
    >
      <DialogTrigger asChild>
        <Button variant={variant}>{label}</Button>
      </DialogTrigger>
      <DialogContent>
        {/* Mounted per opening, so that every opening starts afresh. */}
        {open &&
          form({
            pending: change.isPending,
            error: change.error,
            onChange: (trialChange) => {
              change.mutate(
                { id: customer.id, change: trialChange },
                {
                  onSuccess: () => {
                    setOpen(false);
                  },
                },
              );
            },
          })}
      </DialogContent>
    </Dialog>
  );
}

/** A button that opens the dialog to move a trial's end. */
export function ExtendTrialDialog({
  customer,
  trial,
}: {
  customer: Customer;
  trial: CustomerTrial;
}) {
  return (
    <TrialDialog
      label="Extend trial"
      variant="outline"
      customer={customer}
      form={(props) => <ExtendTrialForm customer={customer} trial={trial} {...props} />}
    />
  );
}

/** A button that opens the dialog to convert a customer, ending its trial. */
export function ConvertTrialDialog({ customer }: { customer: Customer }) {
  return (
    <TrialDialog
      label="Convert"
      variant="default"
      customer={customer}
      form={(props) => <ConvertTrialForm customer={customer} {...props} />}
    />
  );
}

/**
 * The new end of a trial: a day from tomorrow to 90 days from now, a week
 * after the current end by default, at the time of day it ends now.
 */
export function ExtendTrialForm({
  customer,
  trial,
  pending,
  error,
  onChange,
  today = localToday(),
}: {
  customer: Customer;
  trial: CustomerTrial;
  pending: boolean;
  error: Error | null;
  onChange: (change: TrialChange) => void;
  today?: string;
}) {
  const first = addDays(today, 1);
  const last = addDays(today, 89);
  const current = localToday(new Date(trial.endsAt));
  const proposed = addDays(current < today ? today : current, 7);
  const [day, setDay] = useState(proposed > last ? last : proposed);
  const outside = day === "" || day < first || day > last;
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        if (!outside) {
          onChange({ action: "extend", endsAt: endOn(day, trial.endsAt) });
        }
      }}
      className="flex flex-col gap-5"
    >
      <DialogHeader>
        <DialogTitle>Extend {customer.displayName}&apos;s trial</DialogTitle>
        <DialogDescription>
          It ends on {formatDay(new Date(trial.endsAt))} now. The customer is warned again three
          days before the new end.
        </DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-2">
        <Label htmlFor="trial-end">New end</Label>
        <Input
          id="trial-end"
          type="date"
          className="w-fit"
          min={first}
          max={last}
          value={day}
          aria-invalid={outside ? true : undefined}
          onChange={(event) => {
            setDay(event.target.value);
          }}
        />
        {outside && (
          <p className="text-sm text-destructive">
            Choose a day from tomorrow to {formatDay(last)}.
          </p>
        )}
      </div>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {errorText(error)}
        </p>
      )}
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">
            Cancel
          </Button>
        </DialogClose>
        <Button type="submit" disabled={outside || pending}>
          {pending ? "Extending" : "Extend"}
        </Button>
      </DialogFooter>
    </form>
  );
}

/** Converting: the trial ends, and the customer stays. */
export function ConvertTrialForm({
  customer,
  pending,
  error,
  onChange,
}: {
  customer: Customer;
  pending: boolean;
  error: Error | null;
  onChange: (change: TrialChange) => void;
}) {
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        onChange({ action: "convert" });
      }}
      className="flex flex-col gap-5"
    >
      <DialogHeader>
        <DialogTitle>Convert {customer.displayName}?</DialogTitle>
        <DialogDescription>
          Its trial ends now, so its cluster stays instead of being deprovisioned at the
          trial&apos;s end. Deletion protection can then be turned on with provisioner protect.
        </DialogDescription>
      </DialogHeader>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {errorText(error)}
        </p>
      )}
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">
            Cancel
          </Button>
        </DialogClose>
        <Button type="submit" disabled={pending}>
          {pending ? "Converting" : "Convert"}
        </Button>
      </DialogFooter>
    </form>
  );
}

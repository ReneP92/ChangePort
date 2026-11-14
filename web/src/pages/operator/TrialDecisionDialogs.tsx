import { useState } from "react";
import { errorText, type TrialRequest } from "@/api/client";
import { useApproveTrialRequest, useDeclineTrialRequest } from "@/api/queries";
import { ProfileRadio } from "@/components/ProfileRadio";
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
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { type Profile } from "@/lib/profiles";
import { characters } from "@/lib/trials";

/** A button that opens the dialog to approve a pending trial request. */
export function ApproveTrialDialog({ request }: { request: TrialRequest }) {
  const [open, setOpen] = useState(false);
  const approve = useApproveTrialRequest();
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        approve.reset();
      }}
    >
      <DialogTrigger asChild>
        <Button size="sm">Approve</Button>
      </DialogTrigger>
      <DialogContent>
        {/* Mounted per opening, so that every opening starts afresh. */}
        {open && (
          <ApproveTrialForm
            request={request}
            pending={approve.isPending}
            error={approve.error}
            onApprove={(profile) => {
              approve.mutate(
                { id: request.id, approval: { profile } },
                {
                  onSuccess: () => {
                    setOpen(false);
                  },
                },
              );
            }}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

/** The approval: which profile the requester's cluster gets, dev by default. */
export function ApproveTrialForm({
  request,
  pending,
  error,
  onApprove,
}: {
  request: TrialRequest;
  pending: boolean;
  error?: Error | null;
  onApprove: (profile: Profile) => void;
}) {
  const [profile, setProfile] = useState<Profile>("dev");
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        onApprove(profile);
      }}
      className="flex flex-col gap-5"
    >
      <DialogHeader>
        <DialogTitle>Approve {request.company}&apos;s trial?</DialogTitle>
        <DialogDescription>
          Changeport creates the organization {request.company}, makes {request.email} its admin at
          once, and provisions a dedicated Kafka cluster without deletion protection.
        </DialogDescription>
      </DialogHeader>
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-2 text-sm font-medium">Profile</legend>
        <ProfileRadio id="approve-profile" value={profile} onChange={setProfile} />
      </fieldset>
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
          {pending ? "Approving" : "Approve"}
        </Button>
      </DialogFooter>
    </form>
  );
}

/** A button that opens the dialog to decline a pending trial request. */
export function DeclineTrialDialog({ request }: { request: TrialRequest }) {
  const [open, setOpen] = useState(false);
  const decline = useDeclineTrialRequest();
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        decline.reset();
      }}
    >
      <DialogTrigger asChild>
        <Button size="sm" variant="outline">
          Decline
        </Button>
      </DialogTrigger>
      <DialogContent>
        {open && (
          <DeclineTrialForm
            request={request}
            pending={decline.isPending}
            error={decline.error}
            onDecline={(reason) => {
              decline.mutate(
                { id: request.id, reason },
                {
                  onSuccess: () => {
                    setOpen(false);
                  },
                },
              );
            }}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

/** The decline: a reason, which the requester sees, is required. */
export function DeclineTrialForm({
  request,
  pending,
  error,
  onDecline,
}: {
  request: TrialRequest;
  pending: boolean;
  error?: Error | null;
  onDecline: (reason: string) => void;
}) {
  const [reason, setReason] = useState("");
  const length = characters(reason.trim());
  const tooLong = length > 1000;
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        if (length > 0 && !tooLong) {
          onDecline(reason.trim());
        }
      }}
      className="flex flex-col gap-5"
    >
      <DialogHeader>
        <DialogTitle>Decline {request.company}&apos;s request?</DialogTitle>
        <DialogDescription>
          {request.email} sees your reason, and may ask again in a week.
        </DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-2">
        <Label htmlFor="decline-reason">Reason</Label>
        <Textarea
          id="decline-reason"
          rows={4}
          value={reason}
          aria-invalid={tooLong ? true : undefined}
          onChange={(event) => {
            setReason(event.target.value);
          }}
        />
        {tooLong && <p className="text-sm text-destructive">Use at most 1,000 characters.</p>}
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
        <Button type="submit" variant="destructive" disabled={length === 0 || tooLong || pending}>
          {pending ? "Declining" : "Decline"}
        </Button>
      </DialogFooter>
    </form>
  );
}

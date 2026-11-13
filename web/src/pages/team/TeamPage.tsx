import { Trash2 } from "lucide-react";
import { useState } from "react";
import { ApiError, errorText, type Customer, type Team, type TeamRole } from "@/api/client";
import {
  useChangeTeammateRole,
  useInviteTeammate,
  useRemoveTeammate,
  useRevokeInvitation,
  useTeam,
} from "@/api/queries";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { CustomerArea } from "@/pages/CustomerArea";

const roleNames: Record<TeamRole, string> = { admin: "Admin", member: "Member" };

/**
 * The people of the customer's organization, and for admins, who joins:
 * a person belongs to one organization, so an address that belongs, or is
 * invited, to another is refused.
 */
export function TeamPage() {
  return (
    <CustomerArea>
      {(customer, admin) => <TeamContent customer={customer} admin={admin} />}
    </CustomerArea>
  );
}

function TeamContent({ customer, admin }: { customer: Customer; admin: boolean }) {
  const team = useTeam();
  return (
    <main className="mx-auto flex max-w-5xl flex-col gap-6 p-6">
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">Team</h1>
        <p className="text-sm text-muted-foreground">
          Who at {customer.displayName} uses Changeport. Admins manage the cluster, its credentials
          and the team; members use the cluster. A person belongs to one organization.
        </p>
      </header>
      {team.isPending && <p className="text-sm text-muted-foreground">Loading the team</p>}
      {team.isError && <p className="text-sm text-destructive">{team.error.message}</p>}
      {team.data && (
        <>
          <MembersCard team={team.data} admin={admin} organization={customer.displayName} />
          <InvitationsCard team={team.data} admin={admin} />
        </>
      )}
    </main>
  );
}

function MembersCard({
  team,
  admin,
  organization,
}: {
  team: Team;
  admin: boolean;
  organization: string;
}) {
  const changeRole = useChangeTeammateRole();
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <h2>Members</h2>
        </CardTitle>
        <CardDescription>
          {team.members.length === 1 ? "1 person" : `${String(team.members.length)} people`}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        {changeRole.error && (
          <p role="alert" className="text-destructive">
            {errorText(changeRole.error)}
          </p>
        )}
        <ul aria-label="Members" className="flex flex-col divide-y">
          {team.members.map((member) => (
            <li
              key={member.userID}
              className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 py-3 first:pt-0 last:pb-0"
            >
              <div className="flex min-w-0 flex-col">
                <span className="flex items-center gap-2 font-medium">
                  <span className="truncate">{member.name ?? member.email}</span>
                  {member.you && <Badge variant="secondary">You</Badge>}
                </span>
                {member.name && (
                  <span className="truncate text-muted-foreground">{member.email}</span>
                )}
              </div>
              <div className="flex items-center gap-2">
                {admin ? (
                  <Select
                    value={member.role}
                    disabled={changeRole.isPending}
                    onValueChange={(role) => {
                      changeRole.mutate({ userID: member.userID, role: role as TeamRole });
                    }}
                  >
                    <SelectTrigger
                      className="w-28"
                      aria-label={`Role of ${member.name ?? member.email}`}
                    >
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="admin">Admin</SelectItem>
                      <SelectItem value="member">Member</SelectItem>
                    </SelectContent>
                  </Select>
                ) : (
                  <Badge variant={member.role === "admin" ? "default" : "outline"}>
                    {roleNames[member.role]}
                  </Badge>
                )}
                {admin && (
                  <RemoveDialog
                    userID={member.userID}
                    who={member.name ?? member.email}
                    you={member.you}
                    organization={organization}
                  />
                )}
              </div>
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  );
}

function RemoveDialog({
  userID,
  who,
  you,
  organization,
}: {
  userID: string;
  who: string;
  you: boolean;
  organization: string;
}) {
  const [open, setOpen] = useState(false);
  const remove = useRemoveTeammate();
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        remove.reset();
      }}
    >
      <DialogTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={`Remove ${who}`}>
          <Trash2 />
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {you ? `Leave ${organization}?` : `Remove ${who} from ${organization}?`}
          </DialogTitle>
          <DialogDescription>
            {you
              ? "You lose access to its cluster at once."
              : "They lose access to its cluster at once. Their apps keep working: they connect with the cluster's credentials, not as a person."}
          </DialogDescription>
        </DialogHeader>
        {remove.error && (
          <p role="alert" className="text-sm text-destructive">
            {errorText(remove.error)}
          </p>
        )}
        <DialogFooter>
          <DialogClose asChild>
            <Button type="button" variant="outline">
              Cancel
            </Button>
          </DialogClose>
          <Button
            variant="destructive"
            disabled={remove.isPending}
            onClick={() => {
              remove.mutate(userID, {
                onSuccess: () => {
                  setOpen(false);
                  if (you) {
                    window.location.assign("/");
                  }
                },
              });
            }}
          >
            {remove.isPending ? "Removing" : you ? "Leave" : "Remove"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function InvitationsCard({ team, admin }: { team: Team; admin: boolean }) {
  const revoke = useRevokeInvitation();
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <h2>Invitations</h2>
        </CardTitle>
        <CardDescription>
          {admin
            ? "Invite colleagues by email. They join once they accept, signing up if they are new."
            : "Ask an admin of your organization to invite a colleague."}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-5 text-sm">
        {admin && <InviteForm />}
        {team.invitations.length === 0 ? (
          <p className="text-muted-foreground">No invitation is waiting.</p>
        ) : (
          <ul aria-label="Pending invitations" className="flex flex-col divide-y">
            {team.invitations.map((invitation) => (
              <li
                key={invitation.id}
                className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 py-3 first:pt-0 last:pb-0"
              >
                <span className="flex min-w-0 items-center gap-2">
                  <span className="truncate font-medium">{invitation.email}</span>
                  <Badge variant="outline">{roleNames[invitation.role]}</Badge>
                </span>
                {admin && (
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={revoke.isPending}
                    onClick={() => {
                      revoke.mutate(invitation.id);
                    }}
                  >
                    Revoke
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
        {revoke.error && (
          <p role="alert" className="text-destructive">
            {errorText(revoke.error)}
          </p>
        )}
      </CardContent>
    </Card>
  );
}

function InviteForm() {
  const invite = useInviteTeammate();
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<TeamRole>("member");
  const problem = invite.error instanceof ApiError ? invite.error.problem : undefined;
  return (
    <form
      className="flex flex-col gap-3"
      noValidate
      onSubmit={(event) => {
        event.preventDefault();
        invite.mutate(
          { email: email.trim(), role },
          {
            onSuccess: () => {
              setEmail("");
            },
          },
        );
      }}
    >
      <div className="flex flex-wrap items-end gap-3">
        <div className="flex min-w-60 flex-1 flex-col gap-2">
          <Label htmlFor="invite-email">Email address</Label>
          <Input
            id="invite-email"
            type="email"
            autoComplete="off"
            placeholder="grace@example.com"
            value={email}
            aria-invalid={problem?.field === "email"}
            onChange={(event) => {
              setEmail(event.target.value);
            }}
          />
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="invite-role">Role</Label>
          <Select
            value={role}
            onValueChange={(value) => {
              setRole(value as TeamRole);
            }}
          >
            <SelectTrigger id="invite-role" className="w-28">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="member">Member</SelectItem>
              <SelectItem value="admin">Admin</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <Button type="submit" disabled={invite.isPending || email.trim() === ""}>
          {invite.isPending ? "Inviting" : "Invite"}
        </Button>
      </div>
      {invite.error && (
        <p role="alert" className="text-destructive">
          {errorText(invite.error)}
        </p>
      )}
    </form>
  );
}

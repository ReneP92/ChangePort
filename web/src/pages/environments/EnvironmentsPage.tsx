import { useAuth } from "@clerk/react";
import { Pencil, Plus, Shield, ShieldOff, Star, Trash2 } from "lucide-react";
import { useState, type ReactNode } from "react";
import { Link } from "react-router";
import {
  ApiError,
  errorText,
  type Environment,
  type Environments,
  type NewEnvironment,
} from "@/api/client";
import {
  useChangeEnvironment,
  useCreateEnvironment,
  useEnvironments,
  useRemoveEnvironment,
} from "@/api/queries";
import { ProfileRadio } from "@/components/ProfileRadio";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
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
import { environmentPath, renameRemembered } from "@/lib/environments";
import { profiles } from "@/lib/profiles";
import { OrganizationArea } from "@/pages/OrganizationArea";

/** The names an environment usually has, which adding one suggests. */
const suggested = ["production", "staging", "dev"];

/**
 * The environments of the organization, each a Kafka cluster of its own,
 * and for admins, adding, renaming, protecting and removing them, and
 * choosing the default one.
 */
export function EnvironmentsPage() {
  return (
    <OrganizationArea>
      {(admin, organization) => <EnvironmentsContent admin={admin} organization={organization} />}
    </OrganizationArea>
  );
}

function EnvironmentsContent({ admin, organization }: { admin: boolean; organization: string }) {
  const environments = useEnvironments();
  return (
    <main className="mx-auto flex max-w-5xl flex-col gap-6 p-6">
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">Environments</h1>
        <p className="text-sm text-muted-foreground">
          Each environment of {organization} is a Kafka cluster of its own, with its own connection,
          credentials, topics, sources and alerts. The console opens the default one first.
        </p>
      </header>
      {environments.isPending && (
        <p className="text-sm text-muted-foreground">Loading the environments</p>
      )}
      {environments.isError && (
        <p className="text-sm text-destructive">{environments.error.message}</p>
      )}
      {environments.data && <EnvironmentsCard list={environments.data} admin={admin} />}
    </main>
  );
}

function EnvironmentsCard({ list, admin }: { list: Environments; admin: boolean }) {
  const change = useChangeEnvironment();
  const environments = list.environments;
  const trial = environments.some((e) => e.trial);
  const atLimit = environments.length >= list.limit;
  // The organization keeps one environment that is not being removed.
  const live = environments.filter((e) => !e.deleting).length;
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <h2>Your environments</h2>
        </CardTitle>
        <CardDescription>
          {admin
            ? `${String(environments.length)} of at most ${String(list.limit)}`
            : "Ask an admin of your organization to change its environments."}
        </CardDescription>
        {admin && !trial && (
          <CardAction>
            <AddEnvironmentDialog
              taken={environments.map((e) => e.name)}
              disabled={atLimit || environments.length === 0}
            />
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        {admin && trial && (
          <p className="text-muted-foreground">
            A trial is one environment. Once your organization is a customer, you can add more.
          </p>
        )}
        {admin && !trial && atLimit && (
          <p className="text-muted-foreground">
            Your organization has {list.limit} environments, the most it may have. Remove one, or
            ask Changeport for more.
          </p>
        )}
        {change.error && (
          <p role="alert" className="text-destructive">
            {errorText(change.error)}
          </p>
        )}
        <ul aria-label="Environments" className="flex flex-col divide-y">
          {environments.map((environment) => (
            <li
              key={environment.customerID}
              aria-label={environment.name}
              className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 py-3 first:pt-0 last:pb-0"
            >
              <EnvironmentSummary environment={environment} />
              {admin && !environment.deleting && (
                <div className="flex items-center gap-1">
                  <RenameDialog environment={environment} />
                  {environment.default ? (
                    <Button
                      variant="ghost"
                      size="icon"
                      title="The default"
                      aria-label={`${environment.name} is the default`}
                      disabled
                    >
                      <Star className="fill-current" />
                    </Button>
                  ) : (
                    <Button
                      variant="ghost"
                      size="icon"
                      title="Make it the default"
                      aria-label={`Make ${environment.name} the default`}
                      disabled={change.isPending}
                      onClick={() => {
                        change.mutate({ customerID: environment.customerID, default: true });
                      }}
                    >
                      <Star />
                    </Button>
                  )}
                  {!environment.trial &&
                    (environment.deletionProtection ? (
                      <UnprotectDialog environment={environment} />
                    ) : (
                      <Button
                        variant="ghost"
                        size="icon"
                        title="Turn on deletion protection"
                        aria-label={`Protect ${environment.name}`}
                        disabled={change.isPending}
                        onClick={() => {
                          change.mutate({
                            customerID: environment.customerID,
                            deletionProtection: true,
                          });
                        }}
                      >
                        <Shield />
                      </Button>
                    ))}
                  <RemoveDialog environment={environment} last={live === 1} />
                </div>
              )}
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  );
}

/** An environment's name, state and profile. */
export function EnvironmentSummary({ environment }: { environment: Environment }) {
  const profile = profiles.find((p) => p.name === environment.profile);
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <span className="flex flex-wrap items-center gap-2">
        {environment.deleting ? (
          <span className="font-medium">{environment.name}</span>
        ) : (
          <Link
            to={environmentPath(environment.name, "/")}
            className="font-medium underline-offset-4 hover:underline"
          >
            {environment.name}
          </Link>
        )}
        {environment.deleting ? (
          <Badge variant="destructive">Being removed</Badge>
        ) : environment.ready ? (
          <Badge>Ready</Badge>
        ) : (
          <Badge variant="secondary">Starting</Badge>
        )}
        {environment.default && <Badge variant="secondary">Default</Badge>}
        {environment.deletionProtection && (
          <Badge variant="outline">
            <Shield />
            Protected
          </Badge>
        )}
        {environment.trial && <Badge variant="outline">Trial</Badge>}
      </span>
      <span className="text-muted-foreground">
        {environment.profile}
        {profile && `: ${profile.description}`}
      </span>
    </div>
  );
}

/** The problem with one of a form's fields, from the API's answer. */
function fieldProblem(error: Error | null | undefined, field: string): string | null {
  const problem = error instanceof ApiError ? error.problem : undefined;
  return problem?.field === field ? (problem.detail ?? problem.title) : null;
}

/** An error not about one of the form's fields. */
function FormError({ error, fields }: { error: Error | null | undefined; fields: string[] }) {
  if (!error) {
    return null;
  }
  const field = error instanceof ApiError ? error.problem?.field : undefined;
  if (field !== undefined && fields.includes(field)) {
    return null;
  }
  return (
    <p role="alert" className="text-sm text-destructive">
      {errorText(error)}
    </p>
  );
}

function AddEnvironmentDialog({ taken, disabled }: { taken: string[]; disabled: boolean }) {
  const [open, setOpen] = useState(false);
  const create = useCreateEnvironment();
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        create.reset();
      }}
    >
      <DialogTrigger asChild>
        <Button size="sm" disabled={disabled}>
          <Plus />
          Add environment
        </Button>
      </DialogTrigger>
      <DialogContent>
        {open && (
          <AddEnvironmentForm
            taken={taken}
            pending={create.isPending}
            error={create.error}
            onAdd={(environment) => {
              create.mutate(environment, {
                onSuccess: () => {
                  setOpen(false);
                },
              });
            }}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

/**
 * Adding an environment: its name, suggesting one the organization lacks,
 * its profile, and deletion protection, on for production until chosen.
 */
export function AddEnvironmentForm({
  taken,
  pending,
  error,
  onAdd,
}: {
  taken: string[];
  pending: boolean;
  error?: Error | null;
  onAdd: (environment: NewEnvironment) => void;
}) {
  const [environment, setEnvironment] = useState<NewEnvironment>({
    environment: suggested.find((name) => !taken.includes(name)) ?? "",
    profile: "dev",
    deletionProtection: false,
  });
  const [protectionChosen, setProtectionChosen] = useState(false);
  return (
    <form
      className="flex flex-col gap-5"
      noValidate
      onSubmit={(event) => {
        event.preventDefault();
        onAdd({ ...environment, environment: environment.environment.trim() });
      }}
    >
      <DialogHeader>
        <DialogTitle>Add an environment</DialogTitle>
        <DialogDescription>
          A Kafka cluster of its own, with its own connection, credentials and topics, which every
          member of your organization can use once it is ready, in a few minutes.
        </DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-2">
        <Label htmlFor="new-environment-name">Name</Label>
        <Input
          id="new-environment-name"
          autoComplete="off"
          placeholder="staging"
          value={environment.environment}
          aria-invalid={fieldProblem(error, "environment") !== null}
          onChange={(event) => {
            setEnvironment({ ...environment, environment: event.target.value });
          }}
        />
        <p className="text-sm text-muted-foreground">
          Lowercase letters, digits and hyphens. It names the environment in the console&apos;s
          links.
        </p>
        <FieldError text={fieldProblem(error, "environment")} />
      </div>
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-2 text-sm font-medium">Profile</legend>
        <ProfileRadio
          id="new-environment-profile"
          value={environment.profile}
          onChange={(profile) => {
            setEnvironment({
              ...environment,
              profile,
              deletionProtection: protectionChosen
                ? environment.deletionProtection
                : profile === "production",
            });
          }}
        />
        <FieldError text={fieldProblem(error, "profile")} />
      </fieldset>
      <div className="flex items-start gap-3">
        <Checkbox
          id="new-environment-protection"
          className="mt-0.5"
          checked={environment.deletionProtection}
          onCheckedChange={(checked) => {
            setProtectionChosen(true);
            setEnvironment({ ...environment, deletionProtection: checked === true });
          }}
        />
        <Label
          htmlFor="new-environment-protection"
          className="flex flex-col items-start gap-1 font-normal"
        >
          <span className="font-medium">Deletion protection</span>
          <span className="leading-snug text-muted-foreground">
            Removing the environment is refused until an admin turns it off.
          </span>
        </Label>
      </div>
      <FormError error={error} fields={["environment", "profile"]} />
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">
            Cancel
          </Button>
        </DialogClose>
        <Button type="submit" disabled={pending || environment.environment.trim() === ""}>
          {pending ? "Adding" : "Add environment"}
        </Button>
      </DialogFooter>
    </form>
  );
}

function FieldError({ text }: { text: string | null }) {
  return text && <p className="text-sm text-destructive">{text}</p>;
}

/**
 * A dialog behind an icon button, whose form mounts afresh on each open,
 * without the last attempt's error.
 */
function ActionDialog({
  icon,
  label,
  title,
  reset,
  children,
}: {
  icon: ReactNode;
  label: string;
  title: string;
  reset: () => void;
  children: (close: () => void) => ReactNode;
}) {
  const [open, setOpen] = useState(false);
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        reset();
      }}
    >
      <DialogTrigger asChild>
        <Button variant="ghost" size="icon" title={title} aria-label={label}>
          {icon}
        </Button>
      </DialogTrigger>
      <DialogContent>
        {open &&
          children(() => {
            setOpen(false);
          })}
      </DialogContent>
    </Dialog>
  );
}

function RenameDialog({ environment }: { environment: Environment }) {
  const change = useChangeEnvironment();
  const organization = useAuth().orgId ?? "";
  return (
    <ActionDialog
      icon={<Pencil />}
      label={`Rename ${environment.name}`}
      title="Rename"
      reset={change.reset}
    >
      {(close) => (
        <RenameForm
          current={environment.name}
          pending={change.isPending}
          error={change.error}
          onRename={(name) => {
            change.mutate(
              { customerID: environment.customerID, name },
              {
                onSuccess: () => {
                  renameRemembered(organization, environment.name, name);
                  close();
                },
              },
            );
          }}
        />
      )}
    </ActionDialog>
  );
}

/** Renaming an environment, which changes only its links. */
export function RenameForm({
  current,
  pending,
  error,
  onRename,
}: {
  current: string;
  pending: boolean;
  error?: Error | null;
  onRename: (name: string) => void;
}) {
  const [name, setName] = useState(current);
  const trimmed = name.trim();
  return (
    <form
      className="flex flex-col gap-5"
      noValidate
      onSubmit={(event) => {
        event.preventDefault();
        onRename(trimmed);
      }}
    >
      <DialogHeader>
        <DialogTitle>Rename {current}</DialogTitle>
        <DialogDescription>
          The console&apos;s links to it change to the new name. Its cluster, connection and
          credentials stay as they are, so its clients keep working.
        </DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-2">
        <Label htmlFor="environment-new-name">New name</Label>
        <Input
          id="environment-new-name"
          autoComplete="off"
          value={name}
          aria-invalid={fieldProblem(error, "name") !== null}
          onChange={(event) => {
            setName(event.target.value);
          }}
        />
        <FieldError text={fieldProblem(error, "name")} />
      </div>
      <FormError error={error} fields={["name"]} />
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">
            Cancel
          </Button>
        </DialogClose>
        <Button type="submit" disabled={pending || trimmed === "" || trimmed === current}>
          {pending ? "Renaming" : "Rename"}
        </Button>
      </DialogFooter>
    </form>
  );
}

function UnprotectDialog({ environment }: { environment: Environment }) {
  const change = useChangeEnvironment();
  return (
    <ActionDialog
      icon={<ShieldOff />}
      label={`Turn off deletion protection of ${environment.name}`}
      title="Turn off deletion protection"
      reset={change.reset}
    >
      {(close) => (
        <ConfirmByNameForm
          name={environment.name}
          title={`Turn off deletion protection of ${environment.name}?`}
          description="Admins can then remove it, which deletes its cluster with its data."
          action="Turn off"
          pendingAction="Turning off"
          pending={change.isPending}
          error={change.error}
          onConfirm={() => {
            change.mutate(
              {
                customerID: environment.customerID,
                deletionProtection: false,
                confirm: environment.name,
              },
              { onSuccess: close },
            );
          }}
        />
      )}
    </ActionDialog>
  );
}

function RemoveDialog({ environment, last }: { environment: Environment; last: boolean }) {
  const remove = useRemoveEnvironment();
  return (
    <ActionDialog
      icon={<Trash2 />}
      label={`Remove ${environment.name}`}
      title="Remove"
      reset={remove.reset}
    >
      {(close) =>
        last || environment.deletionProtection ? (
          <RemoveRefused name={environment.name} last={last} />
        ) : (
          <ConfirmByNameForm
            name={environment.name}
            title={`Remove ${environment.name}?`}
            description="Its Kafka cluster is deleted, with its topics, messages, sources and credentials, and its clients can no longer connect. This cannot be undone."
            action="Remove"
            pendingAction="Removing"
            destructive
            pending={remove.isPending}
            error={remove.error}
            onConfirm={() => {
              remove.mutate(environment.customerID, { onSuccess: close });
            }}
          />
        )
      }
    </ActionDialog>
  );
}

/** Why an environment cannot be removed. */
export function RemoveRefused({ name, last }: { name: string; last: boolean }) {
  return (
    <>
      <DialogHeader>
        <DialogTitle>{name} cannot be removed</DialogTitle>
        <DialogDescription>
          {last
            ? "Your organization keeps at least one environment. To close your account, contact Changeport."
            : `Its deletion protection is on. Turn it off first, then remove ${name}.`}
        </DialogDescription>
      </DialogHeader>
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">
            OK
          </Button>
        </DialogClose>
      </DialogFooter>
    </>
  );
}

/** A change that takes typing the environment's name, as one not undone. */
export function ConfirmByNameForm({
  name,
  title,
  description,
  action,
  pendingAction,
  destructive = false,
  pending,
  error,
  onConfirm,
}: {
  name: string;
  title: string;
  description: string;
  action: string;
  pendingAction: string;
  destructive?: boolean;
  pending: boolean;
  error?: Error | null;
  onConfirm: () => void;
}) {
  const [typed, setTyped] = useState("");
  return (
    <form
      className="flex flex-col gap-5"
      noValidate
      onSubmit={(event) => {
        event.preventDefault();
        onConfirm();
      }}
    >
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{description}</DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-2">
        <Label htmlFor="confirm-environment">
          Type <span className="font-mono">{name}</span> to confirm
        </Label>
        <Input
          id="confirm-environment"
          autoComplete="off"
          value={typed}
          onChange={(event) => {
            setTyped(event.target.value);
          }}
        />
      </div>
      <FormError error={error} fields={[]} />
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">
            Cancel
          </Button>
        </DialogClose>
        <Button
          type="submit"
          variant={destructive ? "destructive" : "default"}
          disabled={pending || typed.trim() !== name}
        >
          {pending ? pendingAction : action}
        </Button>
      </DialogFooter>
    </form>
  );
}

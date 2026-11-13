import { useState } from "react";
import { Link, useNavigate } from "react-router";
import { ApiError, errorText, type Customer, type NewEnvironment } from "@/api/client";
import { useAddEnvironment, useCustomers } from "@/api/queries";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
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
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";

/** The names an environment usually has, which the dialog suggests. */
const suggested = ["production", "staging", "dev"];

/**
 * The environments of a customer's organization, each a customer and a
 * cluster of its own, and adding one, which a trial cannot.
 */
export function EnvironmentsCard({ customer }: { customer: Customer }) {
  const customers = useCustomers();
  const siblings = (customers.data ?? []).filter(
    (c) => customer.organizationID !== undefined && c.organizationID === customer.organizationID,
  );
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <h2>Environments</h2>
        </CardTitle>
        <CardDescription>
          Each environment of {customer.displayName} is a cluster of its own, with its own
          credentials, topics and alerts, which the organization&apos;s members switch between.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4 text-sm">
        <ul aria-label="Environments" className="flex flex-col gap-1">
          {siblings.map((c) => (
            <li key={c.id} className="flex flex-wrap items-baseline gap-x-2">
              {c.id === customer.id ? (
                <span className="font-medium">{c.environment}</span>
              ) : (
                <Link
                  to={`/operator/customers/${c.id}`}
                  className="font-medium underline underline-offset-4"
                >
                  {c.environment}
                </Link>
              )}
              <span className="text-muted-foreground">
                {c.id}, {c.profile} cluster{c.ready ? "" : ", not ready"}
              </span>
            </li>
          ))}
        </ul>
        {customer.trial ? (
          <p className="text-muted-foreground">
            A trial is one environment. Convert the customer to add more.
          </p>
        ) : (
          customer.organizationID !== undefined && (
            <div>
              <AddEnvironmentDialog
                customer={customer}
                taken={siblings.map((c) => c.environment)}
              />
            </div>
          )
        )}
      </CardContent>
    </Card>
  );
}

function AddEnvironmentDialog({ customer, taken }: { customer: Customer; taken: string[] }) {
  const [open, setOpen] = useState(false);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size="sm">Add environment</Button>
      </DialogTrigger>
      <DialogContent>
        {open && (
          <AddEnvironmentForm
            customer={customer}
            initial={suggested.find((name) => !taken.includes(name)) ?? ""}
            onAdded={() => {
              setOpen(false);
            }}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function AddEnvironmentForm({
  customer,
  initial,
  onAdded,
}: {
  customer: Customer;
  initial: string;
  onAdded: () => void;
}) {
  const add = useAddEnvironment(customer.id);
  const navigate = useNavigate();
  const [environment, setEnvironment] = useState<NewEnvironment>({
    environment: initial,
    profile: "dev",
    deletionProtection: false,
  });
  const problem = add.error instanceof ApiError ? add.error.problem : undefined;
  const fieldProblem = (field: string) =>
    problem?.field === field ? (problem.detail ?? problem.title) : null;
  return (
    <form
      className="flex flex-col gap-5"
      noValidate
      onSubmit={(event) => {
        event.preventDefault();
        add.mutate(
          { ...environment, environment: environment.environment.trim() },
          {
            onSuccess: (created) => {
              onAdded();
              void navigate(`/operator/customers/${created.id}`);
            },
          },
        );
      }}
    >
      <DialogHeader>
        <DialogTitle>Add an environment to {customer.displayName}</DialogTitle>
        <DialogDescription>
          A cluster of its own in the same organization, whose members see it at once.
        </DialogDescription>
      </DialogHeader>
      <div className="flex flex-col gap-2">
        <Label htmlFor="environment-name">Name</Label>
        <Input
          id="environment-name"
          autoComplete="off"
          list="environment-names"
          value={environment.environment}
          onChange={(event) => {
            setEnvironment({ ...environment, environment: event.target.value });
          }}
        />
        <datalist id="environment-names">
          {suggested.map((name) => (
            <option key={name} value={name} />
          ))}
        </datalist>
        {fieldProblem("environment") && (
          <p className="text-sm text-destructive">{fieldProblem("environment")}</p>
        )}
      </div>
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-2 text-sm font-medium">Profile</legend>
        <RadioGroup
          value={environment.profile}
          onValueChange={(profile) => {
            setEnvironment({ ...environment, profile: profile as NewEnvironment["profile"] });
          }}
        >
          {(["dev", "dev-ha", "production"] as const).map((profile) => (
            <div key={profile} className="flex items-center gap-3">
              <RadioGroupItem value={profile} id={`environment-profile-${profile}`} />
              <Label htmlFor={`environment-profile-${profile}`} className="font-normal">
                {profile}
              </Label>
            </div>
          ))}
        </RadioGroup>
      </fieldset>
      <div className="flex items-center gap-3">
        <Checkbox
          id="environment-protection"
          checked={environment.deletionProtection}
          onCheckedChange={(checked) => {
            setEnvironment({ ...environment, deletionProtection: checked === true });
          }}
        />
        <Label htmlFor="environment-protection" className="font-normal">
          Deletion protection
        </Label>
      </div>
      {add.error && !problem?.field && (
        <p role="alert" className="text-sm text-destructive">
          {errorText(add.error)}
        </p>
      )}
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">
            Cancel
          </Button>
        </DialogClose>
        <Button type="submit" disabled={add.isPending || environment.environment.trim() === ""}>
          {add.isPending ? "Adding" : "Add environment"}
        </Button>
      </DialogFooter>
    </form>
  );
}

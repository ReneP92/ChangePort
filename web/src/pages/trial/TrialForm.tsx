import { useState, type ReactNode } from "react";
import {
  ApiError,
  errorText,
  type NewTrialRequest,
  type TrialRetention,
  type TrialThroughput,
} from "@/api/client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Textarea } from "@/components/ui/textarea";
import {
  draftProblems,
  emptyDraft,
  localToday,
  retentions,
  throughputs,
  toRequest,
  type TrialDraft,
} from "@/lib/trials";

/**
 * The form that requests a trial. It checks what the console's API checks
 * before sending, and shows the API's own problem, by field where it names
 * one, when it still refuses.
 */
export function TrialForm({
  onSubmit,
  pending,
  error,
  today = localToday(),
}: {
  onSubmit: (request: NewTrialRequest) => void;
  pending: boolean;
  /** Why the console's API refused the request, if it did. */
  error?: Error | null;
  today?: string;
}) {
  const [draft, setDraft] = useState<TrialDraft>(emptyDraft);
  const [submitted, setSubmitted] = useState(false);
  const problems = draftProblems(draft, today);
  const serverProblem = error instanceof ApiError ? error.problem : undefined;
  const shown = (field: keyof TrialDraft) =>
    (submitted ? problems[field] : undefined) ??
    (serverProblem?.field === field ? (serverProblem.detail ?? serverProblem.title) : undefined);
  const set = (change: Partial<TrialDraft>) => {
    setDraft({ ...draft, ...change });
  };

  const submit = (event: { preventDefault(): void }) => {
    event.preventDefault();
    setSubmitted(true);
    const request = toRequest(draft);
    if (Object.keys(problems).length === 0 && request) {
      onSubmit(request);
    }
  };

  const general = error && !serverProblem?.field ? errorText(error) : undefined;

  return (
    <form onSubmit={submit} className="flex flex-col gap-6" noValidate>
      <Field id="trial-company" label="Company" problem={shown("company")}>
        <Input
          id="trial-company"
          autoComplete="organization"
          value={draft.company}
          aria-invalid={shown("company") ? true : undefined}
          onChange={(event) => {
            set({ company: event.target.value });
          }}
        />
      </Field>

      <Field id="trial-role" label="Your role" problem={shown("role")}>
        <Input
          id="trial-role"
          autoComplete="organization-title"
          placeholder="Staff engineer"
          value={draft.role}
          aria-invalid={shown("role") ? true : undefined}
          onChange={(event) => {
            set({ role: event.target.value });
          }}
        />
      </Field>

      <Field id="trial-use-case" label="What will you use Kafka for?" problem={shown("useCase")}>
        <Textarea
          id="trial-use-case"
          rows={4}
          placeholder="Order events between our checkout and fulfilment services"
          value={draft.useCase}
          aria-invalid={shown("useCase") ? true : undefined}
          onChange={(event) => {
            set({ useCase: event.target.value });
          }}
        />
      </Field>

      <Choice
        legend="Peak throughput"
        name="throughput"
        options={throughputs}
        value={draft.throughput}
        problem={shown("throughput")}
        onChange={(value) => {
          set({ throughput: value as TrialThroughput });
        }}
      />

      <Choice
        legend="How long you keep messages"
        name="retention"
        options={retentions}
        value={draft.retention}
        problem={shown("retention")}
        onChange={(value) => {
          set({ retention: value as TrialRetention });
        }}
      />

      <Field
        id="trial-start"
        label="When you would like to start (optional)"
        problem={shown("preferredStart")}
      >
        <Input
          id="trial-start"
          type="date"
          min={today}
          className="w-fit"
          value={draft.preferredStart}
          aria-invalid={shown("preferredStart") ? true : undefined}
          onChange={(event) => {
            set({ preferredStart: event.target.value });
          }}
        />
      </Field>

      {general && (
        <p role="alert" className="text-sm text-destructive">
          {general}
        </p>
      )}
      <Button type="submit" className="self-start" disabled={pending}>
        {pending ? "Requesting" : "Request a trial"}
      </Button>
    </form>
  );
}

function Field({
  id,
  label,
  problem,
  children,
}: {
  id: string;
  label: string;
  problem?: string | undefined;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      {children}
      {problem && <p className="text-sm text-destructive">{problem}</p>}
    </div>
  );
}

function Choice({
  legend,
  name,
  options,
  value,
  problem,
  onChange,
}: {
  legend: string;
  name: string;
  options: { value: string; label: string }[];
  value?: string | undefined;
  problem?: string | undefined;
  onChange: (value: string) => void;
}) {
  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="mb-2 text-sm font-medium">{legend}</legend>
      <RadioGroup
        value={value ?? ""}
        onValueChange={onChange}
        className="grid grid-cols-1 gap-3 sm:grid-cols-2"
      >
        {options.map((option) => (
          <div key={option.value} className="flex items-center gap-3">
            <RadioGroupItem value={option.value} id={`${name}-${option.value}`} />
            <Label htmlFor={`${name}-${option.value}`} className="font-normal">
              {option.label}
            </Label>
          </div>
        ))}
      </RadioGroup>
      {problem && <p className="text-sm text-destructive">{problem}</p>}
    </fieldset>
  );
}

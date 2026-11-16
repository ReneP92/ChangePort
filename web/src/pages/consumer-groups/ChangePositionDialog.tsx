import { History, TriangleAlert } from "lucide-react";
import { useState, type ReactNode } from "react";
import { Link } from "react-router";
import { ApiError, type PositionChange, type PositionPreview } from "@/api/client";
import { useGroupPositionPreview, useMoveGroup, useReplayGroup } from "@/api/queries";
import { CodeBlock } from "@/components/CodeBlock";
import { Button } from "@/components/ui/button";
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
import { useDebounced } from "@/hooks/use-debounced";
import { formatDuration } from "@/lib/format";
import {
  listWords,
  previewTotals,
  replayName,
  tieredNote,
  toLocalInput,
  topicEffect,
} from "@/lib/position";
import { useEnvironmentPath } from "@/lib/useEnvironment";
import { useNow } from "@/lib/useNow";
import { cn } from "@/lib/utils";
import { Field } from "@/pages/overview/CreateTopicDialog";

type To = PositionChange["to"];

/**
 * A button that opens the dialog to move a consumer group, so that it reads
 * messages again or skips them, or to replay its topics into a new group;
 * it shows what the change does before it is made (docs/console.md).
 */
export function ChangePositionDialog({ group, topics }: { group: string; topics: string[] }) {
  const [open, setOpen] = useState(false);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm">
          <History />
          Change position
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        {open && (
          // Mounted per opening, so that every opening starts afresh.
          <ChangePositionForm
            group={group}
            topics={topics}
            onMoved={() => {
              setOpen(false);
            }}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function ChangePositionForm({
  group,
  topics,
  onMoved,
}: {
  group: string;
  /** The topics the group reads, which it moves on unless some are left out. */
  topics: string[];
  onMoved: () => void;
}) {
  const [to, setTo] = useState<To>("time");
  const [chosen, setChosen] = useState(() => new Set(topics));
  const [time, setTime] = useState(() => toLocalInput(new Date(Date.now() - 60 * 60 * 1000)));
  const [replay, setReplay] = useState(false);
  const [into, setInto] = useState(() => replayName(group, new Date()));
  const [typed, setTyped] = useState("");
  const move = useMoveGroup();
  const replayGroup = useReplayGroup();
  const saving = move.isPending || replayGroup.isPending;

  // A minute's grace, as the server allows, for a time chosen as now.
  const now = useNow(30_000) + 60_000;
  const at = new Date(time);
  const timeProblem =
    to !== "time"
      ? null
      : Number.isNaN(at.getTime())
        ? "Choose a date and time."
        : at.getTime() > now
          ? "Choose a time up to now."
          : null;
  const someTopics = topics.filter((topic) => chosen.has(topic));
  const topicsProblem = someTopics.length === 0 ? "Choose at least one topic." : null;
  const change: PositionChange | null =
    timeProblem || topicsProblem || (replay && into.trim() === "")
      ? null
      : {
          to,
          ...(to === "time" && { time: at.toISOString() }),
          // Every topic it has a position on, unless some are left out.
          ...(someTopics.length < topics.length && { topics: someTopics }),
          ...(replay && { into: into.trim() }),
        };
  // Typing a time or a name asks for a preview once it settles.
  const key = JSON.stringify(change);
  const settledKey = useDebounced(key, 400);
  const settled = JSON.parse(settledKey) as PositionChange | null;
  const preview = useGroupPositionPreview(group, settled);
  const current = settledKey === key && !preview.isPlaceholderData && preview.isSuccess;

  const error = move.error ?? replayGroup.error ?? preview.error;
  const problem = error instanceof ApiError ? error.problem : undefined;
  const fieldProblem = (field: string) =>
    problem?.field === field ? (problem.detail ?? problem.title) : null;
  // Problems about fields the form shows by them; the rest under the form.
  const shownByField = ["time", "into", "topics"];
  const members = replay ? [] : (preview.data?.members ?? []);

  if (replayGroup.data) {
    return <ReplayReady group={group} preview={replayGroup.data} />;
  }

  const submit = (event: { preventDefault(): void }) => {
    event.preventDefault();
    if (!change || !current || members.length > 0 || (!replay && typed !== group)) {
      return;
    }
    if (replay) {
      replayGroup.mutate({ group, change });
    } else {
      move.mutate({ group, change }, { onSuccess: onMoved });
    }
  };
  const skipping = !replay && preview.data !== undefined && previewTotals(preview.data).skipped > 0;

  return (
    <form onSubmit={submit} className="flex flex-col gap-5" noValidate>
      <DialogHeader>
        <DialogTitle className="[overflow-wrap:anywhere]">Change where {group} reads</DialogTitle>
        <DialogDescription>
          Read messages again or skip ahead, in {group} or in a new group.
        </DialogDescription>
      </DialogHeader>

      <fieldset className="flex flex-col gap-3">
        <legend className="mb-3 text-sm font-medium">Read from</legend>
        <RadioGroup
          value={to}
          onValueChange={(value) => {
            setTo(value as To);
            move.reset();
            replayGroup.reset();
          }}
          className="gap-3"
        >
          <Choice value="time" name="to" title="A point in time">
            The first message at or after it, in your time zone.
          </Choice>
          {to === "time" && (
            <div className="flex flex-col gap-2 pl-7">
              <Input
                id="position-time"
                type="datetime-local"
                aria-label="Time"
                aria-invalid={timeProblem !== null || fieldProblem("time") !== null}
                value={time}
                max={toLocalInput(new Date(now))}
                onChange={(event) => {
                  setTime(event.target.value);
                }}
                className="w-56"
              />
              {(timeProblem ?? fieldProblem("time")) && (
                <p className="text-sm text-destructive">{timeProblem ?? fieldProblem("time")}</p>
              )}
            </div>
          )}
          <Choice value="earliest" name="to" title="The oldest message kept">
            Everything its topics still keep.
          </Choice>
          <Choice value="latest" name="to" title="Now">
            {replay ? "Only messages that arrive from now on." : "Skip what it has not read yet."}
          </Choice>
        </RadioGroup>
      </fieldset>

      {topics.length > 1 && (
        <fieldset className="flex flex-col gap-3">
          <legend className="mb-3 text-sm font-medium">Topics</legend>
          <ul className="flex flex-col gap-3">
            {topics.map((topic, i) => (
              <li key={topic} className="flex items-start gap-3">
                <Checkbox
                  id={`position-topic-${String(i)}`}
                  className="mt-0.5"
                  checked={chosen.has(topic)}
                  onCheckedChange={(value) => {
                    setChosen((current) => {
                      const next = new Set(current);
                      if (value === true) {
                        next.add(topic);
                      } else {
                        next.delete(topic);
                      }
                      return next;
                    });
                  }}
                />
                <Label
                  htmlFor={`position-topic-${String(i)}`}
                  className="font-medium [overflow-wrap:anywhere]"
                >
                  {topic}
                </Label>
              </li>
            ))}
          </ul>
          {(topicsProblem ?? fieldProblem("topics")) && (
            <p className="text-sm text-destructive">{topicsProblem ?? fieldProblem("topics")}</p>
          )}
        </fieldset>
      )}

      <fieldset className="flex flex-col gap-3">
        <legend className="mb-3 text-sm font-medium">Group</legend>
        <RadioGroup
          value={replay ? "new" : "same"}
          onValueChange={(value) => {
            setReplay(value === "new");
            move.reset();
            replayGroup.reset();
          }}
          className="gap-3"
        >
          <Choice value="same" name="group" title={group}>
            Its consumers continue from there once you start them again.
          </Choice>
          <Choice value="new" name="group" title="A new group, to replay">
            A service you start with the new group reads the messages; {group} stays where it is.
          </Choice>
        </RadioGroup>
        {replay && (
          <div className="pl-7">
            <Field id="position-into" label="New group" problem={fieldProblem("into")}>
              <Input
                id="position-into"
                autoComplete="off"
                spellCheck={false}
                aria-invalid={fieldProblem("into") !== null}
                value={into}
                onChange={(event) => {
                  setInto(event.target.value);
                }}
                className="font-mono"
              />
            </Field>
          </div>
        )}
      </fieldset>

      <Effects
        group={group}
        replay={replay}
        preview={current ? preview.data : undefined}
        pending={change !== null && !current && !preview.isError}
      />
      {members.length > 0 && (
        <p role="alert" className="flex gap-2 text-sm text-destructive">
          <TriangleAlert aria-hidden className="mt-0.5 size-4 shrink-0" />
          <span className="leading-snug">
            {listWords(members)} {members.length === 1 ? "is" : "are"} reading {group}. Stop its
            consumers first: Kafka moves a group only while it has none. Or replay into a new group
            instead.
          </span>
        </p>
      )}

      {!replay && (
        <div className="flex flex-col gap-2">
          <Label htmlFor="confirm-group">
            Type <span className="font-mono">{group}</span> to confirm
          </Label>
          <Input
            id="confirm-group"
            autoComplete="off"
            spellCheck={false}
            value={typed}
            onChange={(event) => {
              setTyped(event.target.value);
            }}
          />
        </div>
      )}

      {problem && !shownByField.includes(problem.field ?? "") && (
        <p role="alert" className="text-sm text-destructive">
          {problem.detail ?? problem.title}
        </p>
      )}
      {error && !(error instanceof ApiError) && (
        <p role="alert" className="text-sm text-destructive">
          {error.message}
        </p>
      )}
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">
            Cancel
          </Button>
        </DialogClose>
        <Button
          type="submit"
          variant={skipping ? "destructive" : "default"}
          disabled={saving || !current || members.length > 0 || (!replay && typed !== group)}
        >
          {saving ? "Saving" : replay ? `Create ${into.trim() || "group"}` : `Move ${group}`}
        </Button>
      </DialogFooter>
    </form>
  );
}

/** One option of a radio group, with what it means beneath its title. */
function Choice({
  value,
  name,
  title,
  children,
}: {
  value: string;
  name: string;
  title: string;
  children: ReactNode;
}) {
  const id = `position-${name}-${value}`;
  return (
    <div className="flex items-start gap-3">
      <RadioGroupItem value={value} id={id} className="mt-0.5" />
      <Label htmlFor={id} className="flex flex-col items-start gap-1 font-normal">
        <span className="font-medium [overflow-wrap:anywhere]">{title}</span>
        <span className="leading-snug text-muted-foreground">{children}</span>
      </Label>
    </div>
  );
}

/** What the change does on each topic, and what that means for the service. */
export function Effects({
  group,
  replay,
  preview,
  pending,
}: {
  group: string;
  replay: boolean;
  preview: PositionPreview | undefined;
  pending: boolean;
}) {
  if (!preview) {
    return (
      <p role="status" className="min-h-5 text-sm text-muted-foreground">
        {pending ? "Working out what this does" : ""}
      </p>
    );
  }
  const totals = previewTotals(preview);
  const tiered = tieredNote(preview, formatDuration);
  const again = replay ? totals.toRead : totals.readAgain;
  return (
    <section aria-label="What this does" className="flex flex-col gap-3 text-sm">
      <ul role="status" className="flex flex-col divide-y rounded-lg border">
        {preview.topics.map((topic) => (
          <li
            key={topic.topic}
            className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 px-3 py-2"
          >
            <span className="font-medium [overflow-wrap:anywhere]">{topic.topic}</span>
            <span className="text-muted-foreground tabular-nums">{topicEffect(topic, replay)}</span>
          </li>
        ))}
      </ul>
      {tiered && <p className="leading-snug text-muted-foreground">{tiered}</p>}
      {again > 0 && (
        <Warning>
          {replay ? "The service you start with the new group handles" : "Your service handles"}{" "}
          these messages again, with whatever they trigger, such as emails or payments.
        </Warning>
      )}
      {!replay && totals.skipped > 0 && (
        <Warning loss>
          {group} never reads the {totals.skipped.toLocaleString("en-US")} skipped{" "}
          {totals.skipped === 1 ? "message" : "messages"}.
        </Warning>
      )}
    </section>
  );
}

function Warning({ loss = false, children }: { loss?: boolean; children: ReactNode }) {
  return (
    <p className={cn("flex gap-2", loss && "text-destructive")}>
      <TriangleAlert
        aria-hidden
        className={cn("mt-0.5 size-4 shrink-0", !loss && "text-amber-600 dark:text-amber-500")}
      />
      <span className="leading-snug">{children}</span>
    </p>
  );
}

/** The new group of a replay, and how a service starts reading it. */
function ReplayReady({ group, preview }: { group: string; preview: PositionPreview }) {
  const envPath = useEnvironmentPath();
  const { toRead } = previewTotals(preview);
  return (
    <div className="flex flex-col gap-5">
      <DialogHeader>
        <DialogTitle className="[overflow-wrap:anywhere]">{preview.group} is ready</DialogTitle>
        <DialogDescription>
          Start a service with this group, and it reads{" "}
          {toRead === 0 ? "new messages" : `${toRead.toLocaleString("en-US")} messages`} of what{" "}
          {group} reads. {group} stays where it is.
        </DialogDescription>
      </DialogHeader>
      <CodeBlock label="Consumer setting" code={`group.id=${preview.group}`} />
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">
            Close
          </Button>
        </DialogClose>
        <Button asChild>
          <Link to={envPath(`/consumer-groups/${encodeURIComponent(preview.group)}`)}>
            Open {preview.group}
          </Link>
        </Button>
      </DialogFooter>
    </div>
  );
}

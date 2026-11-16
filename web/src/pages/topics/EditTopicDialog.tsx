import { Pencil, TriangleAlert } from "lucide-react";
import { useState } from "react";
import {
  ApiError,
  type ConsumerGroupLag,
  type Topic,
  type TopicChange,
  type TopicPolicy,
} from "@/api/client";
import { useDocsUrl } from "@/api/config";
import { useChangeTopic, useConsumerGroups, useTopicPolicy } from "@/api/queries";
import { DocsLink } from "@/components/DocsLink";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { formatRetention } from "@/lib/format";
import { changeWarnings, compacted } from "@/lib/topicChange";
import { cn } from "@/lib/utils";
import { WithCode } from "@/pages/diagnoses/Diagnoses";
import { Field } from "@/pages/overview/CreateTopicDialog";

/** The retention select's value for a topic kept forever, as it is. */
const forever = "forever";

/**
 * A button that opens the dialog to add partitions to a topic or change how
 * long it keeps messages, which says what the change does to the consumers
 * reading it before it is saved.
 */
export function EditTopicDialog({ topic }: { topic: Topic }) {
  const policy = useTopicPolicy();
  const groups = useConsumerGroups();
  const change = useChangeTopic();
  const [open, setOpen] = useState(false);
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        change.reset();
      }}
    >
      <DialogTrigger asChild>
        <Button variant="outline" size="sm" disabled={!policy.data}>
          <Pencil />
          Edit
        </Button>
      </DialogTrigger>
      <DialogContent>
        {open && policy.data && (
          // Mounted per opening, so that every opening starts from the topic as it is.
          <EditTopicForm
            topic={topic}
            policy={policy.data}
            // Unknown while loading or failed, which the warnings count as a loss.
            readers={groups.data}
            saving={change.isPending}
            error={change.error}
            onEdit={() => {
              change.reset();
            }}
            onSave={(changed) => {
              change.mutate(
                { topic: topic.name, change: changed },
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

/**
 * The form that changes a topic within the guardrails: more partitions, and
 * a retention preset unless it is compacted. readers are the consumer
 * groups' lag, for the warnings, or undefined while unknown; onSave gets
 * only what changed, and onEdit hears of every edit, which makes the
 * server's last problem stale.
 */
export function EditTopicForm({
  topic,
  policy,
  readers,
  saving,
  error,
  onSave,
  onEdit,
}: {
  topic: Topic;
  policy: TopicPolicy;
  readers: ConsumerGroupLag[] | undefined;
  saving: boolean;
  error: Error | null;
  onSave: (change: TopicChange) => void;
  onEdit?: () => void;
}) {
  const [partitions, setPartitions] = useState(topic.partitions);
  const [retentionMs, setRetentionMs] = useState(topic.retentionMs);
  const keepsLatest = compacted(topic);
  const full = topic.partitions >= policy.partitions.max;
  const partitionsProblem =
    Number.isInteger(partitions) &&
    partitions >= topic.partitions &&
    partitions <= policy.partitions.max
      ? null
      : partitions < topic.partitions
        ? `Partitions can be added, never removed: keep at least ${String(topic.partitions)}.`
        : `Choose ${String(topic.partitions)} to ${String(policy.partitions.max)} partitions.`;
  // A retention set outside the console is offered as it is, among the
  // presets, so that it can be kept.
  const presets: { label: string; value: string }[] = policy.retentionPresets.map((preset) => ({
    label: preset.label,
    value: String(preset.ms),
  }));
  if (topic.retentionMs === undefined) {
    presets.push({ label: "Forever", value: forever });
  } else if (!policy.retentionPresets.some((preset) => preset.ms === topic.retentionMs)) {
    presets.push({ label: formatRetention(topic.retentionMs), value: String(topic.retentionMs) });
    presets.sort((a, b) => Number(a.value) - Number(b.value));
  }

  const change: TopicChange = {};
  if (!partitionsProblem && partitions !== topic.partitions) {
    change.partitions = partitions;
  }
  if (!keepsLatest && retentionMs !== undefined && retentionMs !== topic.retentionMs) {
    change.retentionMs = retentionMs;
  }
  const warnings = changeWarnings(
    topic,
    change.partitions ?? topic.partitions,
    keepsLatest ? undefined : retentionMs,
    readers,
  );
  const loss = warnings.some((warning) => warning.loss);
  const docsUrl = useDocsUrl();
  // Each kind of change once, with its section of the docs.
  const docs = [...new Map(warnings.map((warning) => [warning.docs.page, warning.docs])).values()];
  const unchanged = change.partitions === undefined && change.retentionMs === undefined;

  // The server has the last word; its problem is shown by the field it is about.
  const serverProblem = error instanceof ApiError ? error.problem : undefined;
  const fieldProblem = (field: string) =>
    serverProblem?.field === field ? (serverProblem.detail ?? serverProblem.title) : null;

  const submit = (event: { preventDefault(): void }) => {
    event.preventDefault();
    if (!partitionsProblem && !unchanged) {
      onSave(change);
    }
  };

  return (
    <form onSubmit={submit} className="flex flex-col gap-5" noValidate>
      <DialogHeader>
        <DialogTitle className="[overflow-wrap:anywhere]">Edit {topic.name}</DialogTitle>
        <DialogDescription>Add partitions, or change how long it keeps messages.</DialogDescription>
      </DialogHeader>

      <Field
        id="edit-partitions"
        label="Partitions"
        problem={partitionsProblem ?? fieldProblem("partitions")}
      >
        <Input
          id="edit-partitions"
          type="number"
          inputMode="numeric"
          min={topic.partitions}
          max={policy.partitions.max}
          disabled={full}
          aria-describedby="edit-partitions-hint"
          value={Number.isNaN(partitions) ? "" : partitions}
          onChange={(event) => {
            setPartitions(event.target.valueAsNumber);
            onEdit?.();
          }}
          className="w-28"
        />
        <p id="edit-partitions-hint" className="text-sm text-muted-foreground">
          {full
            ? `It has ${String(topic.partitions)}, the most a topic may have on this cluster.`
            : `It has ${String(topic.partitions)}, and may have up to ${String(policy.partitions.max)}.`}
        </p>
      </Field>

      {keepsLatest ? (
        <div className="flex flex-col gap-2 text-sm">
          <span className="font-medium">Retention</span>
          <p className="text-muted-foreground">
            Compacted: it keeps the latest message per key however old, so it has no retention.
          </p>
        </div>
      ) : (
        <Field id="edit-retention" label="Retention" problem={fieldProblem("retention")}>
          <Select
            value={retentionMs === undefined ? forever : String(retentionMs)}
            onValueChange={(value) => {
              setRetentionMs(value === forever ? undefined : Number(value));
              onEdit?.();
            }}
          >
            <SelectTrigger id="edit-retention" className="w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {presets.map((preset) => (
                <SelectItem key={preset.value} value={preset.value}>
                  {preset.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      )}

      {warnings.length > 0 && (
        <ul aria-label="What this change does" className="flex flex-col gap-2 text-sm">
          {warnings.map((warning) => (
            <li key={warning.text} className={cn("flex gap-2", warning.loss && "text-destructive")}>
              <TriangleAlert
                aria-hidden
                className={cn(
                  "mt-0.5 size-4 shrink-0",
                  !warning.loss && "text-amber-600 dark:text-amber-500",
                )}
              />
              <span className="leading-snug">
                <WithCode text={warning.text} />
              </span>
            </li>
          ))}
        </ul>
      )}
      {docsUrl && docs.length > 0 && (
        <p className="-mt-2 flex flex-wrap gap-x-3 pl-6 text-sm text-muted-foreground">
          {docs.map((section) => (
            <DocsLink key={section.page} page={section.page}>
              {section.title}
            </DocsLink>
          ))}
        </p>
      )}
      {/* Always there, so that screen readers hear what a choice adds; out of
          the layout, so that it adds no gap. */}
      <p aria-live="polite" className="sr-only">
        {warnings
          .filter((warning) => warning.loss)
          .map((warning) => warning.text.replaceAll("`", ""))
          .join(" ")}
      </p>

      {serverProblem && !serverProblem.field && (
        <p role="alert" className="text-sm text-destructive">
          {serverProblem.detail ?? serverProblem.title}
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
          variant={loss ? "destructive" : "default"}
          disabled={saving || unchanged || partitionsProblem !== null}
        >
          {saving ? "Saving" : loss ? "Save, losing unread messages" : "Save changes"}
        </Button>
      </DialogFooter>
    </form>
  );
}

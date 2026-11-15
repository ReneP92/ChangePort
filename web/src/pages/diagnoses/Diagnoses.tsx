import { CircleAlert, Info, TriangleAlert, type LucideIcon } from "lucide-react";
import { Link } from "react-router";
import type { Diagnosis } from "@/api/client";
import { useDiagnoses } from "@/api/queries";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { formatDuration } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useEnvironmentPath } from "@/lib/useEnvironment";

const severities: Record<
  Diagnosis["severity"],
  { icon: LucideIcon; label: string; className: string }
> = {
  critical: { icon: CircleAlert, label: "Critical", className: "text-destructive" },
  warning: {
    icon: TriangleAlert,
    label: "Warning",
    className: "text-amber-600 dark:text-amber-500",
  },
  info: { icon: Info, label: "Note", className: "text-muted-foreground" },
};

/**
 * The diagnoses, one per row: what is wrong, what shows it, what to do,
 * and, with links, where to look.
 */
export function DiagnosisRows({
  diagnoses,
  links = true,
}: {
  diagnoses: Diagnosis[];
  links?: boolean;
}) {
  const envPath = useEnvironmentPath();
  return (
    <ul className="flex flex-col divide-y">
      {diagnoses.map((diagnosis) => {
        const severity = severities[diagnosis.severity];
        const Icon = severity.icon;
        return (
          <li
            key={diagnosis.id}
            aria-label={diagnosis.title}
            className="flex gap-3 py-3 first:pt-0 last:pb-0"
          >
            <Icon aria-hidden className={cn("mt-0.5 size-4 shrink-0", severity.className)} />
            <div className="flex min-w-0 flex-col gap-1">
              <p className="font-medium [overflow-wrap:anywhere]">
                <span className="sr-only">{severity.label}: </span>
                {diagnosis.title}
              </p>
              <p className="text-muted-foreground [overflow-wrap:anywhere]">
                {diagnosis.explanation}
              </p>
              <p className="[overflow-wrap:anywhere]">
                <span className="font-medium">What to do: </span>
                {diagnosis.suggestion}
              </p>
              {links && (diagnosis.consumerGroup ?? diagnosis.topic) !== undefined && (
                <p className="flex flex-wrap gap-x-3 text-muted-foreground">
                  {diagnosis.consumerGroup !== undefined && (
                    <Link
                      to={envPath(
                        `/consumer-groups/${encodeURIComponent(diagnosis.consumerGroup)}`,
                      )}
                      className="underline-offset-4 hover:text-foreground hover:underline"
                    >
                      Consumer group {diagnosis.consumerGroup}
                    </Link>
                  )}
                  {diagnosis.topic !== undefined && (
                    <Link
                      to={envPath(`/topics/${encodeURIComponent(diagnosis.topic)}`)}
                      className="underline-offset-4 hover:text-foreground hover:underline"
                    >
                      Topic {diagnosis.topic}
                    </Link>
                  )}
                </p>
              )}
            </div>
          </li>
        );
      })}
    </ul>
  );
}

/**
 * The overview's diagnoses: the common Kafka problems the cluster has, or
 * that it has none.
 */
export function DiagnosesCard() {
  const diagnoses = useDiagnoses();
  const count = diagnoses.data?.diagnoses.length ?? 0;
  const window = formatDuration(diagnoses.data?.windowSeconds ?? 15 * 60);
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <h2>What needs attention</h2>
        </CardTitle>
        <CardDescription>
          {diagnoses.isPending
            ? "Changeport is looking at your topics, consumer groups and producers."
            : count === 0
              ? `Nothing. Changeport looks at your topics, consumer groups and producers for the common Kafka problems, over the last ${window}.`
              : `Common Kafka problems Changeport found in your topics, consumer groups and producers, over the last ${window}, the most pressing first.`}
        </CardDescription>
      </CardHeader>
      {(diagnoses.isError || count > 0) && (
        <CardContent className="text-sm">
          {diagnoses.isError && <p className="text-destructive">{diagnoses.error.message}</p>}
          {diagnoses.data && <DiagnosisRows diagnoses={diagnoses.data.diagnoses} />}
        </CardContent>
      )}
    </Card>
  );
}

/** The diagnoses about one topic or consumer group; nothing without. */
export function DiagnosesAbout({
  topic,
  consumerGroup,
}: {
  topic?: string;
  consumerGroup?: string;
}) {
  const diagnoses = useDiagnoses();
  const about = (diagnoses.data?.diagnoses ?? []).filter((diagnosis) =>
    consumerGroup !== undefined
      ? diagnosis.consumerGroup === consumerGroup
      : diagnosis.topic === topic,
  );
  if (about.length === 0) {
    return null;
  }
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <h2>What needs attention</h2>
        </CardTitle>
      </CardHeader>
      <CardContent className="text-sm">
        <DiagnosisRows diagnoses={about} links={false} />
      </CardContent>
    </Card>
  );
}

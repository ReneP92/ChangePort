import { CircleAlert, Info, TriangleAlert, type LucideIcon } from "lucide-react";
import { Link } from "react-router";
import type { Diagnosis } from "@/api/client";
import { useDocsUrl } from "@/api/config";
import { useDiagnoses } from "@/api/queries";
import { DocsLink } from "@/components/DocsLink";
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
 * Text with commands and settings between backticks, shown as code. A code
 * word never breaks at its hyphens or dots, which would read as two.
 */
export function WithCode({ text }: { text: string }) {
  return text.split("`").map((part, index) =>
    index % 2 === 0 ? (
      part
    ) : (
      <code key={index} className="rounded bg-muted px-1 py-0.5 font-mono text-[0.85em]">
        {part.split(" ").map((word, at) => (
          <span key={at} className="whitespace-nowrap">
            {at > 0 && " "}
            {word}
          </span>
        ))}
      </code>
    ),
  );
}

/** The ID of a diagnosis's title, which names its row. */
const titleID = (id: string) => `diagnosis-${id.replace(/[^\w-]/g, "_")}`;

/**
 * The diagnoses, one per row: what is wrong, what shows it, what to do,
 * its page in the docs, and, with links, where to look.
 */
export function DiagnosisRows({
  diagnoses,
  links = true,
}: {
  diagnoses: Diagnosis[];
  links?: boolean;
}) {
  const envPath = useEnvironmentPath();
  const docsUrl = useDocsUrl();
  return (
    <ul className="flex flex-col divide-y">
      {diagnoses.map((diagnosis) => {
        const severity = severities[diagnosis.severity];
        const Icon = severity.icon;
        const subject = links && (diagnosis.consumerGroup ?? diagnosis.topic) !== undefined;
        return (
          <li
            key={diagnosis.id}
            aria-labelledby={titleID(diagnosis.id)}
            className="flex gap-3 py-3 first:pt-0 last:pb-0"
          >
            <Icon aria-hidden className={cn("mt-0.5 size-4 shrink-0", severity.className)} />
            <div className="flex min-w-0 flex-col gap-1">
              <p className="font-medium [overflow-wrap:anywhere]">
                <span className="sr-only">{severity.label}: </span>
                <span id={titleID(diagnosis.id)}>{diagnosis.title}</span>
              </p>
              <p className="text-muted-foreground [overflow-wrap:anywhere]">
                <WithCode text={diagnosis.explanation} />
              </p>
              <p className="[overflow-wrap:anywhere]">
                <span className="font-medium">What to do: </span>
                <WithCode text={diagnosis.suggestion} />
              </p>
              {(subject || (docsUrl && diagnosis.docs)) && (
                <p className="flex flex-wrap gap-x-3 text-muted-foreground">
                  <DocsLink page={diagnosis.docs}>Learn more</DocsLink>
                  {links && diagnosis.consumerGroup !== undefined && (
                    <Link
                      to={envPath(
                        `/consumer-groups/${encodeURIComponent(diagnosis.consumerGroup)}`,
                      )}
                      className="underline-offset-4 hover:text-foreground hover:underline"
                    >
                      Consumer group {diagnosis.consumerGroup}
                    </Link>
                  )}
                  {links && diagnosis.topic !== undefined && (
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
    <Card aria-busy={diagnoses.isPending}>
      <CardHeader>
        <CardTitle>
          <h2>What needs attention</h2>
        </CardTitle>
        <CardDescription>
          {diagnoses.isPending
            ? "Changeport is looking at your topics, consumer groups and producers."
            : !diagnoses.data
              ? "Changeport could not look at your cluster just now."
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

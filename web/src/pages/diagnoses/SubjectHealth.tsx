import type { HealthLevel, HealthStatus } from "@/api/client";
import { useDiagnoses, useHealthHistory } from "@/api/queries";
import { HealthBadge } from "@/components/HealthBadge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { healthLabels, historySummary } from "@/lib/health";
import { cn } from "@/lib/utils";
import { DiagnosisRows } from "@/pages/diagnoses/Diagnoses";

// Each level by its height as well as its color, so that the strip reads
// without telling colors apart; a step nobody looked at is a thin line.
const cells: Record<HealthLevel, string> = {
  healthy: "h-1.5 bg-emerald-500/60",
  needsAttention: "h-3.5 bg-amber-500",
  atRisk: "h-5 bg-destructive",
};

/**
 * A topic's or a consumer group's health: its level, the last 24 hours of
 * it, and the diagnoses about it, which say why.
 */
export function SubjectHealth({
  health: listed,
  ...subject
}: ({ topic: string } | { consumerGroup: string }) & { health: HealthStatus | undefined }) {
  const diagnoses = useDiagnoses();
  const history = useHealthHistory(subject);
  // The history says it now too, also of a group in no list yet.
  const health = history.data?.current ?? listed;
  const all = diagnoses.data?.diagnoses ?? [];
  // Its own diagnoses set its level. A topic's page also tells what the
  // groups that read it have, which are theirs.
  const own = all.filter((diagnosis) =>
    "topic" in subject
      ? diagnosis.topic === subject.topic && diagnosis.consumerGroup === undefined
      : diagnosis.consumerGroup === subject.consumerGroup,
  );
  const readers =
    "topic" in subject
      ? all.filter(
          (diagnosis) => diagnosis.topic === subject.topic && diagnosis.consumerGroup !== undefined,
        )
      : [];
  const levels = history.data?.points.map((point) => point.level) ?? [];
  const summary = history.data ? historySummary(levels, history.data.stepSeconds) : undefined;
  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-4">
        <div className="flex flex-col gap-1.5">
          <CardTitle>
            <h2>Health</h2>
          </CardTitle>
          <CardDescription>{summary ?? "From the diagnoses of the last 24 hours."}</CardDescription>
        </div>
        <HealthBadge health={health} className="shrink-0 font-medium" />
      </CardHeader>
      <CardContent className="flex flex-col gap-4 text-sm">
        {history.data && (
          <figure className="flex flex-col gap-1">
            <div
              role="img"
              aria-label={summary}
              className="flex h-5 gap-px overflow-hidden rounded-sm"
            >
              {history.data.points.map((point) => (
                <span
                  key={point.time}
                  title={`${new Date(point.time).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}: ${
                    point.level ? healthLabels[point.level] : "Not looked at"
                  }`}
                  className={cn(
                    "flex-1 rounded-[1px]",
                    point.level ? cells[point.level] : "h-px bg-muted-foreground/50",
                  )}
                />
              ))}
            </div>
            <figcaption className="flex justify-between text-xs text-muted-foreground">
              <span>24 hours ago</span>
              <span>Now</span>
            </figcaption>
          </figure>
        )}
        {own.length > 0 && <DiagnosisRows diagnoses={own} links={false} />}
        {readers.length > 0 && (
          <section aria-labelledby="readers" className="flex flex-col gap-3">
            <h3 id="readers" className="font-medium text-muted-foreground">
              The consumer groups that read it
            </h3>
            <DiagnosisRows diagnoses={readers} />
          </section>
        )}
      </CardContent>
    </Card>
  );
}

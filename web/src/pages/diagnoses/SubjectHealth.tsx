import type { HealthLevel, HealthStatus } from "@/api/client";
import { useDiagnoses, useHealthHistory } from "@/api/queries";
import { HealthBadge } from "@/components/HealthBadge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { healthLabels, historySummary } from "@/lib/health";
import { cn } from "@/lib/utils";
import { DiagnosisRows } from "@/pages/diagnoses/Diagnoses";

const cells: Record<HealthLevel, string> = {
  healthy: "bg-emerald-500/45",
  needsAttention: "bg-amber-500",
  atRisk: "bg-destructive",
};

/**
 * A topic's or a consumer group's health: its level, the last 24 hours of
 * it, and the diagnoses about it, which say why.
 */
export function SubjectHealth({
  health,
  ...subject
}: ({ topic: string } | { consumerGroup: string }) & { health: HealthStatus | undefined }) {
  const diagnoses = useDiagnoses();
  const history = useHealthHistory(subject);
  const about = (diagnoses.data?.diagnoses ?? []).filter((diagnosis) =>
    "topic" in subject
      ? diagnosis.topic === subject.topic
      : diagnosis.consumerGroup === subject.consumerGroup,
  );
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
                  className={cn("flex-1", point.level ? cells[point.level] : "bg-muted")}
                />
              ))}
            </div>
            <figcaption className="flex justify-between text-xs text-muted-foreground">
              <span>24 hours ago</span>
              <span>Now</span>
            </figcaption>
          </figure>
        )}
        {about.length > 0 && <DiagnosisRows diagnoses={about} links={false} />}
      </CardContent>
    </Card>
  );
}

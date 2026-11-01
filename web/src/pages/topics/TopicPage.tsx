import { Link, useParams } from "react-router";
import { useAlerts, useConsumerGroups, useTopics } from "@/api/queries";
import { Notice } from "@/components/Notice";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { alertsAbout } from "@/lib/alerts";
import { formatBytes, formatLag, formatRate, formatRetention } from "@/lib/format";
import { cn } from "@/lib/utils";
import { AlertBadge, AlertRows } from "@/pages/alerts/ActiveAlerts";
import { CustomerArea } from "@/pages/CustomerArea";
import { ThroughputCard } from "@/pages/overview/ThroughputCard";

/**
 * One topic: its settings, its traffic with what changed meanwhile, and the
 * consumer groups reading it.
 */
export function TopicPage() {
  const { name = "" } = useParams();
  return <CustomerArea>{() => <Topic name={name} />}</CustomerArea>;
}

function Topic({ name }: { name: string }) {
  const topics = useTopics();
  const alerts = useAlerts();
  if (topics.isPending) {
    return <Notice title="Loading" busy />;
  }
  if (topics.isError) {
    return <Notice title="The console is unavailable">{topics.error.message}</Notice>;
  }
  const topic = topics.data.find((t) => t.name === name);
  if (!topic) {
    return (
      <Notice title="There is no such topic">
        The cluster has no topic {name}.{" "}
        <Link to="/" className="underline underline-offset-4">
          Back to the cluster
        </Link>
      </Notice>
    );
  }
  const topicAlerts = alerts.data?.filter((alert) => alert.topic === name) ?? [];
  const settings: [string, string][] = [
    ["Partitions", String(topic.partitions)],
    [
      "Retention",
      topic.cleanupPolicy.includes("compact") ? "Compacted" : formatRetention(topic.retentionMs),
    ],
    ["Storage", topic.tiered ? "Tiered" : "Brokers only"],
    [
      "Size",
      topic.localBytes === undefined && topic.remoteBytes === undefined
        ? "–"
        : formatBytes((topic.localBytes ?? 0) + (topic.remoteBytes ?? 0)),
    ],
    ["In", topic.bytesInPerSecond === undefined ? "–" : formatRate(topic.bytesInPerSecond)],
    ["Out", topic.bytesOutPerSecond === undefined ? "–" : formatRate(topic.bytesOutPerSecond)],
  ];
  return (
    <main className="mx-auto flex max-w-5xl flex-col gap-6 p-6">
      <header className="flex flex-col gap-1">
        <nav aria-label="Breadcrumb" className="text-sm text-muted-foreground">
          <Link to="/" className="underline-offset-4 hover:underline">
            Cluster
          </Link>{" "}
          / Topics
        </nav>
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="text-2xl font-semibold tracking-tight [overflow-wrap:anywhere]">
            {topic.name}
          </h1>
          <AlertBadge alerts={alertsAbout(alerts.data, { topic: name })} />
          {topic.source && (
            <Badge variant="outline" asChild>
              <Link to={`/sources/${topic.source}`}>Source {topic.source}</Link>
            </Badge>
          )}
        </div>
      </header>
      {topicAlerts.length > 0 && (
        <section
          aria-label="Alerts"
          className="rounded-xl border border-destructive/40 bg-destructive/5 p-4 text-sm"
        >
          <AlertRows alerts={topicAlerts} />
        </section>
      )}
      <Card>
        <CardHeader>
          <CardTitle>
            <h2>Settings</h2>
          </CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm sm:grid-cols-3">
            {settings.map(([term, value]) => (
              <div key={term} className="flex flex-col gap-0.5">
                <dt className="text-muted-foreground">{term}</dt>
                <dd className="font-medium tabular-nums">{value}</dd>
              </div>
            ))}
          </dl>
        </CardContent>
      </Card>
      <ThroughputCard topic={topic.name} />
      <ReadersCard topic={topic.name} />
    </main>
  );
}

/** The consumer groups that read the topic, and how far behind each is. */
function ReadersCard({ topic }: { topic: string }) {
  const groups = useConsumerGroups();
  const readers = groups.data?.filter((group) => group.topic === topic) ?? [];
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <h2>Consumer groups</h2>
        </CardTitle>
        <CardDescription>The groups that read this topic, most behind first.</CardDescription>
      </CardHeader>
      <CardContent className="text-sm">
        {groups.isPending && <p className="text-muted-foreground">Loading consumer groups</p>}
        {groups.isError && <p className="text-destructive">{groups.error.message}</p>}
        {groups.data && readers.length === 0 && (
          <p className="text-muted-foreground">No consumer group has committed offsets here yet.</p>
        )}
        {readers.length > 0 && (
          <ul className="flex flex-col divide-y">
            {readers.map((group) => (
              <li
                key={group.group}
                className="flex items-center justify-between gap-4 py-2 first:pt-0 last:pb-0"
              >
                <Link
                  to={`/consumer-groups/${encodeURIComponent(group.group)}`}
                  className="font-medium underline-offset-4 [overflow-wrap:anywhere] hover:underline"
                >
                  {group.group}
                </Link>
                <span
                  className={cn(
                    "shrink-0",
                    group.lagMessages === 0 ? "text-muted-foreground" : "font-medium",
                  )}
                >
                  {formatLag(group.lagSeconds, group.lagMessages, group.capped)}
                </span>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

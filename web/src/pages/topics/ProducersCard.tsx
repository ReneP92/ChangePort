import { TriangleAlert } from "lucide-react";
import type { TopicProducers } from "@/api/client";
import { useTopicProducers } from "@/api/queries";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { formatAgo } from "@/lib/format";
import { useNow } from "@/lib/useNow";

/**
 * The producers that write a topic idempotently or in transactions, as Kafka
 * knows them (docs/console.md), and what open transactions hold back.
 */
export function ProducersCard({ topic }: { topic: string }) {
  const producers = useTopicProducers(topic);
  const now = useNow();
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <h2>Producers</h2>
        </CardTitle>
        <CardDescription>
          Those that write it idempotently or in transactions, as Kafka knows them: transactional
          ones by their transactional ID, the others by an ID Kafka gives them.
        </CardDescription>
      </CardHeader>
      <CardContent className="text-sm">
        {producers.isPending && <p className="text-muted-foreground">Loading producers</p>}
        {producers.isError && <p className="text-destructive">{producers.error.message}</p>}
        {producers.data && <ProducerList producers={producers.data} now={now} />}
      </CardContent>
    </Card>
  );
}

/** A topic's producers, the most recent first. */
export function ProducerList({ producers, now }: { producers: TopicProducers; now: number }) {
  return (
    <div className="flex flex-col gap-3">
      {producers.heldBack > 0 && (
        <p className="flex gap-2">
          <TriangleAlert
            aria-hidden
            className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-500"
          />
          <span>
            Open transactions hold back {producers.heldBack.toLocaleString()} message
            {producers.heldBack === 1 ? "" : "s"} from read_committed consumers until they commit or
            abort.
          </span>
        </p>
      )}
      {producers.producers.length === 0 ? (
        <p className="text-muted-foreground">
          No producer wrote to it idempotently or in a transaction in the last day.
        </p>
      ) : (
        <ul aria-label="Producers" className="flex flex-col divide-y">
          {producers.producers.map((producer) => (
            <li
              key={producer.producerId}
              className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 py-2 first:pt-0 last:pb-0"
            >
              <span className="flex min-w-0 flex-wrap items-center gap-2">
                <span className="font-medium [overflow-wrap:anywhere]">
                  {producer.transactionalId ?? `Producer ${String(producer.producerId)}`}
                </span>
                <Badge variant="outline">
                  {producer.transactionalId ? "Transactional" : "Idempotent"}
                </Badge>
                {producer.inTransaction && <Badge variant="secondary">In a transaction</Badge>}
              </span>
              <span className="shrink-0 text-muted-foreground">
                {producer.partitions === 1
                  ? "1 partition"
                  : `${String(producer.partitions)} partitions`}
                , last wrote {formatAgo(producer.lastWrite, now)}
              </span>
            </li>
          ))}
        </ul>
      )}
      <p className="text-muted-foreground">
        Producers that are not idempotent never show here: Kafka keeps no state for them.
      </p>
    </div>
  );
}

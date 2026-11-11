import { Check } from "lucide-react";
import { useEffect, useState } from "react";
import { useConsumerGroups, useTopics } from "@/api/queries";
import { useTail } from "@/api/tail";
import { firstMessage } from "@/lib/onboarding";
import { cn } from "@/lib/utils";
import { MessageRow } from "@/pages/topics/TailCard";
import { StepTitle } from "@/pages/welcome/StepTitle";

/** How many of the arrived messages the step shows. */
const shown = 5;

/**
 * The customer runs their example, and sees their message arrive: admins
 * in the topic's tail, as it lands, members by the topic's traffic. Then
 * the consumer group that read it.
 */
export function FirstMessageStep({
  admin,
  topic,
  onArrived,
}: {
  admin: boolean;
  topic: string;
  onArrived: (arrived: boolean) => void;
}) {
  const [since] = useState(() => Date.now());
  const tail = useTail(topic, admin);
  const topics = useTopics();
  const groups = useConsumerGroups(5_000);
  const progress = firstMessage(topic, since, tail.messages, topics.data, groups.data);
  useEffect(() => {
    onArrived(progress.arrived);
  }, [progress.arrived, onArrived]);
  return (
    <>
      <StepTitle>Send your first message</StepTitle>
      <p className="text-muted-foreground">
        Run the example you picked. Changeport watches <code>{topic}</code>, and shows your message
        as it arrives.
      </p>
      <Progress done={progress.arrived}>
        {progress.arrived
          ? `Your message arrived on ${topic}.`
          : `Waiting for a message on ${topic}`}
      </Progress>
      {progress.messages.length > 0 && (
        <ol aria-label="Messages" className="flex flex-col divide-y rounded-lg border">
          {progress.messages.slice(0, shown).map((message) => (
            <MessageRow
              key={`${String(message.partition)}:${String(message.offset)}`}
              message={message}
            />
          ))}
        </ol>
      )}
      {tail.error && (
        <p role="alert" className="text-destructive">
          {tail.error}
        </p>
      )}
      {progress.arrived && (
        <Progress done={progress.readBy !== undefined}>
          {progress.readBy
            ? `Consumer group ${progress.readBy} read it.`
            : "Waiting for a consumer group to read it"}
        </Progress>
      )}
      {progress.readBy && (
        <p className="text-muted-foreground">
          That is Kafka at work: your producer wrote to the topic, and your consumer group read it
          at its own pace. Your cluster&apos;s overview shows them on its data flow map, and how far
          behind the group is.
        </p>
      )}
    </>
  );
}

function Progress({ done, children }: { done: boolean; children: string }) {
  return (
    <p role="status" className={cn("flex items-center gap-2", !done && "text-muted-foreground")}>
      {done ? (
        <Check className="size-4 text-primary" aria-hidden />
      ) : (
        <span
          className="size-2 rounded-full bg-muted-foreground motion-safe:animate-pulse"
          aria-hidden
        />
      )}
      {children}
    </p>
  );
}

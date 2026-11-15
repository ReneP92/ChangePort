import type { ConsumerGroupLag, Topic } from "@/api/client";
import { formatDuration, formatRetention } from "@/lib/format";

/**
 * A warning about what a change to a topic does to the consumers reading it:
 * loss means messages are deleted before a group reads them.
 */
export interface ChangeWarning {
  loss: boolean;
  text: string;
}

/** Whether a topic keeps the latest message per key, and so has no retention. */
export function compacted(topic: Topic): boolean {
  return topic.cleanupPolicy.includes("compact");
}

/**
 * What adding partitions or a new retention does, before it is saved
 * (docs/console.md): groups further behind than the new retention lose what
 * they have not read, a shorter retention deletes older messages, and more
 * partitions send a key's new messages elsewhere. readers are the consumer
 * groups' lag, or undefined while it is not known, which counts as a loss.
 */
export function changeWarnings(
  topic: Topic,
  partitions: number,
  retentionMs: number | undefined,
  readers: ConsumerGroupLag[] | undefined,
): ChangeWarning[] {
  const warnings: ChangeWarning[] = [];
  if (
    retentionMs !== undefined &&
    !compacted(topic) &&
    retentionMs < (topic.retentionMs ?? Infinity)
  ) {
    const retention = formatRetention(retentionMs).toLowerCase();
    if (readers === undefined) {
      warnings.push({
        loss: true,
        text: `How far behind the consumer groups are is not known, so a group further behind than ${retention} may lose messages it has not read.`,
      });
    }
    const behind = (readers ?? [])
      .filter((reader) => reader.topic === topic.name && reader.lagMessages > 0)
      .sort((a, b) => b.lagSeconds - a.lagSeconds);
    for (const reader of behind) {
      const lag = `${reader.capped ? "more than " : ""}${formatDuration(reader.lagSeconds)}`;
      if (reader.lagSeconds * 1000 >= retentionMs) {
        warnings.push({
          loss: true,
          text: `${reader.group} is ${lag} behind. Keeping messages for ${retention}, ${topic.name} deletes messages ${reader.group} has not read yet.`,
        });
      } else if (reader.lagSeconds * 1000 >= retentionMs / 2) {
        warnings.push({
          loss: false,
          text: `${reader.group} is ${lag} behind, close to the ${retention} ${topic.name} would keep messages. Unless it catches up, messages are deleted before it reads them.`,
        });
      }
    }
    warnings.push({
      loss: false,
      text: `Kafka then deletes messages older than ${retention} for good, a log segment at a time: within minutes on a busy topic, and on a quiet one once its current segment closes, after up to 7 days.`,
    });
  }
  if (partitions > topic.partitions) {
    warnings.push({
      loss: false,
      text: "Messages with the same key always go to the same partition. With more partitions, a key's new messages may go to another partition than its earlier ones, so for a while consumers may read them out of order.",
    });
    warnings.push({
      loss: false,
      text: "Consumers find the new partitions within 5 minutes (metadata.max.age.ms). Those with auto.offset.reset=latest, the Java client's default, skip the messages that reach a new partition before they do.",
    });
    warnings.push({
      loss: false,
      text: "Partitions can be added, never removed.",
    });
  }
  return warnings;
}

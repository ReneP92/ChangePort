import type { PositionPreview, TopicPosition } from "@/api/client";

/** A time for a datetime-local input: the browser's local time, to the minute. */
export function toLocalInput(time: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${String(time.getFullYear())}-${pad(time.getMonth() + 1)}-${pad(time.getDate())}T${pad(time.getHours())}:${pad(time.getMinutes())}`;
}

/** The name the console suggests for a replay of group: its name and the day. */
export function replayName(group: string, today: Date): string {
  return `${group}-replay-${toLocalInput(today).slice(0, 10).replaceAll("-", "")}`;
}

/** "1 message" or "2,100 messages". */
function messages(n: number): string {
  return `${n.toLocaleString("en-US")} ${n === 1 ? "message" : "messages"}`;
}

/** When a topic's first message to read is, briefly, in the browser's time. */
function since(position: TopicPosition): string {
  if (!position.from) {
    return "";
  }
  const from = new Date(position.from).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
  return `, from ${from}`;
}

/** What a move or a replay does on one topic, in a phrase after its name. */
export function topicEffect(position: TopicPosition, replay: boolean): string {
  if (replay) {
    return position.toRead === 0
      ? "only new messages"
      : `reads ${messages(position.toRead)}${since(position)}`;
  }
  const parts: string[] = [];
  if (position.readAgain > 0) {
    parts.push(`reads ${messages(position.readAgain)} again${since(position)}`);
  }
  if (position.skipped > 0) {
    parts.push(`skips ${messages(position.skipped)}`);
  }
  return parts.length === 0 ? "stays where it is" : parts.join(" and ");
}

/** The totals of a preview over its topics. */
export function previewTotals(preview: PositionPreview) {
  let readAgain = 0;
  let skipped = 0;
  let toRead = 0;
  let tiered = 0;
  for (const topic of preview.topics) {
    readAgain += topic.readAgain;
    skipped += topic.skipped;
    toRead += topic.toRead;
    tiered += topic.tieredMessages;
  }
  return { readAgain, skipped, toRead, tiered };
}

/** How many messages come from tiered storage, and how long that takes at least. */
export function tieredNote(
  preview: PositionPreview,
  formatDuration: (s: number) => string,
): string | null {
  const { tiered } = previewTotals(preview);
  if (tiered === 0) {
    return null;
  }
  const what = `${messages(tiered)} ${tiered === 1 ? "is" : "are"} older than the brokers keep on disk, and come back from tiered storage`;
  return preview.tieredSeconds && preview.tieredSeconds > 0
    ? `${what}, which takes at least ${formatDuration(preview.tieredSeconds)}.`
    : `${what}.`;
}

/** "a", "a and b", "a, b and c". */
export function listWords(words: string[]): string {
  return words.length <= 1
    ? (words[0] ?? "")
    : `${words.slice(0, -1).join(", ")} and ${words.at(-1) ?? ""}`;
}

import { useState } from "react";
import type { TailMessage, TailPayload } from "@/api/client";
import { useTail } from "@/api/tail";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { formatBytes } from "@/lib/format";
import { cn } from "@/lib/utils";

/**
 * A topic's messages, for admins: its recent ones, then new ones as they
 * arrive, read by the console as it shows them and never kept. Members see
 * why they cannot.
 */
export function TailCard({ topic, admin }: { topic: string; admin: boolean }) {
  const [running, setRunning] = useState(false);
  const tail = useTail(topic, running && admin);
  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-4">
        <div className="flex flex-col gap-1.5">
          <CardTitle>
            <h2>Messages</h2>
          </CardTitle>
          <CardDescription>
            {admin
              ? "The topic's last 50 messages, then new ones as they arrive, at most 20 a second. Changeport reads them only to show them here, and keeps none."
              : "Only your organization's admins can read the topic's messages, as they may hold what only admins should see."}
          </CardDescription>
        </div>
        {admin && (
          <Button
            variant={running ? "outline" : "default"}
            className="shrink-0"
            onClick={() => {
              setRunning(!running);
            }}
          >
            {running ? "Stop" : "Show messages"}
          </Button>
        )}
      </CardHeader>
      {admin && (running || tail.messages.length > 0 || tail.status === "failed") && (
        <CardContent className="flex flex-col gap-3 text-sm">
          <p role="status" className="flex items-center gap-2 text-muted-foreground">
            {tail.status === "live" && (
              <span aria-hidden className="size-2 animate-pulse rounded-full bg-emerald-500" />
            )}
            {statusText(tail.status, tail.messages.length)}
            {tail.skipped > 0 &&
              ` ${tail.skipped.toLocaleString()} more arrived than the console shows, and were skipped.`}
          </p>
          {tail.error && (
            <p role="alert" className="text-destructive">
              {tail.error}
            </p>
          )}
          {tail.messages.length > 0 && (
            <ol aria-label="Messages" className="flex flex-col divide-y rounded-lg border">
              {tail.messages.map((message) => (
                <MessageRow
                  key={`${String(message.partition)}:${String(message.offset)}`}
                  message={message}
                />
              ))}
            </ol>
          )}
        </CardContent>
      )}
    </Card>
  );
}

function statusText(status: string, count: number): string {
  switch (status) {
    case "connecting":
      return "Connecting.";
    case "live":
      return count === 0
        ? "Live; no messages yet."
        : `Live; the newest ${count.toLocaleString()} shown.`;
    case "failed":
      return "Stopped.";
    default:
      return "Stopped.";
  }
}

const formats: Record<TailPayload["format"], string> = {
  json: "JSON",
  text: "Text",
  binary: "Binary",
  avro: "Avro",
};

/** One message: where and when, its key and value, and, opened, all of it. */
export function MessageRow({ message }: { message: TailMessage }) {
  const [open, setOpen] = useState(false);
  return (
    <li className="flex flex-col gap-2 p-3">
      <button
        type="button"
        aria-expanded={open}
        className="flex min-w-0 flex-col gap-1 text-left"
        onClick={() => {
          setOpen(!open);
        }}
      >
        <span className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
          <span className="tabular-nums">
            Partition {message.partition}, offset {message.offset.toLocaleString()}
          </span>
          <time dateTime={message.timestamp}>{new Date(message.timestamp).toLocaleString()}</time>
          {message.value && <Badge variant="secondary">{formats[message.value.format]}</Badge>}
        </span>
        <span className="flex min-w-0 gap-2 font-mono text-xs">
          {message.key && (
            <span className="max-w-1/3 shrink-0 truncate font-medium">{message.key.text}</span>
          )}
          <span className="min-w-0 truncate">
            {message.value ? oneLine(message.value) : "No value"}
          </span>
        </span>
      </button>
      {open && (
        <dl className="grid grid-cols-1 gap-x-4 gap-y-1 text-xs sm:grid-cols-[minmax(0,14rem)_minmax(0,1fr)] sm:gap-y-2">
          <dt className="text-muted-foreground">Key</dt>
          <dd className="mb-2 min-w-0 sm:mb-0">
            <PayloadText payload={message.key} />
          </dd>
          <dt className="text-muted-foreground">Value</dt>
          <dd className="mb-2 min-w-0 sm:mb-0">
            <PayloadText payload={message.value} />
          </dd>
          {message.headers.map((header, i) => (
            <HeaderRow
              key={`${header.key}\u0000${String(i)}`}
              name={header.key}
              payload={header.value}
            />
          ))}
        </dl>
      )}
    </li>
  );
}

function HeaderRow({ name, payload }: { name: string; payload: TailPayload | undefined }) {
  return (
    <>
      <dt className="text-muted-foreground [overflow-wrap:anywhere]">Header {name}</dt>
      <dd className="mb-2 min-w-0 sm:mb-0">
        <PayloadText payload={payload} />
      </dd>
    </>
  );
}

/** A value on one line, for the list. */
function oneLine(payload: TailPayload): string {
  return payload.format === "binary" ? `0x${payload.text}` : payload.text.replace(/\s+/g, " ");
}

/** A key, value or header in full: JSON indented, binary as hex. */
function PayloadText({ payload }: { payload: TailPayload | undefined }) {
  if (!payload) {
    return <span className="text-muted-foreground">None</span>;
  }
  return (
    <div className="flex flex-col gap-1">
      <pre
        className={cn(
          "max-h-80 overflow-auto rounded-md bg-muted p-2 font-mono whitespace-pre-wrap [overflow-wrap:anywhere]",
          payload.format === "binary" && "break-all",
        )}
      >
        {shown(payload)}
      </pre>
      <span className="text-muted-foreground">
        {formats[payload.format]}
        {payload.schemaId !== undefined && `, schema ${String(payload.schemaId)}`},{" "}
        {formatBytes(payload.size)}
        {payload.truncated && "; only its start is shown"}
      </span>
    </div>
  );
}

function shown(payload: TailPayload): string {
  if ((payload.format === "json" || payload.format === "avro") && !payload.truncated) {
    try {
      return JSON.stringify(JSON.parse(payload.text), null, 2);
    } catch {
      return payload.text;
    }
  }
  return payload.text;
}

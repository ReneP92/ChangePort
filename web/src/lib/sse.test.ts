import { describe, expect, test } from "bun:test";
import { readEvents, type ServerSentEvent } from "@/lib/sse";

/** A body that arrives in the given chunks. */
function body(...chunks: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  return new ReadableStream({
    start(controller) {
      for (const chunk of chunks) {
        controller.enqueue(encoder.encode(chunk));
      }
      controller.close();
    },
  });
}

async function all(stream: ReadableStream<Uint8Array>): Promise<ServerSentEvent[]> {
  const events: ServerSentEvent[] = [];
  for await (const event of readEvents(stream)) {
    events.push(event);
  }
  return events;
}

describe("readEvents", () => {
  test("reads events however the body is split, and skips comments", async () => {
    const events = await all(
      body(
        'event: alerts\ndata: {"alerts":[]}\n\n',
        ": still here\n\n",
        "event: al",
        'erts\r\ndata: {"alerts":\r',
        "\ndata: [1]}\r\n\r\n",
        "data: unnamed\n\n",
        "data: unfinished",
      ),
    );
    expect(events).toEqual([
      { event: "alerts", data: '{"alerts":[]}' },
      { event: "alerts", data: '{"alerts":\n[1]}' },
      { event: "message", data: "unnamed" },
    ]);
  });
});

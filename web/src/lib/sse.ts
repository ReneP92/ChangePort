/** A server-sent event: its type, "message" unless named, and its data. */
export interface ServerSentEvent {
  event: string;
  data: string;
}

/**
 * Reads the events of a text/event-stream body as they arrive, as the
 * browser's EventSource would; EventSource itself cannot send the session
 * token. Comments and fields other than event and data are skipped.
 */
export async function* readEvents(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<ServerSentEvent> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let event = "";
  let data: string[] = [];
  for (;;) {
    const { done, value } = await reader.read();
    if (done) {
      return;
    }
    buffer += decoder.decode(value, { stream: true });
    let end: number;
    while ((end = buffer.search(/\r\n|\r|\n/)) >= 0) {
      if (end === buffer.length - 1 && buffer.endsWith("\r")) {
        // The \n of a \r\n may be in the next chunk.
        break;
      }
      const line = buffer.slice(0, end);
      buffer = buffer.slice(end + (buffer.startsWith("\r\n", end) ? 2 : 1));
      if (line === "") {
        if (data.length > 0) {
          yield { event: event || "message", data: data.join("\n") };
        }
        event = "";
        data = [];
        continue;
      }
      if (line.startsWith(":")) {
        continue;
      }
      const colon = line.indexOf(":");
      const field = colon < 0 ? line : line.slice(0, colon);
      const content = colon < 0 ? "" : line.slice(colon + 1).replace(/^ /, "");
      if (field === "event") {
        event = content;
      } else if (field === "data") {
        data.push(content);
      }
    }
  }
}

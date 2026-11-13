import { useAuth } from "@clerk/react";
import { useEffect, useState } from "react";
import { ApiError, type Problem, type TailMessage, type TailSkipped } from "@/api/client";
import { environmentHeaders } from "@/lib/environments";
import { readEvents } from "@/lib/sse";

/** How many messages a tail keeps on screen; older ones scroll away. */
export const tailShown = 200;

/** Where a tail stands. */
export interface TailState {
  status: "idle" | "connecting" | "live" | "failed";
  /** The messages, newest first. */
  messages: TailMessage[];
  /** How many messages the console skipped to keep within its rate. */
  skipped: number;
  error?: string;
}

const idle: TailState = { status: "idle", messages: [], skipped: 0 };

/**
 * Tails a topic while running: its recent messages, then new ones as they
 * arrive. The stream ends when the session token expires, and opens again
 * at once with a new one, going on in each partition where it ended; after
 * a failure of the network or the server, a little later each time. A
 * refusal, such as for a member who is no admin, stops it.
 */
export function useTail(topic: string, running: boolean): TailState {
  const { getToken } = useAuth();
  const [state, setState] = useState<TailState>(idle);
  useEffect(() => {
    if (!running) {
      return;
    }
    const controller = new AbortController();
    const { signal } = controller;
    const sleep = (ms: number) =>
      new Promise<void>((resolve) => {
        const timer = setTimeout(resolve, ms);
        signal.addEventListener("abort", () => {
          clearTimeout(timer);
          resolve();
        });
      });
    // The next offset of each partition, to go on from after a reconnect.
    const next = new Map<number, number>();
    let skippedBefore = 0;
    setState({ status: "connecting", messages: [], skipped: 0 });
    void (async () => {
      let failures = 0;
      while (!signal.aborted) {
        const query = new URLSearchParams({ from: next.size > 0 ? "latest" : "recent" });
        if (next.size > 0) {
          query.set("resume", [...next].map(([p, o]) => `${String(p)}:${String(o)}`).join(","));
        }
        try {
          const token = await getToken();
          const response = await fetch(
            `${window.location.origin}/api/customer/topics/${encodeURIComponent(topic)}/tail?${query.toString()}`,
            {
              headers: {
                Accept: "text/event-stream",
                Authorization: `Bearer ${token ?? ""}`,
                ...environmentHeaders(),
              },
              signal,
            },
          );
          if (!response.ok || !response.body) {
            const problem = (await response.json().catch(() => undefined)) as Problem | undefined;
            throw new ApiError(response.status, problem);
          }
          setState((s) => ({ ...s, status: "live" }));
          failures = 0;
          let skippedHere = 0;
          for await (const event of readEvents(response.body)) {
            if (event.event === "message") {
              const message = JSON.parse(event.data) as TailMessage;
              next.set(message.partition, message.offset + 1);
              setState((s) => ({ ...s, messages: [message, ...s.messages].slice(0, tailShown) }));
            } else if (event.event === "skipped") {
              skippedHere = (JSON.parse(event.data) as TailSkipped).skipped;
              setState((s) => ({ ...s, skipped: skippedBefore + skippedHere }));
            } else if (event.event === "failed") {
              throw new Error(
                (JSON.parse(event.data) as Problem).detail ?? "Reading the topic failed",
              );
            }
          }
          skippedBefore += skippedHere;
        } catch (error) {
          // Stopped, or the page went.
          if (controller.signal.aborted) {
            return;
          }
          if (
            error instanceof ApiError &&
            error.status >= 400 &&
            error.status < 500 &&
            error.status !== 429
          ) {
            setState((s) => ({
              ...s,
              status: "failed",
              error: error.problem?.detail ?? error.message,
            }));
            return;
          }
          failures++;
          setState((s) => ({ ...s, status: "connecting" }));
          await sleep(Math.min(30_000, 1_000 * 2 ** failures));
        }
      }
    })();
    return () => {
      controller.abort();
    };
  }, [getToken, topic, running]);
  return running ? state : { ...state, status: state.status === "failed" ? "failed" : "idle" };
}

import { useEffect, useState } from "react";

/**
 * The time now, in milliseconds, which moves on every interval, so that
 * what says how long ago or for how long stays true while it shows.
 */
export function useNow(interval = 15_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => {
      setNow(Date.now());
    }, interval);
    return () => {
      clearInterval(timer);
    };
  }, [interval]);
  return now;
}

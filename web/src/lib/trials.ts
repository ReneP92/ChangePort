import type { NewTrialRequest, TrialRequest, TrialRetention, TrialThroughput } from "@/api/client";

/** The rough sizes a request gives, as people read them. */
export const throughputs: { value: TrialThroughput; label: string }[] = [
  { value: "under-1-mbps", label: "Under 1 MB/s" },
  { value: "1-10-mbps", label: "1 to 10 MB/s" },
  { value: "10-100-mbps", label: "10 to 100 MB/s" },
  { value: "over-100-mbps", label: "Over 100 MB/s" },
];

export const retentions: { value: TrialRetention; label: string }[] = [
  { value: "up-to-1-day", label: "Up to a day" },
  { value: "up-to-7-days", label: "Up to a week" },
  { value: "up-to-30-days", label: "Up to a month" },
  { value: "over-30-days", label: "Longer than a month" },
];

export const throughputLabel = (value: TrialThroughput) =>
  throughputs.find((option) => option.value === value)?.label ?? value;

export const retentionLabel = (value: TrialRetention) =>
  retentions.find((option) => option.value === value)?.label ?? value;

/**
 * How long a declined user waits before asking again, as the console's API
 * enforces it (declineWait in internal/console/trials.go).
 */
const declineWaitMs = 7 * 24 * 60 * 60 * 1000;

/** When the user of a declined request may ask again. */
export function askAgainFrom(request: TrialRequest): Date | undefined {
  if (request.phase !== "Declined" || !request.decidedAt) {
    return undefined;
  }
  return new Date(Date.parse(request.decidedAt) + declineWaitMs);
}

/**
 * How many characters text has as the console's API and Kubernetes count
 * them for their limits: in Unicode code points.
 */
export function characters(text: string): number {
  return Array.from(text).length;
}

/** A request form as people fill it in; the sizes start unchosen. */
export interface TrialDraft {
  company: string;
  role: string;
  useCase: string;
  throughput?: TrialThroughput | undefined;
  retention?: TrialRetention | undefined;
  preferredStart: string;
}

export const emptyDraft: TrialDraft = {
  company: "",
  role: "",
  useCase: "",
  preferredStart: "",
};

/** The problems of a draft by field, as the console's API would find them. */
export function draftProblems(
  draft: TrialDraft,
  today: string,
): Partial<Record<keyof TrialDraft, string>> {
  const problems: Partial<Record<keyof TrialDraft, string>> = {};
  // What is missing says what to fill in; what is too long, the limit.
  const check = (text: string, limit: number, missing: string) => {
    const length = characters(text.trim());
    if (length === 0) {
      return missing;
    }
    return length > limit ? `Use at most ${limit.toLocaleString("en-US")} characters.` : undefined;
  };
  const company = check(draft.company, 100, "Name your company.");
  const role = check(draft.role, 100, "Give your role.");
  const useCase = check(draft.useCase, 2000, "Say what you want Kafka for.");
  if (company) {
    problems.company = company;
  }
  if (role) {
    problems.role = role;
  }
  if (useCase) {
    problems.useCase = useCase;
  }
  if (!draft.throughput) {
    problems.throughput = "Choose roughly how much you write at peak.";
  }
  if (!draft.retention) {
    problems.retention = "Choose roughly how long you keep messages.";
  }
  if (draft.preferredStart !== "" && draft.preferredStart < today) {
    problems.preferredStart = "Choose today or a later day, or leave it empty.";
  }
  return problems;
}

/** The request a draft makes, once its sizes are chosen. */
export function toRequest(draft: TrialDraft): NewTrialRequest | undefined {
  if (!draft.throughput || !draft.retention) {
    return undefined;
  }
  const request: NewTrialRequest = {
    company: draft.company.trim(),
    role: draft.role.trim(),
    useCase: draft.useCase.trim(),
    throughput: draft.throughput,
    retention: draft.retention,
  };
  return draft.preferredStart === ""
    ? request
    : { ...request, preferredStart: draft.preferredStart };
}

/** Today in the user's time zone, as an input of type date writes days. */
export function localToday(now: Date = new Date()): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${String(now.getFullYear())}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
}

/** A day or an instant as people read it, such as 12 November 2026. */
export function formatDay(day: string | Date): string {
  const date = typeof day === "string" && day.length === 10 ? new Date(`${day}T00:00:00`) : day;
  return new Date(date).toLocaleDateString(undefined, {
    day: "numeric",
    month: "long",
    year: "numeric",
  });
}

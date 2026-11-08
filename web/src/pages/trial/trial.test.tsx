import { afterEach, describe, expect, mock, test } from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ApiError, type NewTrialRequest, type TrialRequest } from "@/api/client";
import { askAgainFrom, characters, draftProblems, emptyDraft } from "@/lib/trials";
import { TrialForm } from "@/pages/trial/TrialForm";
import { DeclinedRequest, PendingRequest } from "@/pages/trial/TrialStatus";

afterEach(cleanup);

const today = "2026-11-08";

const pending: TrialRequest = {
  id: "tr-abcdef",
  userID: "user_2ada",
  email: "ada@example.com",
  company: "Hooli",
  role: "Staff engineer",
  useCase: "Order events between checkout and fulfilment",
  throughput: "1-10-mbps",
  retention: "up-to-7-days",
  preferredStart: "2026-12-01",
  phase: "Pending",
  createdAt: "2026-11-08T09:00:00Z",
};

/** Fills the form in as someone would, every field but the start. */
function fillIn() {
  fireEvent.change(screen.getByLabelText("Company"), { target: { value: "  Hooli  " } });
  fireEvent.change(screen.getByLabelText("Your role"), { target: { value: "Staff engineer" } });
  fireEvent.change(screen.getByLabelText("What will you use Kafka for?"), {
    target: { value: "Order events" },
  });
  fireEvent.click(screen.getByRole("radio", { name: "1 to 10 MB/s" }));
  fireEvent.click(screen.getByRole("radio", { name: "Up to a week" }));
}

describe("TrialForm", () => {
  test("says what is missing instead of sending an incomplete request", () => {
    const onSubmit = mock((request: NewTrialRequest) => request);
    render(<TrialForm onSubmit={onSubmit} pending={false} today={today} />);
    // Nothing is flagged before the first try.
    expect(screen.queryByText("Choose roughly how much you write at peak.")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Request a trial" }));
    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.getByText("Name your company.")).toBeDefined();
    expect(screen.getByText("Give your role.")).toBeDefined();
    expect(screen.getByText("Say what you want Kafka for.")).toBeDefined();
    expect(screen.getByText("Choose roughly how much you write at peak.")).toBeDefined();
    expect(screen.getByText("Choose roughly how long you keep messages.")).toBeDefined();
    expect(screen.getByLabelText("Company").getAttribute("aria-invalid")).toBe("true");
  });

  test("sends the request, trimmed, without a start unless one is chosen", () => {
    const onSubmit = mock((request: NewTrialRequest) => request);
    render(<TrialForm onSubmit={onSubmit} pending={false} today={today} />);
    fillIn();
    fireEvent.click(screen.getByRole("button", { name: "Request a trial" }));
    expect(onSubmit).toHaveBeenCalledWith({
      company: "Hooli",
      role: "Staff engineer",
      useCase: "Order events",
      throughput: "1-10-mbps",
      retention: "up-to-7-days",
    });
  });

  test("refuses a start in the past, and sends a chosen one", () => {
    const onSubmit = mock((request: NewTrialRequest) => request);
    render(<TrialForm onSubmit={onSubmit} pending={false} today={today} />);
    fillIn();
    const start = screen.getByLabelText("When you would like to start (optional)");
    expect(start.getAttribute("min")).toBe(today);
    fireEvent.change(start, { target: { value: "2026-11-01" } });
    fireEvent.click(screen.getByRole("button", { name: "Request a trial" }));
    expect(onSubmit).not.toHaveBeenCalled();
    expect(screen.getByText("Choose today or a later day, or leave it empty.")).toBeDefined();
    fireEvent.change(start, { target: { value: "2026-12-01" } });
    fireEvent.click(screen.getByRole("button", { name: "Request a trial" }));
    expect(onSubmit.mock.calls[0]?.[0].preferredStart).toBe("2026-12-01");
  });

  test("shows the API's refusal by its field, or on its own", () => {
    const { rerender } = render(
      <TrialForm
        onSubmit={() => undefined}
        pending={false}
        today={today}
        error={
          new ApiError(400, {
            status: 400,
            title: "Invalid trial request",
            detail: "Name your company in 1 to 100 characters.",
            field: "company",
          })
        }
      />,
    );
    expect(screen.getByText("Name your company in 1 to 100 characters.")).toBeDefined();
    expect(screen.queryByRole("alert")).toBeNull();
    rerender(
      <TrialForm
        onSubmit={() => undefined}
        pending
        today={today}
        error={
          new ApiError(409, {
            status: 409,
            title: "You have a pending request",
            detail: "Your trial request tr-abcdef waits for an operator's decision.",
          })
        }
      />,
    );
    expect(screen.getByRole("alert").textContent).toBe(
      "Your trial request tr-abcdef waits for an operator's decision.",
    );
    const button = screen.getByRole("button", { name: "Requesting" });
    expect(button.hasAttribute("disabled")).toBe(true);
  });
});

describe("the request's status", () => {
  test("a pending request says what it asked for", () => {
    render(<PendingRequest request={pending} />);
    expect(screen.getByRole("heading", { name: "Your request is in" })).toBeDefined();
    const summary = screen.getByText("Peak throughput").closest("dl");
    expect(summary?.textContent).toContain("Hooli");
    expect(summary?.textContent).toContain("1 to 10 MB/s");
    expect(summary?.textContent).toContain("Up to a week");
    expect(summary?.textContent).toContain("Order events between checkout and fulfilment");
  });

  test("a declined request gives the reason, and when to ask again", () => {
    const declined: TrialRequest = {
      ...pending,
      phase: "Declined",
      decidedAt: "2026-11-08T10:00:00Z",
      reason: "We start with teams in the EU.",
    };
    const { rerender } = render(
      <DeclinedRequest request={declined} now={new Date("2026-11-09T00:00:00Z")} />,
    );
    expect(screen.getByRole("heading", { name: "Your request was declined" })).toBeDefined();
    expect(screen.getByText("We start with teams in the EU.")).toBeDefined();
    expect(screen.getByText(/You may ask again from/)).toBeDefined();
    // A week later, there is nothing to wait for.
    rerender(<DeclinedRequest request={declined} now={new Date("2026-11-16T00:00:00Z")} />);
    expect(screen.queryByText(/You may ask again from/)).toBeNull();
  });
});

describe("trial rules", () => {
  test("a declined user may ask again a week after the decision", () => {
    expect(
      askAgainFrom({ ...pending, phase: "Declined", decidedAt: "2026-11-08T10:00:00Z" }),
    ).toEqual(new Date("2026-11-15T10:00:00Z"));
    expect(askAgainFrom(pending)).toBeUndefined();
  });

  test("limits count characters as the API does, not bytes", () => {
    expect(characters("é".repeat(100))).toBe(100);
    const problems = draftProblems(
      {
        ...emptyDraft,
        company: "é".repeat(100),
        role: "r".repeat(101),
        useCase: "u",
        throughput: "under-1-mbps",
        retention: "up-to-1-day",
      },
      today,
    );
    expect(problems).toEqual({ role: "Use at most 100 characters." });
  });
});

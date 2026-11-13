import { afterEach, describe, expect, mock, test } from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import type { Customer, TrialChange } from "@/api/client";
import { TrialNotice } from "@/components/TrialNotice";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { endOn, trialEnding, trialLeft } from "@/lib/trials";
import { ConvertTrialForm, ExtendTrialForm } from "@/pages/operator/TrialChangeDialogs";

afterEach(cleanup);

const now = new Date("2026-11-08T12:00:00");
const endsAt = new Date("2026-11-20T09:00:00").toISOString();

const customer: Customer = {
  id: "c-7f3k2q",
  displayName: "Hooli",
  environment: "production",
  profile: "dev",
  ready: true,
  deletionProtection: false,
  createdAt: "2026-11-06T09:00:00Z",
  status: { reason: "Done", message: "Clients can connect" },
  conditions: [],
  trial: { endsAt },
};

function inDialog(children: ReactNode) {
  return (
    <Dialog open>
      <DialogContent>{children}</DialogContent>
    </Dialog>
  );
}

describe("TrialNotice", () => {
  test("says when a trial ends, quietly while it runs", () => {
    render(<TrialNotice trial={{ endsAt }} now={now} />);
    const notice = screen.getByRole("status");
    expect(notice.textContent).toContain("Trial: 12 days left, until");
    expect(notice.className).not.toContain("destructive");
  });

  test("warns in a trial's last three days that the cluster then goes", () => {
    render(<TrialNotice trial={{ endsAt }} now={new Date("2026-11-18T12:00:00")} />);
    const notice = screen.getByRole("status");
    expect(notice.textContent).toContain("Your trial ends in 2 days");
    expect(notice.textContent).toContain("Your cluster and its data are then deleted");
    expect(notice.className).toContain("destructive");
  });
});

describe("trial times", () => {
  test("count whole days left, and the warning's three days", () => {
    expect(trialLeft(endsAt, now)).toBe("12 days");
    expect(trialLeft(endsAt, new Date("2026-11-19T12:00:00"))).toBe("1 day");
    expect(trialLeft(endsAt, new Date("2026-11-21T00:00:00"))).toBe("under a day");
    expect(trialEnding(endsAt, now)).toBe(false);
    expect(trialEnding(endsAt, new Date("2026-11-17T09:00:00"))).toBe(true);
  });

  test("an extended trial ends at the time of day it ended before", () => {
    expect(new Date(endOn("2026-12-01", endsAt))).toEqual(new Date("2026-12-01T09:00:00"));
  });
});

describe("ExtendTrialForm", () => {
  test("proposes a week after the end, and sends the new end", () => {
    const onChange = mock((change: TrialChange) => change);
    render(
      inDialog(
        <ExtendTrialForm
          customer={customer}
          trial={{ endsAt }}
          pending={false}
          error={null}
          onChange={onChange}
          today="2026-11-08"
        />,
      ),
    );
    expect(screen.getByRole("heading", { name: "Extend Hooli's trial" })).toBeDefined();
    const day = screen.getByLabelText("New end");
    expect(day.getAttribute("value")).toBe("2026-11-27");
    expect(day.getAttribute("min")).toBe("2026-11-09");
    expect(day.getAttribute("max")).toBe("2027-02-05");
    fireEvent.click(screen.getByRole("button", { name: "Extend" }));
    expect(onChange).toHaveBeenCalledWith({
      action: "extend",
      endsAt: new Date("2026-11-27T09:00:00").toISOString(),
    });
  });

  test("refuses a day outside what the API allows", () => {
    const onChange = mock((change: TrialChange) => change);
    render(
      inDialog(
        <ExtendTrialForm
          customer={customer}
          trial={{ endsAt }}
          pending={false}
          error={null}
          onChange={onChange}
          today="2026-11-08"
        />,
      ),
    );
    fireEvent.change(screen.getByLabelText("New end"), { target: { value: "2026-11-08" } });
    expect(screen.getByText(/Choose a day from tomorrow to/)).toBeDefined();
    expect(screen.getByRole("button", { name: "Extend" }).hasAttribute("disabled")).toBe(true);
  });
});

describe("ConvertTrialForm", () => {
  test("converts the customer", () => {
    const onChange = mock((change: TrialChange) => change);
    render(
      inDialog(
        <ConvertTrialForm customer={customer} pending={false} error={null} onChange={onChange} />,
      ),
    );
    expect(screen.getByRole("heading", { name: "Convert Hooli?" })).toBeDefined();
    fireEvent.click(screen.getByRole("button", { name: "Convert" }));
    expect(onChange).toHaveBeenCalledWith({ action: "convert" });
  });
});

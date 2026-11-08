import { afterEach, describe, expect, mock, test } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { ApiError, createApiClient, type TrialRequest } from "@/api/client";
import { ApiContext } from "@/api/queries";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import { TrialRequestItem } from "@/pages/operator/OperatorTrialRequests";
import { ApproveTrialForm, DeclineTrialForm } from "@/pages/operator/TrialDecisionDialogs";

afterEach(cleanup);

const request: TrialRequest = {
  id: "tr-abcdef",
  userID: "user_2ada",
  email: "ada@example.com",
  company: "Hooli",
  role: "Staff engineer",
  useCase: "Order events between checkout and fulfilment",
  throughput: "1-10-mbps",
  retention: "up-to-7-days",
  phase: "Pending",
  createdAt: "2026-11-08T09:00:00Z",
};

function inDialog(children: ReactNode) {
  return (
    <Dialog open>
      <DialogContent>{children}</DialogContent>
    </Dialog>
  );
}

/** Renders what needs the console's API, which nothing here calls. */
function withApi(children: ReactNode) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      <ApiContext value={createApiClient(() => Promise.resolve(null))}>
        <MemoryRouter>{children}</MemoryRouter>
      </ApiContext>
    </QueryClientProvider>
  );
}

describe("ApproveTrialForm", () => {
  test("approves on dev unless another profile is chosen", () => {
    const onApprove = mock((profile: string) => profile);
    render(inDialog(<ApproveTrialForm request={request} pending={false} onApprove={onApprove} />));
    expect(screen.getByRole("heading", { name: "Approve Hooli's trial?" })).toBeDefined();
    expect(screen.getByText(/makes ada@example.com its admin/)).toBeDefined();
    expect(screen.getByRole("radio", { name: /^dev One broker/ }).getAttribute("data-state")).toBe(
      "checked",
    );
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    expect(onApprove).toHaveBeenLastCalledWith("dev");
    fireEvent.click(screen.getByRole("radio", { name: /^production/ }));
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    expect(onApprove).toHaveBeenLastCalledWith("production");
  });

  test("shows why the API refused", () => {
    render(
      inDialog(
        <ApproveTrialForm
          request={request}
          pending={false}
          onApprove={() => undefined}
          error={
            new ApiError(409, {
              status: 409,
              title: "Decided meanwhile",
              detail: "Trial request tr-abcdef changed while you decided.",
            })
          }
        />,
      ),
    );
    expect(screen.getByRole("alert").textContent).toBe(
      "Trial request tr-abcdef changed while you decided.",
    );
  });
});

describe("DeclineTrialForm", () => {
  test("needs a reason, which it sends trimmed", () => {
    const onDecline = mock((reason: string) => reason);
    render(inDialog(<DeclineTrialForm request={request} pending={false} onDecline={onDecline} />));
    const decline = screen.getByRole("button", { name: "Decline" });
    expect(decline.hasAttribute("disabled")).toBe(true);
    fireEvent.change(screen.getByLabelText("Reason"), { target: { value: "   " } });
    expect(decline.hasAttribute("disabled")).toBe(true);
    fireEvent.change(screen.getByLabelText("Reason"), {
      target: { value: " We start with teams in the EU. " },
    });
    fireEvent.click(decline);
    expect(onDecline).toHaveBeenCalledWith("We start with teams in the EU.");
  });

  test("refuses a reason over 1,000 characters", () => {
    render(inDialog(<DeclineTrialForm request={request} pending={false} onDecline={() => {}} />));
    fireEvent.change(screen.getByLabelText("Reason"), { target: { value: "r".repeat(1001) } });
    expect(screen.getByText("Use at most 1,000 characters.")).toBeDefined();
    expect(screen.getByRole("button", { name: "Decline" }).hasAttribute("disabled")).toBe(true);
  });
});

describe("TrialRequestItem", () => {
  test("offers a pending request's decisions", () => {
    render(withApi(<TrialRequestItem request={request} />));
    const item = screen.getByRole("listitem", { name: "Hooli, tr-abcdef" });
    expect(item.textContent).toContain("ada@example.com·Staff engineer");
    expect(item.textContent).toContain("1 to 10 MB/s");
    expect(item.textContent).toContain("As soon as possible");
    expect(screen.getByRole("button", { name: "Approve" })).toBeDefined();
    expect(screen.getByRole("button", { name: "Decline" })).toBeDefined();
  });

  test("shows a decision instead, linking an approval to its customer", () => {
    const { rerender } = render(
      withApi(
        <TrialRequestItem
          request={{
            ...request,
            phase: "Approved",
            decidedAt: "2026-11-08T10:00:00Z",
            customerID: "c-7f3k2q",
          }}
        />,
      ),
    );
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
    expect(screen.getByRole("link", { name: "c-7f3k2q" }).getAttribute("href")).toBe(
      "/operator/customers/c-7f3k2q",
    );
    rerender(
      withApi(
        <TrialRequestItem
          request={{
            ...request,
            phase: "Declined",
            decidedAt: "2026-11-08T10:00:00Z",
            reason: "Not yet",
          }}
        />,
      ),
    );
    expect(screen.getByText("Declined")).toBeDefined();
    expect(screen.getByText("Not yet")).toBeDefined();
  });
});

import { afterEach, describe, expect, mock, test } from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { ApiError, type Environment } from "@/api/client";
import { Dialog, DialogContent } from "@/components/ui/dialog";
import {
  AddEnvironmentForm,
  ConfirmByNameForm,
  EnvironmentSummary,
  RemoveRefused,
  RenameForm,
} from "@/pages/environments/EnvironmentsPage";

afterEach(cleanup);

function inDialog(children: ReactNode) {
  return (
    <Dialog open>
      <DialogContent>{children}</DialogContent>
    </Dialog>
  );
}

const environment = (more: Partial<Environment> = {}): Environment => ({
  name: "staging",
  customerID: "c-staging",
  profile: "dev-ha",
  ready: true,
  deleting: false,
  default: false,
  deletionProtection: false,
  trial: false,
  ...more,
});

describe("AddEnvironmentForm", () => {
  test("suggests a name the organization lacks, and protects production", () => {
    const onAdd = mock((environment: unknown) => environment);
    render(inDialog(<AddEnvironmentForm taken={["production"]} pending={false} onAdd={onAdd} />));
    const name = screen.getByRole<HTMLInputElement>("textbox", { name: "Name" });
    expect(name.value).toBe("staging");
    fireEvent.click(screen.getByRole("button", { name: "Add environment" }));
    expect(onAdd).toHaveBeenLastCalledWith({
      environment: "staging",
      profile: "dev",
      deletionProtection: false,
    });
    fireEvent.change(name, { target: { value: " live " } });
    fireEvent.click(screen.getByRole("radio", { name: /^production/ }));
    expect(
      screen.getByRole("checkbox", { name: /Deletion protection/ }).getAttribute("data-state"),
    ).toBe("checked");
    fireEvent.click(screen.getByRole("button", { name: "Add environment" }));
    expect(onAdd).toHaveBeenLastCalledWith({
      environment: "live",
      profile: "production",
      deletionProtection: true,
    });
  });

  test("shows the API's problem with the name by the name", () => {
    render(
      inDialog(
        <AddEnvironmentForm
          taken={["production", "staging", "dev"]}
          pending={false}
          onAdd={() => undefined}
          error={
            new ApiError(409, {
              status: 409,
              title: "Environment exists",
              detail: "Your organization has an environment qa already.",
              field: "environment",
            })
          }
        />,
      ),
    );
    expect(screen.getByText("Your organization has an environment qa already.")).toBeDefined();
    expect(screen.getByRole("textbox", { name: "Name" }).getAttribute("aria-invalid")).toBe("true");
    expect(screen.queryByRole("alert")).toBeNull();
    // Every suggestion is taken: no name is offered.
    expect(screen.getByRole<HTMLInputElement>("textbox", { name: "Name" }).value).toBe("");
    expect(
      screen.getByRole<HTMLButtonElement>("button", { name: "Add environment" }).disabled,
    ).toBe(true);
  });
});

describe("RenameForm", () => {
  test("renames to a new name only", () => {
    const onRename = mock((name: string) => name);
    render(inDialog(<RenameForm current="staging" pending={false} onRename={onRename} />));
    const button = screen.getByRole<HTMLButtonElement>("button", { name: "Rename" });
    expect(button.disabled).toBe(true);
    fireEvent.change(screen.getByRole("textbox", { name: "New name" }), {
      target: { value: "qa " },
    });
    fireEvent.click(button);
    expect(onRename).toHaveBeenLastCalledWith("qa");
  });
});

describe("ConfirmByNameForm", () => {
  test("waits for the environment's name", () => {
    const onConfirm = mock(() => undefined);
    render(
      inDialog(
        <ConfirmByNameForm
          name="staging"
          title="Remove staging?"
          description="Its cluster is deleted."
          action="Remove"
          pendingAction="Removing"
          destructive
          pending={false}
          onConfirm={onConfirm}
          error={new ApiError(409, { status: 409, title: "Deletion protection is on" })}
        />,
      ),
    );
    const button = screen.getByRole<HTMLButtonElement>("button", { name: "Remove" });
    const typed = screen.getByRole("textbox", { name: "Type staging to confirm" });
    fireEvent.change(typed, { target: { value: "stagin" } });
    expect(button.disabled).toBe(true);
    fireEvent.change(typed, { target: { value: "staging" } });
    fireEvent.click(button);
    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("alert").textContent).toBe("Deletion protection is on");
  });
});

describe("RemoveRefused", () => {
  test("says why", () => {
    render(inDialog(<RemoveRefused name="production" last />));
    expect(screen.getByText(/keeps at least one environment/)).toBeDefined();
    cleanup();
    render(inDialog(<RemoveRefused name="production" last={false} />));
    expect(screen.getByText(/deletion protection is on/i)).toBeDefined();
  });
});

describe("EnvironmentSummary", () => {
  const summary = (environment: Environment) =>
    render(
      <MemoryRouter>
        <EnvironmentSummary environment={environment} />
      </MemoryRouter>,
    );

  test("links to the environment, with its state and profile", () => {
    summary(environment({ default: true, deletionProtection: true }));
    expect(screen.getByRole("link", { name: "staging" }).getAttribute("href")).toBe("/e/staging");
    for (const badge of ["Ready", "Default", "Protected"]) {
      expect(screen.getByText(badge)).toBeDefined();
    }
    expect(screen.getByText("dev-ha: Three brokers in three zones, small")).toBeDefined();
  });

  test("one being removed is no link", () => {
    summary(environment({ deleting: true, ready: false }));
    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.getByText("Being removed")).toBeDefined();
  });
});

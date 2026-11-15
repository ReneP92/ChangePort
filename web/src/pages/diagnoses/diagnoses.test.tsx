import { afterEach, describe, expect, test } from "bun:test";
import { cleanup, render } from "@testing-library/react";
import { WithCode } from "@/pages/diagnoses/Diagnoses";

afterEach(cleanup);

describe("WithCode", () => {
  test("shows what is between backticks as code that breaks only at spaces", () => {
    const { container } = render(
      <p>
        <WithCode text="Delete it with `kafka-consumer-groups --delete --group billing`, or set `group.instance.id`." />
      </p>,
    );
    expect(container.textContent).toBe(
      "Delete it with kafka-consumer-groups --delete --group billing, or set group.instance.id.",
    );
    const code = [...container.querySelectorAll("code")].map((c) => c.textContent);
    expect(code).toEqual(["kafka-consumer-groups --delete --group billing", "group.instance.id"]);
    expect(container.querySelectorAll("code span.whitespace-nowrap")).toHaveLength(5);
  });

  test("leaves text without backticks as it is", () => {
    const { container } = render(<p>{<WithCode text="Nothing to see." />}</p>);
    expect(container.innerHTML).toBe("<p>Nothing to see.</p>");
  });
});

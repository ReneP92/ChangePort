import { afterEach, describe, expect, test } from "bun:test";
import { cleanup, render, screen } from "@testing-library/react";
import { DocsLink } from "@/components/DocsLink";
import { testDocsUrl, withConfig } from "@/lib/withConfig";

afterEach(cleanup);

describe("DocsLink", () => {
  test("opens the page of the docs in a new tab", () => {
    render(withConfig(<DocsLink page="alerts.html#requests-fail">What to do</DocsLink>));
    const link = screen.getByRole("link", { name: /^What to do/ });
    expect(link.getAttribute("href")).toBe(`${testDocsUrl}/alerts.html#requests-fail`);
    expect(link.getAttribute("target")).toBe("_blank");
    expect(link.textContent).toContain("opens the docs in a new tab");
  });

  test("shows nothing without the docs' address or a page", () => {
    const { container } = render(
      withConfig(<DocsLink page="alerts.html">What to do</DocsLink>, {
        state: "ready",
        publishableKey: "pk_test",
      }),
    );
    expect(container.textContent).toBe("");
    cleanup();
    const without = render(withConfig(<DocsLink page={undefined}>What to do</DocsLink>));
    expect(without.container.textContent).toBe("");
  });
});

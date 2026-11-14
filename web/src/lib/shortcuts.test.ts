import { describe, expect, test } from "bun:test";
import { typing } from "@/lib/shortcuts";

/** The element of html that selector finds, outside the document, which other tests share. */
function element(html: string, selector: string): HTMLElement {
  const root = document.createElement("div");
  root.innerHTML = html;
  const found = root.querySelector<HTMLElement>(selector);
  if (!found) {
    throw new Error(`no ${selector}`);
  }
  return found;
}

describe("typing", () => {
  test("a field's keys are its own", () => {
    expect(typing(element("<input>", "input"))).toBe(true);
    expect(typing(element("<textarea></textarea>", "textarea"))).toBe(true);
    expect(typing(element('<div contenteditable="true"></div>', "div"))).toBe(true);
  });

  test("an open menu's or dialog's keys are its own", () => {
    expect(typing(element('<div role="menu" data-state="open"><a>x</a></div>', "a"))).toBe(true);
    expect(typing(element('<div role="dialog"><button>x</button></div>', "button"))).toBe(true);
  });

  test("a closing menu, which keeps the focus while it fades, lets keys through", () => {
    expect(typing(element('<div role="menu" data-state="closed"><a>x</a></div>', "a"))).toBe(false);
  });

  test("anywhere else keys are shortcuts", () => {
    expect(typing(element("<main><button>x</button></main>", "button"))).toBe(false);
    expect(typing(document.body)).toBe(false);
    expect(typing(null)).toBe(false);
  });
});

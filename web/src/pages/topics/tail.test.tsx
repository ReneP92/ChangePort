import { afterEach, describe, expect, test } from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { TailMessage } from "@/api/client";
import { MessageRow } from "@/pages/topics/TailCard";

afterEach(cleanup);

const message: TailMessage = {
  partition: 2,
  offset: 1234,
  timestamp: "2026-11-01T12:00:00Z",
  key: { format: "text", text: "order-7", size: 7, truncated: false },
  value: {
    format: "avro",
    text: '{"id":7,"total":"12.50"}',
    size: 14,
    truncated: false,
    schemaId: 4,
  },
  headers: [
    { key: "trace", value: { format: "binary", text: "fffe", size: 2, truncated: false } },
    { key: "empty" },
  ],
};

describe("MessageRow", () => {
  test("shows where and when a message is, and its key and value on one line", () => {
    render(<MessageRow message={message} />);
    const row = screen.getByRole("button");
    expect(row.textContent).toContain("Partition 2, offset 1,234");
    expect(row.textContent).toContain("Avro");
    expect(row.textContent).toContain("order-7");
    expect(row.textContent).toContain('{"id":7,"total":"12.50"}');
    expect(row.getAttribute("aria-expanded")).toBe("false");
  });

  test("opens to the whole message: JSON indented, binary as hex, and its headers", () => {
    render(<MessageRow message={message} />);
    fireEvent.click(screen.getByRole("button"));
    const texts = [...document.querySelectorAll("pre")].map((pre) => pre.textContent);
    expect(texts).toEqual(["order-7", '{\n  "id": 7,\n  "total": "12.50"\n}', "fffe"]);
    expect(document.body.textContent).toContain("Avro, schema 4, 14 B");
    expect(document.body.textContent).toContain("Header empty");
    expect(document.body.textContent).toContain("None");
  });

  test("says when only a value's start is shown, and shows binary on one line as hex", () => {
    render(
      <MessageRow
        message={{
          partition: 0,
          offset: 1,
          timestamp: message.timestamp,
          headers: [],
          value: { format: "binary", text: "00ff", size: 9000, truncated: true },
        }}
      />,
    );
    expect(screen.getByRole("button").textContent).toContain("0x00ff");
    fireEvent.click(screen.getByRole("button"));
    expect(document.body.textContent).toContain("Binary, 9.0 KB; only its start is shown");
  });
});

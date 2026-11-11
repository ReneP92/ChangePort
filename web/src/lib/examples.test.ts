import { describe, expect, test } from "bun:test";
import examples from "@/lib/connecting-examples.json";
import { extractExamples } from "@/lib/examples";

const guide = await Bun.file(new URL("../../../docs/connecting.md", import.meta.url)).text();

describe("the connection guide's examples", () => {
  test("are those of docs/connecting.md; bun run generate updates them", () => {
    expect(examples).toEqual(extractExamples(guide));
  });

  test("cover every client the guide shows, with how to install it", () => {
    expect(
      examples.map((e) => [e.language, e.syntax, "install" in e ? e.install : undefined]),
    ).toEqual([
      ["Java", "java", undefined],
      ["Python", "python", "pip install confluent-kafka"],
      ["Go", "go", "go get github.com/twmb/franz-go"],
      ["Node.js", "js", "npm install @confluentinc/kafka-javascript"],
      ["Command line", "bash", undefined],
    ]);
    for (const example of examples) {
      expect(example.code.length).toBeGreaterThan(0);
      expect(example.code.endsWith("\n")).toBe(false);
    }
  });

  test("take the first code block of each section of Examples only", () => {
    const markdown = [
      "## Settings",
      "### Not an example",
      "```bash",
      "echo settings",
      "```",
      "## Examples",
      "### Ruby",
      "With rdkafka",
      "(`gem install rdkafka`).",
      "```ruby",
      "puts 1",
      "",
      "```",
      "```ruby",
      "puts 2",
      "```",
      "## If you cannot connect",
      "```bash",
      "echo help",
      "```",
    ].join("\n");
    expect(extractExamples(markdown)).toEqual([
      { language: "Ruby", syntax: "ruby", install: "gem install rdkafka", code: "puts 1" },
    ]);
  });
});

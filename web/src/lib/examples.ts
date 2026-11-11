/**
 * The client examples of the connection guide, docs/connecting.md, which
 * hack/connecting-guide-test.sh runs against a cluster on kind: the console
 * shows the same code. bun run generate extracts them into
 * connecting-examples.json, and a unit test checks it is up to date.
 */
export interface Example {
  /** The heading of its section, such as "Python". */
  language: string;
  /** The code block's language, such as "python". */
  syntax: string;
  /** How to install the client, when the guide says, such as "pip install confluent-kafka". */
  install?: string;
  code: string;
}

/**
 * Takes each section of the guide's Examples, in order: its first code
 * block, and the command in parentheses before it that installs the client.
 */
export function extractExamples(markdown: string): Example[] {
  const examples: Example[] = [];
  let inExamples = false;
  let current: { language: string; prose: string[] } | null = null;
  let code: { syntax: string; lines: string[] } | null = null;
  for (const line of markdown.split("\n")) {
    if (code) {
      if (line === "```") {
        if (current) {
          const install = /\(`([^`]+)`\)/.exec(current.prose.join(" "))?.[1];
          examples.push({
            language: current.language,
            syntax: code.syntax,
            ...(install ? { install } : {}),
            code: code.lines.join("\n").trimEnd(),
          });
          current = null;
        }
        code = null;
      } else {
        code.lines.push(line);
      }
      continue;
    }
    if (line.startsWith("## ")) {
      inExamples = line === "## Examples";
      current = null;
    } else if (inExamples && line.startsWith("### ")) {
      current = { language: line.slice(4), prose: [] };
    } else if (line.startsWith("```")) {
      code = { syntax: line.slice(3), lines: [] };
    } else if (current) {
      current.prose.push(line);
    }
  }
  return examples;
}

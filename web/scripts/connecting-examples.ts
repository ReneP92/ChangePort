// Writes src/lib/connecting-examples.json, the client examples of the
// connection guide (docs/connecting.md) that the console shows, for
// bun run generate.
import { extractExamples } from "../src/lib/examples.ts";

const guide = await Bun.file(new URL("../../docs/connecting.md", import.meta.url)).text();
const examples = extractExamples(guide);
await Bun.write(
  new URL("../src/lib/connecting-examples.json", import.meta.url),
  JSON.stringify(examples, null, 2) + "\n",
);

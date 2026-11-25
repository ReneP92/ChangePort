import { CodeBlock } from "@/components/CodeBlock";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { Example } from "@/lib/examples";
import { exampleEnvironment } from "@/lib/onboarding";

/**
 * The variables the connection guide's examples read, for a cluster and a
 * topic, and the examples themselves, a tab per language.
 */
export function Examples({
  examples,
  bootstrap,
  username,
  topic,
}: {
  examples: Example[];
  bootstrap: string;
  username: string;
  topic: string;
}) {
  return (
    <>
      <CodeBlock label="Shell" code={exampleEnvironment(bootstrap, username, topic)} />
      <Tabs defaultValue={examples[0]?.language} className="gap-3">
        {/* The tabs scroll sideways on phones rather than the page. */}
        <div className="-mx-1 overflow-x-auto px-1 pb-1">
          <TabsList>
            {examples.map((example) => (
              <TabsTrigger key={example.language} value={example.language} className="px-2.5">
                {tabName(example.language)}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
        {examples.map((example) => (
          <TabsContent
            key={example.language}
            value={example.language}
            className="flex flex-col gap-3"
          >
            {example.install && <CodeBlock label="Install the client" code={example.install} />}
            <CodeBlock label={tabName(example.language)} code={example.code} />
          </TabsContent>
        ))}
      </Tabs>
    </>
  );
}

/** The JavaScript example runs on Node.js, which the guide's heading names. */
function tabName(language: string): string {
  return language === "Node.js" ? "JavaScript (Node.js)" : language;
}

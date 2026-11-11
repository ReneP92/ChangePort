import type { SourceSQL } from "@/api/client";
import { CodeBlock } from "@/components/CodeBlock";

/**
 * What the customer does to their database for a step: the steps outside
 * SQL first, then the SQL, which they copy and run.
 */
export function SqlBlock({ sql, label }: { sql: SourceSQL; label: string }) {
  return (
    <div className="flex flex-col gap-4 text-sm">
      {sql.steps.length > 0 && (
        <ol className="flex list-decimal flex-col gap-2 pl-5 marker:text-muted-foreground">
          {sql.steps.map((step) => (
            <li key={step} className="pl-1 leading-relaxed">
              {step}
            </li>
          ))}
        </ol>
      )}
      {sql.sql.trim() !== "" && <CodeBlock code={sql.sql} label={label} />}
    </div>
  );
}

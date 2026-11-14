import { errorText } from "@/api/client";
import { Link } from "react-router";
import { useSources } from "@/api/queries";
import { Page } from "@/components/Page";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { formatBytes } from "@/lib/format";
import { CustomerArea } from "@/pages/CustomerArea";
import { PhaseBadge } from "@/pages/sources/PhaseBadge";
import { useEnvironmentCrumb, useEnvironmentPath } from "@/lib/useEnvironment";

/** The customer's Postgres sources, and for admins adding one. */
export function Sources() {
  return <CustomerArea>{(_, admin) => <SourceList admin={admin} />}</CustomerArea>;
}

function SourceList({ admin }: { admin: boolean }) {
  const envPath = useEnvironmentPath();
  const crumbs = useEnvironmentCrumb();
  const sources = useSources();
  return (
    <Page
      crumbs={crumbs}
      title="Postgres sources"
      description="Each table of a source streams into a topic of its own: its existing rows first, then every insert, update and delete."
      actions={
        admin && (
          <Button asChild size="sm">
            <Link to={envPath("/sources/new")}>Add source</Link>
          </Button>
        )
      }
    >
      <Card>
        <CardHeader>
          <CardTitle>
            <h2>Sources</h2>
          </CardTitle>
          <CardDescription>
            The WAL a database keeps for a source grows while the source is paused or behind.
          </CardDescription>
        </CardHeader>
        <CardContent className="text-sm">
          {sources.isPending && <p className="text-muted-foreground">Loading sources</p>}
          {sources.isError && <p className="text-destructive">{errorText(sources.error)}</p>}
          {sources.data?.length === 0 && (
            <p className="text-muted-foreground">
              There are no sources yet.{" "}
              {admin
                ? "Add one to stream a Postgres database's tables into topics."
                : "Ask an admin of your organization to add one."}
            </p>
          )}
          {sources.data && sources.data.length > 0 && (
            // Wide tables scroll sideways on phones instead of overflowing.
            <div className="relative -mx-1 overflow-x-auto px-1">
              <table className="w-full text-left whitespace-nowrap [&_td]:px-3 [&_td:first-child]:pl-0 [&_td:last-child]:pr-0 [&_th]:px-3 [&_th:first-child]:pl-0 [&_th:last-child]:pr-0">
                <thead className="border-b text-muted-foreground">
                  <tr>
                    <th className="py-2 font-medium">Source</th>
                    <th className="py-2 font-medium">Status</th>
                    <th className="py-2 font-medium">Database</th>
                    <th className="py-2 text-right font-medium">Tables</th>
                    <th className="py-2 text-right font-medium">Retained WAL</th>
                  </tr>
                </thead>
                <tbody>
                  {sources.data.map((source) => (
                    <tr key={source.name} className="border-b last:border-0">
                      <td className="py-2 font-medium">
                        <Link
                          to={envPath(`/sources/${source.name}`)}
                          className="underline-offset-4 hover:underline"
                        >
                          {source.name}
                        </Link>
                      </td>
                      <td className="py-2">
                        <PhaseBadge source={source} />
                      </td>
                      <td className="py-2 text-muted-foreground">
                        {source.database} on {source.host}
                      </td>
                      <td className="py-2 text-right tabular-nums">{source.tables.length}</td>
                      <td className="py-2 text-right tabular-nums">
                        {source.slot ? formatBytes(source.slot.retainedWALBytes) : "–"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </Page>
  );
}

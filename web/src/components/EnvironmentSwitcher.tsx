import { Link, useNavigate } from "react-router";
import { useMe } from "@/api/queries";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { environmentPath } from "@/lib/environments";
import { useEnvironment } from "@/lib/useEnvironment";

/** The choice that opens the page of the organization's environments. */
const manage = "/environments";

/**
 * The environment the console shows, in the header: a choice of the
 * organization's environments, each a cluster of its own, which opens the
 * chosen one's overview, or their page; with one environment, just its
 * name, which opens their page.
 */
export function EnvironmentSwitcher() {
  const me = useMe();
  const environment = useEnvironment();
  const navigate = useNavigate();
  const environments = me.data?.access === "customer" ? (me.data.environments ?? []) : [];
  if (environments.length === 0 || environment === undefined) {
    return null;
  }
  if (environments.length === 1) {
    return (
      <Badge variant="outline" asChild>
        <Link to={manage} title="Your organization's environments">
          {environment}
        </Link>
      </Badge>
    );
  }
  return (
    <Select
      value={environment}
      onValueChange={(next) => {
        void navigate(next === manage ? manage : environmentPath(next, "/"));
      }}
    >
      <SelectTrigger size="sm" className="w-auto min-w-28" aria-label="Environment">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {environments.map((e) => (
          <SelectItem key={e.name} value={e.name}>
            {e.name}
          </SelectItem>
        ))}
        <SelectSeparator />
        <SelectItem value={manage}>Manage environments</SelectItem>
      </SelectContent>
    </Select>
  );
}

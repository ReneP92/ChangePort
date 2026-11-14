import { useNavigate } from "react-router";
import { useMe } from "@/api/queries";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useSidebar } from "@/components/ui/sidebar";
import { environmentPath } from "@/lib/environments";
import { useEnvironment } from "@/lib/useEnvironment";

/** The choice that opens the page of the organization's environments. */
const manage = "/environments";

/**
 * The environment the console shows, in the sidebar above its pages: a
 * choice of the organization's environments, each a cluster of its own,
 * which opens the chosen one's overview, or their page.
 */
export function EnvironmentSwitcher() {
  const me = useMe();
  const environment = useEnvironment();
  const navigate = useNavigate();
  const { isMobile, setOpenMobile } = useSidebar();
  const environments = me.data?.access === "customer" ? (me.data.environments ?? []) : [];
  if (environments.length === 0 || environment === undefined) {
    return null;
  }
  return (
    <Select
      value={environment}
      onValueChange={(next) => {
        if (isMobile) {
          setOpenMobile(false);
        }
        void navigate(next === manage ? manage : environmentPath(next, "/"));
      }}
    >
      <SelectTrigger
        aria-label="Environment"
        className="w-full bg-background group-data-[collapsible=icon]:hidden"
      >
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

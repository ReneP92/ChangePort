import { useOrganization, UserButton } from "@clerk/react";
import {
  Bell,
  Building2,
  Database,
  Inbox,
  LayoutDashboard,
  Layers,
  Menu,
  PanelLeft,
  Users,
  type LucideIcon,
} from "lucide-react";
import { Link, matchPath, NavLink, useLocation } from "react-router";
import { useAlerts, useMe, useTrialRequests } from "@/api/queries";
import { EnvironmentSwitcher } from "@/components/EnvironmentSwitcher";
import { Button } from "@/components/ui/button";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
  useSidebar,
} from "@/components/ui/sidebar";
import { useEnvironment, useEnvironmentPath } from "@/lib/useEnvironment";

/**
 * The console's sidebar, which reads from the outside in: the organization,
 * the environment, that environment's pages, then the whole organization's.
 * Operators get the operator console's pages instead. On phones it opens as
 * a sheet from the top bar's menu button; on wider screens it collapses to
 * icons.
 */
export function ConsoleSidebar() {
  const me = useMe();
  const access = me.data?.access;
  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton asChild size="lg" tooltip="Changeport">
              <Link to="/">
                <img src="/favicon.svg" alt="" className="size-6 shrink-0" />
                <span className="text-base font-semibold tracking-tight">Changeport</span>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <Organization />
        </SidebarMenu>
        {access === "customer" && <EnvironmentSwitcher />}
      </SidebarHeader>
      <SidebarContent>
        <nav aria-label={access === "operator" ? "Operator console" : "Console"}>
          {access === "customer" && <CustomerMenu />}
          {access === "operator" && <OperatorMenu />}
        </nav>
      </SidebarContent>
      <SidebarFooter>
        <SidebarMenu>
          <CollapseItem />
          <UserItem />
        </SidebarMenu>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}

/**
 * The top bar of phones, whose menu button opens the sidebar, and which
 * names the environment shown. Wider screens have the sidebar instead.
 */
export function MobileTopBar() {
  const { toggleSidebar } = useSidebar();
  const me = useMe();
  const environment = useEnvironment();
  return (
    <header className="sticky top-0 z-10 flex items-center gap-2 border-b bg-background px-4 py-2 md:hidden">
      <Button variant="ghost" size="icon-sm" aria-label="Open the menu" onClick={toggleSidebar}>
        <Menu />
      </Button>
      <Link to="/" className="flex items-center gap-2 font-semibold tracking-tight">
        <img src="/favicon.svg" alt="" className="size-6" />
        Changeport
      </Link>
      {me.data?.access === "customer" && environment !== undefined && (
        <span className="ml-auto truncate text-sm text-muted-foreground" title="The environment">
          {environment}
        </span>
      )}
    </header>
  );
}

/** The user's menu, with their name while the sidebar is wide. */
function UserItem() {
  const { state, isMobile } = useSidebar();
  return (
    <SidebarMenuItem className="flex h-10 items-center px-1 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0">
      <UserButton showName={state === "expanded" || isMobile} />
    </SidebarMenuItem>
  );
}

/**
 * The user's organization: its avatar and name, which lead a customer to
 * their team. A user belongs to one organization, so there is nothing to
 * switch to.
 */
function Organization() {
  const { organization } = useOrganization();
  const me = useMe();
  if (!organization) {
    return null;
  }
  const name = (
    <>
      <img src={organization.imageUrl} alt="" className="size-4 shrink-0 rounded" />
      <span className="truncate">{organization.name}</span>
    </>
  );
  return (
    <SidebarMenuItem>
      {me.data?.access === "customer" ? (
        <SidebarMenuButton asChild tooltip={organization.name}>
          <Link to="/team" title="Your organization's team">
            {name}
          </Link>
        </SidebarMenuButton>
      ) : (
        <div className="flex h-8 min-w-0 items-center gap-2 px-2 text-sm group-data-[collapsible=icon]:[&>span]:hidden">
          {name}
        </div>
      )}
    </SidebarMenuItem>
  );
}

/**
 * The pages of the environment the console shows, then those of the whole
 * organization. A topic's and a consumer group's pages belong to the
 * overview, which lists them.
 */
function CustomerMenu() {
  const envPath = useEnvironmentPath();
  const alerts = useAlerts();
  const { pathname } = useLocation();
  const inEnvironment = (page: string) =>
    matchPath({ path: `/e/:environment${page}`, end: false }, pathname) !== null;
  const overview =
    matchPath({ path: "/e/:environment", end: true }, pathname) !== null ||
    ["/topics", "/consumer-groups", "/welcome"].some(inEnvironment);
  return (
    <>
      <SidebarGroup>
        <SidebarGroupLabel>Environment</SidebarGroupLabel>
        <SidebarGroupContent>
          <SidebarMenu>
            <Item to={envPath("/")} icon={LayoutDashboard} label="Overview" active={overview} />
            <Item
              to={envPath("/sources")}
              icon={Database}
              label="Sources"
              active={inEnvironment("/sources")}
            />
            <Item
              to={envPath("/alerts")}
              icon={Bell}
              label="Alerts"
              active={inEnvironment("/alerts")}
              count={{ value: alerts.data?.length ?? 0, label: "active" }}
            />
          </SidebarMenu>
        </SidebarGroupContent>
      </SidebarGroup>
      <SidebarGroup>
        <SidebarGroupLabel>Organization</SidebarGroupLabel>
        <SidebarGroupContent>
          <SidebarMenu>
            <Item
              to="/environments"
              icon={Layers}
              label="Environments"
              active={pathname === "/environments"}
            />
            <Item to="/team" icon={Users} label="Team" active={pathname === "/team"} />
          </SidebarMenu>
        </SidebarGroupContent>
      </SidebarGroup>
    </>
  );
}

/** The pages of the operator console. */
function OperatorMenu() {
  const { pathname } = useLocation();
  const requests = useTrialRequests();
  const pending = requests.data?.filter((request) => request.phase === "Pending").length ?? 0;
  return (
    <SidebarGroup>
      <SidebarGroupLabel>Operator console</SidebarGroupLabel>
      <SidebarGroupContent>
        <SidebarMenu>
          <Item
            to="/operator"
            icon={Building2}
            label="Customers"
            active={pathname === "/operator" || pathname.startsWith("/operator/customers/")}
          />
          <Item
            to="/operator/trials"
            icon={Inbox}
            label="Trial requests"
            active={pathname === "/operator/trials"}
            count={{ value: pending, label: "waiting" }}
          />
        </SidebarMenu>
      </SidebarGroupContent>
    </SidebarGroup>
  );
}

/** A page of the menu, which closes the sheet on phones once chosen. */
function Item({
  to,
  icon: Icon,
  label,
  active,
  count,
}: {
  to: string;
  icon: LucideIcon;
  label: string;
  active: boolean;
  /** A count by the link, such as of active alerts, read out with it. */
  count?: { value: number; label: string };
}) {
  const { isMobile, setOpenMobile } = useSidebar();
  const shown = count !== undefined && count.value > 0;
  return (
    <SidebarMenuItem>
      <SidebarMenuButton asChild isActive={active} tooltip={label}>
        <NavLink
          to={to}
          aria-current={active ? "page" : undefined}
          onClick={() => {
            if (isMobile) {
              setOpenMobile(false);
            }
          }}
        >
          <Icon />
          <span>{label}</span>
          {shown && (
            <span className="sr-only">
              , {count.value} {count.label}
            </span>
          )}
        </NavLink>
      </SidebarMenuButton>
      {shown && (
        <SidebarMenuBadge
          aria-hidden
          className="bg-destructive text-white peer-data-active/menu-button:text-white"
        >
          {count.value}
        </SidebarMenuBadge>
      )}
    </SidebarMenuItem>
  );
}

/** Collapses the sidebar to icons, or expands it, on wider screens. */
function CollapseItem() {
  const { state, toggleSidebar, isMobile } = useSidebar();
  if (isMobile) {
    return null;
  }
  const label = state === "expanded" ? "Collapse the menu" : "Expand the menu";
  return (
    <SidebarMenuItem>
      <SidebarMenuButton onClick={toggleSidebar} tooltip={label}>
        <PanelLeft />
        <span>{label}</span>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}

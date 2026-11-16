import { useClerk, useOrganization, useUser } from "@clerk/react";
import {
  Bell,
  BookOpen,
  Building2,
  ChartColumn,
  ChevronDown,
  ChevronRight,
  Database,
  Inbox,
  Keyboard,
  LayoutDashboard,
  Layers,
  LogOut,
  Menu,
  MessageCircle,
  Moon,
  PanelLeft,
  Settings2,
  UserRound,
  Users,
  type LucideIcon,
} from "lucide-react";
import { useState } from "react";
import { Link, matchPath, NavLink, useLocation } from "react-router";
import type { Environment } from "@/api/client";
import { useSupportEmail } from "@/api/config";
import { useEnvironmentAlerts, useMe, useTrialRequests } from "@/api/queries";
import { ChangeportMark } from "@/components/ChangeportMark";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupAction,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarRail,
  useSidebar,
} from "@/components/ui/sidebar";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { Skeleton } from "@/components/ui/skeleton";
import { environmentPath } from "@/lib/environments";
import { modifier, useOpenShortcuts } from "@/lib/shortcuts";
import { setTheme, useTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";
import { useEnvironment, useEnvironmentPath } from "@/lib/useEnvironment";

/**
 * The console's sidebar, after Linear's: the workspace menu, then the
 * organization's environments, each a section with its pages and its
 * alerts counted, the open one unfolded, then the organization's own
 * pages. Operators get the operator console's pages instead. On phones it
 * opens as a sheet from the top bar's menu button; on wider screens it
 * collapses to icons.
 */
export function ConsoleSidebar() {
  const me = useMe();
  const access = me.data?.access;
  return (
    <Sidebar collapsible="icon" variant="inset">
      <SidebarHeader>
        <SidebarMenu>
          <WorkspaceMenu />
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <nav aria-label={access === "operator" ? "Operator console" : "Console"}>
          {access === "customer" && <CustomerMenu environments={me.data?.environments ?? []} />}
          {access === "operator" && <OperatorMenu />}
        </nav>
      </SidebarContent>
      {/* Changeport's mark opens help, as Linear's does; beside it, or under
          it collapsed to icons, the menu collapses. */}
      <SidebarFooter className="flex-row items-center justify-between group-data-[collapsible=icon]:flex-col">
        <HelpMenu />
        <CollapseButton />
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
        <ChangeportMark className="size-6" />
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

/**
 * The organization, or Changeport without one, as Linear's workspace: its
 * menu leads to the organization's pages, the user's profile and signing
 * out. A user belongs to one organization, so there is nothing to switch
 * to.
 */
function WorkspaceMenu() {
  const { organization, isLoaded } = useOrganization();
  const { user } = useUser();
  const { openUserProfile, signOut } = useClerk();
  const me = useMe();
  const { isMobile, setOpenMobile } = useSidebar();
  const close = () => {
    if (isMobile) {
      setOpenMobile(false);
    }
  };
  const name = organization?.name ?? "Changeport";
  // Until Clerk has loaded the organization, its name and image wait, rather
  // than Changeport's showing for a moment.
  const mark = !isLoaded ? (
    <Skeleton className="size-5 shrink-0 rounded" />
  ) : organization ? (
    <img src={organization.imageUrl} alt="" className="size-5 shrink-0 rounded" />
  ) : (
    <ChangeportMark className="size-5" />
  );
  return (
    <SidebarMenuItem>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <SidebarMenuButton
            aria-label="Workspace menu"
            aria-busy={!isLoaded}
            tooltip={name}
            className="font-medium"
          >
            {mark}
            {isLoaded ? (
              <span className="truncate">{name}</span>
            ) : (
              <Skeleton className="h-4 w-28" />
            )}
            <ChevronDown className="ml-auto size-3.5 text-muted-foreground" />
          </SidebarMenuButton>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-60">
          <DropdownMenuLabel className="flex flex-col gap-0.5">
            <span className="truncate font-medium">{name}</span>
            <span className="truncate text-xs font-normal text-muted-foreground">
              {user?.primaryEmailAddress?.emailAddress}
            </span>
          </DropdownMenuLabel>
          {me.data?.access === "customer" && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuItem asChild>
                  <Link to="/environments" onClick={close}>
                    <Layers />
                    Environments
                  </Link>
                </DropdownMenuItem>
                <DropdownMenuItem asChild>
                  <Link to="/team" onClick={close}>
                    <Users />
                    Team
                  </Link>
                </DropdownMenuItem>
              </DropdownMenuGroup>
            </>
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onSelect={() => {
              openUserProfile();
            }}
          >
            <UserRound />
            Your profile
          </DropdownMenuItem>
          <DropdownMenuItem
            onSelect={() => {
              void signOut({ redirectUrl: "/" });
            }}
          >
            <LogOut />
            Sign out
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </SidebarMenuItem>
  );
}

/**
 * The organization's environments, each a section with the pages of its
 * cluster, then the organization's own pages. A topic's and a consumer
 * group's pages belong to the overview, which lists them.
 */
function CustomerMenu({ environments }: { environments: Environment[] }) {
  const current = useEnvironment();
  const { pathname } = useLocation();
  return (
    <>
      <SidebarGroup>
        <SidebarGroupLabel>Your environments</SidebarGroupLabel>
        <SidebarGroupAction asChild title="Manage environments">
          <Link to="/environments" aria-label="Manage environments">
            <Settings2 />
          </Link>
        </SidebarGroupAction>
        <SidebarGroupContent>
          <SidebarMenu>
            {environments.map((environment) => (
              <EnvironmentSection
                // A section unfolds when it becomes the open environment.
                key={`${environment.name}/${String(environment.name === current)}`}
                environment={environment}
                current={environment.name === current}
              />
            ))}
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

/**
 * An environment and the pages of its cluster, unfolded while it is the one
 * the console shows, with how many of its alerts are active.
 */
function EnvironmentSection({
  environment,
  current,
}: {
  environment: Environment;
  current: boolean;
}) {
  const [open, setOpen] = useState(current);
  const alerts = useEnvironmentAlerts(environment.name).data?.length ?? 0;
  const { pathname } = useLocation();
  const { isMobile, setOpenMobile, state } = useSidebar();
  const page = (path: string) =>
    current && matchPath({ path: `/e/:environment${path}`, end: path === "" }, pathname) !== null;
  const overview = page("") || ["/topics", "/consumer-groups", "/welcome"].some(page);
  const close = () => {
    if (isMobile) {
      setOpenMobile(false);
    }
  };
  const initial = environment.name.slice(0, 1).toUpperCase();
  // Collapsed to icons, an environment's icon opens its overview.
  if (state === "collapsed" && !isMobile) {
    return (
      <SidebarMenuItem>
        <SidebarMenuButton asChild isActive={current} tooltip={environment.name}>
          <NavLink to={environmentPath(environment.name, "/")}>
            <EnvironmentIcon initial={initial} />
            <span>{environment.name}</span>
          </NavLink>
        </SidebarMenuButton>
      </SidebarMenuItem>
    );
  }
  return (
    <Collapsible open={open} onOpenChange={setOpen} asChild>
      <SidebarMenuItem>
        <CollapsibleTrigger asChild>
          <SidebarMenuButton className="group/section">
            <EnvironmentIcon initial={initial} />
            <span className="truncate">{environment.name}</span>
            {!environment.ready && <span className="text-xs text-muted-foreground">starting</span>}
            <ChevronRight className="ml-auto size-3.5 text-muted-foreground transition-transform group-data-[state=open]/section:rotate-90" />
            {alerts > 0 && (
              <span className="sr-only">
                , {alerts} active {alerts === 1 ? "alert" : "alerts"}
              </span>
            )}
          </SidebarMenuButton>
        </CollapsibleTrigger>
        {!open && alerts > 0 && <Count value={alerts} />}
        <CollapsibleContent>
          <SidebarMenuSub aria-label={environment.name}>
            <SubItem
              to={environmentPath(environment.name, "/")}
              icon={LayoutDashboard}
              label="Overview"
              active={overview}
              onClick={close}
            />
            <SubItem
              to={environmentPath(environment.name, "/sources")}
              icon={Database}
              label="Sources"
              active={page("/sources")}
              onClick={close}
            />
            <SubItem
              to={environmentPath(environment.name, "/alerts")}
              icon={Bell}
              label="Alerts"
              active={page("/alerts")}
              onClick={close}
              count={{ value: alerts, label: "active" }}
            />
            <SubItem
              to={environmentPath(environment.name, "/usage")}
              icon={ChartColumn}
              label="Usage"
              active={page("/usage")}
              onClick={close}
            />
          </SidebarMenuSub>
        </CollapsibleContent>
      </SidebarMenuItem>
    </Collapsible>
  );
}

/** An environment's mark: its initial in a small square, as Linear's teams have. */
function EnvironmentIcon({ initial }: { initial: string }) {
  return (
    <span
      aria-hidden
      className="flex size-4 shrink-0 items-center justify-center rounded bg-sidebar-accent text-[0.625rem] font-semibold text-sidebar-accent-foreground ring-1 ring-sidebar-border"
    >
      {initial}
    </span>
  );
}

/** A page of an environment, which closes the sheet on phones once chosen. */
function SubItem({
  to,
  icon: Icon,
  label,
  active,
  onClick,
  count,
}: {
  to: string;
  icon: LucideIcon;
  label: string;
  active: boolean;
  onClick: () => void;
  count?: { value: number; label: string };
}) {
  const shown = count !== undefined && count.value > 0;
  return (
    <SidebarMenuSubItem>
      <SidebarMenuSubButton asChild isActive={active}>
        <NavLink to={to} aria-current={active ? "page" : undefined} onClick={onClick}>
          <Icon />
          <span>{label}</span>
          {shown && (
            <>
              <span className="sr-only">
                , {count.value} {count.label}
              </span>
              <span
                aria-hidden
                className="ml-auto flex h-4 min-w-4 items-center justify-center rounded-full bg-destructive px-1 text-[0.625rem] font-medium text-white tabular-nums dark:bg-destructive/60"
              >
                {count.value}
              </span>
            </>
          )}
        </NavLink>
      </SidebarMenuSubButton>
    </SidebarMenuSubItem>
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
  /** A count by the link, such as of trial requests waiting, read out with it. */
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
      {shown && <Count value={count.value} />}
    </SidebarMenuItem>
  );
}

/** A count by a menu row, which the row's link reads out. */
function Count({ value }: { value: number }) {
  return (
    <SidebarMenuBadge
      aria-hidden
      className="bg-destructive text-white peer-data-active/menu-button:text-white dark:bg-destructive/60"
    >
      {value}
    </SidebarMenuBadge>
  );
}

/** Collapses the sidebar to icons, or expands it, on wider screens. */
/**
 * Help, behind Changeport's mark as Linear's is behind its: the getting
 * started guide of the environment shown, the keyboard shortcuts, support's
 * address when the console has one, and the theme.
 */
function HelpMenu() {
  const me = useMe();
  const environmentPath = useEnvironmentPath();
  const openShortcuts = useOpenShortcuts();
  const supportEmail = useSupportEmail();
  const theme = useTheme();
  const { isMobile, setOpenMobile } = useSidebar();
  const customer = me.data?.access === "customer";
  const close = () => {
    if (isMobile) {
      setOpenMobile(false);
    }
  };
  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              aria-label="Help"
              className="bg-sidebar-accent hover:bg-sidebar-border aria-expanded:bg-sidebar-border"
            >
              <ChangeportMark className="size-5" />
            </Button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent side="right">Help</TooltipContent>
      </Tooltip>
      <DropdownMenuContent side="top" align="start" className="w-60">
        {customer && (
          <DropdownMenuItem asChild>
            <Link to={environmentPath("/welcome")} onClick={close}>
              <BookOpen />
              Getting started
            </Link>
          </DropdownMenuItem>
        )}
        {customer && supportEmail !== undefined && (
          <DropdownMenuItem asChild>
            <a href={`mailto:${supportEmail}`}>
              <MessageCircle />
              Contact support
            </a>
          </DropdownMenuItem>
        )}
        <DropdownMenuItem
          onSelect={() => {
            close();
            openShortcuts();
          }}
        >
          <Keyboard />
          Keyboard shortcuts
          <DropdownMenuShortcut>{modifier()} /</DropdownMenuShortcut>
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        {/* A switch, which keeps the menu open to show it flip. */}
        <DropdownMenuItem
          role="menuitemcheckbox"
          aria-checked={theme === "dark"}
          onSelect={(event) => {
            event.preventDefault();
            setTheme(theme === "dark" ? "light" : "dark");
          }}
        >
          <Moon />
          Dark mode
          <span
            aria-hidden
            className={cn(
              "ml-auto inline-flex h-4 w-7 shrink-0 items-center rounded-full p-0.5 transition-colors",
              theme === "dark" ? "bg-primary" : "bg-muted-foreground/60",
            )}
          >
            <span
              className={cn(
                "size-3 rounded-full bg-background shadow-sm transition-transform",
                theme === "dark" && "translate-x-3",
              )}
            />
          </span>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function CollapseButton() {
  const { state, toggleSidebar, isMobile } = useSidebar();
  if (isMobile) {
    return null;
  }
  const label = state === "expanded" ? "Collapse the menu" : "Expand the menu";
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          aria-label={label}
          onClick={toggleSidebar}
          className="text-muted-foreground hover:bg-sidebar-accent"
        >
          <PanelLeft />
        </Button>
      </TooltipTrigger>
      <TooltipContent side="right">{label}</TooltipContent>
    </Tooltip>
  );
}

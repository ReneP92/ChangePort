import { useOrganization } from "@clerk/react";
import { Link } from "react-router";
import { useMe } from "@/api/queries";

/**
 * The user's organization, in the header: its name, which leads a customer
 * to their team. A user belongs to one organization, so there is nothing
 * to switch to.
 */
export function OrganizationButton() {
  const { organization } = useOrganization();
  const me = useMe();
  if (!organization) {
    return null;
  }
  const name = (
    <>
      <img src={organization.imageUrl} alt="" className="size-5 shrink-0 rounded" />
      <span className="truncate">{organization.name}</span>
    </>
  );
  if (me.data?.access !== "customer") {
    return <span className="flex min-w-0 items-center gap-2 px-2 py-1 text-sm">{name}</span>;
  }
  return (
    <Link
      to="/team"
      title="Your organization's team"
      className="flex min-w-0 items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-muted"
    >
      {name}
    </Link>
  );
}

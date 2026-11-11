import { useClerk, useOrganization } from "@clerk/react";

/**
 * The user's organization, in the header: its name, which opens its
 * settings and members. A user belongs to one organization, so there is
 * nothing to switch to.
 */
export function OrganizationButton() {
  const { organization } = useOrganization();
  const clerk = useClerk();
  if (!organization) {
    return null;
  }
  return (
    <button
      type="button"
      title="Your organization's settings and members"
      className="flex min-w-0 items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-muted"
      onClick={() => {
        clerk.openOrganizationProfile();
      }}
    >
      <img src={organization.imageUrl} alt="" className="size-5 shrink-0 rounded" />
      <span className="truncate">{organization.name}</span>
    </button>
  );
}

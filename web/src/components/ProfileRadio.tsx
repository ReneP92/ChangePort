import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { profiles, type Profile } from "@/lib/profiles";

/** A choice of the cluster profiles, each with what it is for. */
export function ProfileRadio({
  id,
  value,
  onChange,
}: {
  /** Prefixes the options' IDs, unique on the page. */
  id: string;
  value: Profile;
  onChange: (profile: Profile) => void;
}) {
  return (
    <RadioGroup
      value={value}
      onValueChange={(next) => {
        onChange(next as Profile);
      }}
    >
      {profiles.map((profile) => (
        <div key={profile.name} className="flex items-start gap-3">
          <RadioGroupItem value={profile.name} id={`${id}-${profile.name}`} className="mt-0.5" />
          <Label
            htmlFor={`${id}-${profile.name}`}
            className="flex flex-col items-start gap-1 font-normal"
          >
            <span className="font-medium">{profile.name}</span>
            <span className="leading-snug text-muted-foreground">{profile.description}</span>
          </Label>
        </div>
      ))}
    </RadioGroup>
  );
}

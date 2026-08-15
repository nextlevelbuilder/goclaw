export function roleBadgeVariant(role: string) {
  switch (role) {
    case "lead": return "info" as const;
    case "reviewer": return "warning" as const;
    case "member": return "outline" as const;
    default: return "outline" as const;
  }
}

export const MEMBER_ROLE_OPTIONS = [
  { value: "member", label: "Member" },
  { value: "reviewer", label: "Reviewer" },
] as const;

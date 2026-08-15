import { useAuthStore } from "@/stores/use-auth-store"

export function useTenants() {
  const tenantId = useAuthStore((s) => s.tenantId)
  const tenantName = useAuthStore((s) => s.tenantName)
  const tenantSlug = useAuthStore((s) => s.tenantSlug)
  const isOwner = useAuthStore((s) => s.isOwner)
  const availableTenants = useAuthStore((s) => s.availableTenants)

  return {
    tenants: availableTenants,
    currentTenantId: tenantId,
    currentTenantName: tenantName,
    currentTenantSlug: tenantSlug,
    isOwner,
    isMultiTenant: availableTenants.length > 1 || isOwner,
    currentTenant: availableTenants.find((t) => t.id === tenantId),
  }
}

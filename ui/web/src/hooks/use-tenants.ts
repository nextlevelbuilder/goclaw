import { useAuthStore } from "@/stores/use-auth-store"

export function useTenants() {
  const tenantId = useAuthStore((s) => s.tenantId)
  const tenantName = useAuthStore((s) => s.tenantName)
  const tenantSlug = useAuthStore((s) => s.tenantSlug)
  const isCrossTenant = useAuthStore((s) => s.isCrossTenant)
  const availableTenants = useAuthStore((s) => s.availableTenants)

  return {
    tenants: availableTenants,
    currentTenantId: tenantId,
    currentTenantName: tenantName,
    currentTenantSlug: tenantSlug,
    isCrossTenant,
    isMultiTenant: availableTenants.length > 1 || isCrossTenant,
    currentTenant: availableTenants.find((t) => t.id === tenantId),
  }
}

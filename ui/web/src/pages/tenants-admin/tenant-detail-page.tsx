import { useState } from "react";
import { useParams, useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { ArrowLeft, Plus, RefreshCw, Users, Trash2, Calendar, Hash, Shield } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useTenantDetail } from "./hooks/use-tenant-detail";
import { ROUTES } from "@/lib/constants";

const TENANT_ROLES = ["owner", "admin", "operator", "member", "viewer"] as const;

const ROLE_KEYS: Record<string, string> = {
  owner: "roleOwner", admin: "roleAdmin", operator: "roleOperator",
  member: "roleMember", viewer: "roleViewer",
};

const ROLE_COLORS: Record<string, string> = {
  owner: "bg-amber-100 text-amber-800 dark:bg-amber-900/30 dark:text-amber-300",
  admin: "bg-orange-100 text-orange-800 dark:bg-orange-900/30 dark:text-orange-300",
  operator: "bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300",
  member: "bg-muted text-muted-foreground",
  viewer: "bg-muted text-muted-foreground",
};

export function TenantDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { t } = useTranslation("tenants");
  const { t: tc } = useTranslation("common");

  const { tenant, tenantLoading, users, usersLoading, usersRefreshing, refreshUsers, addUser, removeUser } =
    useTenantDetail(id);

  const spinning = useMinLoading(usersRefreshing);
  const showSkeleton = useDeferredLoading(usersLoading && users.length === 0);

  const [addOpen, setAddOpen] = useState(false);
  const [userId, setUserId] = useState("");
  const [role, setRole] = useState("member");
  const [adding, setAdding] = useState(false);
  const [removeTarget, setRemoveTarget] = useState<string | null>(null);
  const [removing, setRemoving] = useState(false);

  const handleAdd = async () => {
    if (!userId.trim()) return;
    setAdding(true);
    try {
      await addUser(userId.trim(), role);
      setAddOpen(false);
      setUserId("");
      setRole("member");
    } finally {
      setAdding(false);
    }
  };

  const handleRemove = async () => {
    if (!removeTarget) return;
    setRemoving(true);
    try {
      await removeUser(removeTarget);
      setRemoveTarget(null);
    } finally {
      setRemoving(false);
    }
  };

  if (tenantLoading) {
    return <div className="p-4 sm:p-6 pb-10"><TableSkeleton rows={3} /></div>;
  }

  return (
    <div className="p-4 sm:p-6 space-y-6">
      <PageHeader
        title={tenant?.name ?? t("detail")}
        description=""
        actions={
          <Button variant="outline" size="sm" onClick={() => navigate(ROUTES.TENANTS)} className="gap-1">
            <ArrowLeft className="h-3.5 w-3.5" /> {t("back")}
          </Button>
        }
      />

      {/* Tenant Info Card */}
      {tenant && (
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <InfoCard icon={Hash} label={t("slug")} value={tenant.slug} mono />
          <InfoCard icon={Shield} label={t("status")}>
            <Badge variant={tenant.status === "active" ? "default" : tenant.status === "suspended" ? "destructive" : "secondary"}>
              {t(tenant.status) || tenant.status}
            </Badge>
          </InfoCard>
          <InfoCard icon={Calendar} label={t("created")} value={new Date(tenant.created_at).toLocaleDateString()} />
        </div>
      )}

      {/* User Management */}
      <div>
        <div className="flex items-center justify-between mb-3">
          <h2 className="text-base font-semibold flex items-center gap-2">
            <Users className="h-4 w-4 text-muted-foreground" />
            {t("userManagement")}
            {users.length > 0 && (
              <span className="text-xs font-normal text-muted-foreground">({users.length})</span>
            )}
          </h2>
          <div className="flex gap-2">
            <Button size="sm" onClick={() => setAddOpen(true)} className="gap-1">
              <Plus className="h-3.5 w-3.5" /> {t("addUser")}
            </Button>
            <Button variant="outline" size="sm" onClick={refreshUsers} disabled={spinning} className="gap-1">
              <RefreshCw className={spinning ? "animate-spin h-3.5 w-3.5" : "h-3.5 w-3.5"} />
            </Button>
          </div>
        </div>

        {showSkeleton ? (
          <TableSkeleton rows={4} />
        ) : users.length === 0 ? (
          <EmptyState icon={Users} title={t("noUsers")} description="" />
        ) : (
          <div className="grid gap-2">
            {users.map((u) => (
              <div key={u.user_id} className="flex items-center justify-between rounded-lg border px-4 py-3 hover:bg-muted/30 transition-colors">
                <div className="flex items-center gap-3 min-w-0">
                  <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-muted text-xs font-medium uppercase">
                    {u.user_id.charAt(0)}
                  </div>
                  <div className="min-w-0">
                    <p className="text-sm font-medium truncate">{u.user_id}</p>
                    <p className="text-xs text-muted-foreground">{new Date(u.created_at).toLocaleDateString()}</p>
                  </div>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                  <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${ROLE_COLORS[u.role] || ROLE_COLORS.member}`}>
                    {t(ROLE_KEYS[u.role] ?? u.role)}
                  </span>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-8 w-8 p-0 text-muted-foreground hover:text-destructive"
                    onClick={() => setRemoveTarget(u.user_id)}
                    title={t("removeUser")}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Add User Dialog */}
      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("addUserTitle")}</DialogTitle>
            <DialogDescription>{t("description")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1.5">
              <Label htmlFor="add-user-id">{t("userId")}</Label>
              <Input id="add-user-id" value={userId} onChange={(e) => setUserId(e.target.value)}
                placeholder="user-id" className="text-base md:text-sm" />
            </div>
            <div className="space-y-1.5">
              <Label>{t("selectRole")}</Label>
              <Select value={role} onValueChange={setRole}>
                <SelectTrigger className="text-base md:text-sm"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {TENANT_ROLES.map((r) => (
                    <SelectItem key={r} value={r}>{t(ROLE_KEYS[r] ?? r)}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setAddOpen(false)} disabled={adding}>{tc("cancel")}</Button>
            <Button onClick={handleAdd} disabled={adding || !userId.trim()}>{t("addUser")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={!!removeTarget}
        onOpenChange={(o) => { if (!o) setRemoveTarget(null); }}
        title={t("removeUser")}
        description={t("confirmRemoveUser")}
        confirmLabel={t("removeUser")}
        variant="destructive"
        onConfirm={handleRemove}
        loading={removing}
      />
    </div>
  );
}

function InfoCard({ icon: Icon, label, value, mono, children }: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value?: string;
  mono?: boolean;
  children?: React.ReactNode;
}) {
  return (
    <div className="rounded-lg border p-3 flex items-start gap-3">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-muted">
        <Icon className="h-4 w-4 text-muted-foreground" />
      </div>
      <div className="min-w-0">
        <p className="text-xs text-muted-foreground">{label}</p>
        {children ?? (
          <p className={`text-sm font-medium truncate ${mono ? "font-mono" : ""}`}>{value}</p>
        )}
      </div>
    </div>
  );
}

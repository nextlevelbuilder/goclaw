import { useState, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Combobox } from "@/components/ui/combobox";
import { Shield, X, AlertTriangle, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { WorkspaceSharingConfig } from "@/types/agent";
import { useContacts } from "@/pages/contacts/hooks/use-contacts";
import { InfoLabel } from "./config-section";

const MAX_SHARED_USERS = 100;

interface WorkspaceSharingSectionProps {
  value: WorkspaceSharingConfig;
  onChange: (v: WorkspaceSharingConfig) => void;
}

export function WorkspaceSharingSection({ value, onChange }: WorkspaceSharingSectionProps) {
  const { t } = useTranslation("agents");
  const s = "configSections.workspaceSharing";
  const [contactSearch, setContactSearch] = useState("");

  // Fetch contacts for the combobox
  const { contacts } = useContacts({ search: contactSearch, limit: 20 });

  // Build combobox options from contacts, excluding already-added users
  const existingUsers = value.shared_users ?? [];
  const contactOptions = useMemo(() => {
    const existing = new Set(existingUsers);
    return contacts
      .filter((c) => c.user_id && !existing.has(c.user_id))
      .map((c) => ({
        value: c.user_id!,
        label: c.display_name
          ? `${c.display_name} (${c.user_id})`
          : c.user_id!,
      }));
  }, [contacts, existingUsers]);

  const addUser = (userId: string) => {
    const trimmed = userId.trim();
    if (!trimmed) return;
    if (existingUsers.includes(trimmed)) return;
    if (existingUsers.length >= MAX_SHARED_USERS) return;
    onChange({ ...value, shared_users: [...existingUsers, trimmed] });
    setContactSearch("");
  };

  const removeUser = (idx: number) => {
    const updated = existingUsers.filter((_, i) => i !== idx);
    onChange({ ...value, shared_users: updated.length > 0 ? updated : undefined });
  };

  const isActive = !!value.shared_dm || !!value.shared_group || existingUsers.length > 0;

  return (
    <section className="space-y-3">
      {/* Header — prominent with shield icon */}
      <div className="flex items-center gap-2">
        <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-amber-100 dark:bg-amber-900/30">
          <Shield className="h-4 w-4 text-amber-600 dark:text-amber-400" />
        </div>
        <div>
          <h3 className="text-sm font-semibold">{t(`${s}.title`)}</h3>
          <p className="text-xs text-muted-foreground">{t(`${s}.description`)}</p>
        </div>
      </div>

      {/* Content card with accent border when active */}
      <div className={`rounded-lg border p-3 space-y-4 sm:p-4 ${isActive ? "border-amber-400/60 bg-amber-50/30 dark:border-amber-500/30 dark:bg-amber-950/10" : ""}`}>
        {/* Security warning */}
        <Alert variant="destructive" className="border-amber-500/50 bg-amber-500/10 text-amber-700 dark:text-amber-400 [&>svg]:text-amber-600">
          <AlertTriangle className="h-4 w-4" />
          <AlertDescription className="text-xs">
            {t(`${s}.warning`)}
          </AlertDescription>
        </Alert>

        {/* DM / Group sharing toggles */}
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div className="flex items-center justify-between rounded-md border bg-background p-3">
            <InfoLabel tip={t(`${s}.sharedDmTip`)}>{t(`${s}.sharedDm`)}</InfoLabel>
            <Switch
              checked={value.shared_dm ?? false}
              onCheckedChange={(v) => onChange({ ...value, shared_dm: v })}
            />
          </div>
          <div className="flex items-center justify-between rounded-md border bg-background p-3">
            <InfoLabel tip={t(`${s}.sharedGroupTip`)}>{t(`${s}.sharedGroup`)}</InfoLabel>
            <Switch
              checked={value.shared_group ?? false}
              onCheckedChange={(v) => onChange({ ...value, shared_group: v })}
            />
          </div>
        </div>

        {/* Shared users allowlist with contact search */}
        <div className="space-y-2">
          <InfoLabel tip={t(`${s}.sharedUsersTip`)}>{t(`${s}.sharedUsers`)}</InfoLabel>
          {existingUsers.length > 0 && (
            <div className="flex flex-wrap gap-1.5">
              {existingUsers.map((u, i) => (
                <Badge key={i} variant="secondary" className="gap-1 pr-1">
                  <span className="max-w-48 truncate">{u}</span>
                  <button
                    type="button"
                    onClick={() => removeUser(i)}
                    className="rounded-full p-0.5 hover:bg-muted-foreground/20"
                  >
                    <X className="h-3 w-3" />
                  </button>
                </Badge>
              ))}
            </div>
          )}
          <div className="flex gap-2">
            <Combobox
              value={contactSearch}
              onChange={(val) => setContactSearch(val)}
              options={contactOptions}
              placeholder={t(`${s}.userIdPlaceholder`)}
              className="flex-1"
            />
            <Button
              type="button"
              variant="outline"
              size="icon"
              onClick={() => addUser(contactSearch)}
              disabled={!contactSearch.trim()}
              className="shrink-0"
            >
              <Plus className="h-4 w-4" />
            </Button>
          </div>
        </div>
      </div>
    </section>
  );
}

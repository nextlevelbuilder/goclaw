import { useState, useEffect, useCallback } from "react";
import { Save, Shield } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useHttp } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";

/* eslint-disable @typescript-eslint/no-explicit-any */
type ToolsData = Record<string, any>;

interface DenyGroupInfo {
  name: string;
  description: string;
  default: boolean;
}

interface Props {
  data: ToolsData | undefined;
  onSave: (value: ToolsData) => Promise<void>;
  saving: boolean;
}

export function ShellSecuritySection({ data, onSave, saving }: Props) {
  const { t } = useTranslation("config");
  const http = useHttp();
  const connected = useAuthStore((s) => s.connected);

  // Fetch available deny groups from API.
  const { data: groupsData } = useQuery({
    queryKey: ["shell-deny-groups"],
    queryFn: () => http.get<{ groups: DenyGroupInfo[] }>("/v1/shell-deny-groups"),
    staleTime: 300_000,
    enabled: connected,
  });

  const groups = groupsData?.groups ?? [];

  // Local draft of shellDenyGroups config.
  const [draft, setDraft] = useState<Record<string, boolean>>({});
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    setDraft(data?.shellDenyGroups ?? {});
    setDirty(false);
  }, [data]);

  const toggle = useCallback((name: string, enabled: boolean) => {
    setDraft((prev) => ({ ...prev, [name]: enabled }));
    setDirty(true);
  }, []);

  const handleSave = async () => {
    await onSave({ ...data, shellDenyGroups: draft });
    setDirty(false);
  };

  // Resolve effective state: draft override → group default.
  const isGroupDenied = (g: DenyGroupInfo) => {
    if (draft[g.name] !== undefined) return draft[g.name];
    return g.default;
  };

  if (!data || groups.length === 0) return null;

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-3">
        <CardTitle className="flex items-center gap-2 text-base">
          <Shield className="h-4 w-4" />
          {t("shellSecurity.title", "Shell Security Groups")}
        </CardTitle>
        {dirty && (
          <Button size="sm" onClick={handleSave} disabled={saving} className="gap-1">
            <Save className="h-3.5 w-3.5" />
            {saving ? t("common:saving", "Saving...") : t("common:save", "Save")}
          </Button>
        )}
      </CardHeader>
      <CardContent className="space-y-1">
        <p className="text-xs text-muted-foreground mb-3">
          {t("shellSecurity.description", "Control which command categories AI agents are blocked from executing. Disabling a group allows ALL agents to run those commands unless overridden per-agent.")}
        </p>

        <div className="space-y-2">
          {groups.map((g) => {
            const denied = isGroupDenied(g);
            return (
              <div
                key={g.name}
                className="flex items-center justify-between gap-3 rounded-md border px-3 py-2"
              >
                <div className="flex-1 min-w-0">
                  <Label className="text-sm font-medium cursor-pointer" htmlFor={`deny-${g.name}`}>
                    {g.description}
                  </Label>
                  <p className="text-xs text-muted-foreground font-mono">{g.name}</p>
                </div>
                <Switch
                  id={`deny-${g.name}`}
                  checked={denied}
                  onCheckedChange={(checked) => toggle(g.name, checked)}
                />
              </div>
            );
          })}
        </div>
      </CardContent>
    </Card>
  );
}

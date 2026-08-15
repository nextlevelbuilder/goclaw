import { ShieldCheck, KeyRound, Info } from "lucide-react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

interface SecurityValues {
  injection_action?: string;
  scrub_credentials?: boolean;
}

interface Props {
  value: SecurityValues;
  onChange: (v: SecurityValues) => void;
}

/** Input security settings with visual emphasis on impact. */
export function BehaviorSecurityCard({ value, onChange }: Props) {
  const { t } = useTranslation("config");

  const action = value.injection_action ?? "warn";
  const scrub = value.scrub_credentials !== false;

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{t("behavior.securityTitle")}</CardTitle>
        <CardDescription>{t("behavior.securityDescription")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-0">
        {/* Injection Action */}
        <div className="border-b py-4">
          <div className="flex items-start justify-between gap-4">
            <div className="flex items-start gap-3">
              <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-amber-500" />
              <div className="space-y-1">
                <Label className="text-sm font-medium">{t("gateway.injectionAction")}</Label>
                <p className="text-xs text-muted-foreground">{t("behavior.injectionActionHint")}</p>
              </div>
            </div>
            <Select
              value={action}
              onValueChange={(v) => onChange({ ...value, injection_action: v })}
            >
              <SelectTrigger className="w-32 shrink-0"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="off">Off</SelectItem>
                <SelectItem value="log">Log</SelectItem>
                <SelectItem value="warn">Warn</SelectItem>
                <SelectItem value="block">Block</SelectItem>
              </SelectContent>
            </Select>
          </div>

          {action === "block" && (
            <div className="mt-3 flex items-start gap-2 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-300">
              <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              <span>{t("behavior.injectionBlockInfo")}</span>
            </div>
          )}

          {action === "off" && (
            <div className="mt-3 flex items-start gap-2 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-800 dark:bg-red-950/30 dark:text-red-300">
              <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              <span>{t("behavior.injectionOffInfo")}</span>
            </div>
          )}
        </div>

        {/* Scrub Credentials */}
        <div className="py-4">
          <div className="flex items-center justify-between gap-4">
            <div className="flex items-center gap-3">
              <KeyRound className={cn("h-4 w-4 shrink-0", scrub ? "text-emerald-500" : "text-red-500")} />
              <div className="space-y-1">
                <Label className="text-sm font-medium">{t("tools.scrubCredentials")}</Label>
                <p className="text-xs text-muted-foreground">{t("behavior.scrubCredentialsHint")}</p>
              </div>
            </div>
            <Switch
              checked={scrub}
              onCheckedChange={(v) => onChange({ ...value, scrub_credentials: v })}
              className="shrink-0"
            />
          </div>

          {scrub && (
            <div className="mt-3 flex items-start gap-2 rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2 text-xs text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950/30 dark:text-emerald-300">
              <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              <span>{t("behavior.scrubCredentialsInfo")}</span>
            </div>
          )}

          {!scrub && (
            <div className="mt-3 flex items-start gap-2 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-800 dark:bg-red-950/30 dark:text-red-300">
              <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              <span>{t("behavior.scrubCredentialsOffInfo")}</span>
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

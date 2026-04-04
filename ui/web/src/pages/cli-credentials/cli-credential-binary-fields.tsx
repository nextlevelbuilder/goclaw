import type { UseFormReturn } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { Search, Check, AlertCircle } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import type { CliCredentialFormData } from "@/schemas/credential.schema";

interface CheckResult {
  found: boolean;
  path?: string;
  error?: string;
}

interface CliCredentialBinaryFieldsProps {
  form: UseFormReturn<CliCredentialFormData>;
  checking: boolean;
  checkResult: CheckResult | null;
  onCheckBinary: () => void;
}

/**
 * Renders binary name/path, description, deny-args/deny-verbose,
 * timeout, and tips fields for the CLI credential form.
 */
export function CliCredentialBinaryFields({
  form,
  checking,
  checkResult,
  onCheckBinary,
}: CliCredentialBinaryFieldsProps) {
  const { t } = useTranslation("cli-credentials");
  const { t: tc } = useTranslation("common");
  const { register, formState: { errors }, watch } = form;
  const binaryName = watch("binaryName");

  return (
    <>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="grid gap-1.5">
          <Label htmlFor="cc-name">{t("form.binaryName")}</Label>
          <div className="flex gap-1.5">
            <Input
              id="cc-name"
              {...register("binaryName")}
              placeholder={t("placeholders.binaryName")}
              className="text-base md:text-sm"
            />
            <Button
              type="button"
              variant="outline"
              size="icon"
              className="shrink-0"
              disabled={!binaryName.trim() || checking}
              onClick={onCheckBinary}
              title={t("form.checkBinary")}
            >
              <Search className="h-4 w-4" />
            </Button>
          </div>
          {errors.binaryName && (
            <p className="text-xs text-destructive">{errors.binaryName.message}</p>
          )}
          {checkResult && (
            <p className={`text-xs flex items-center gap-1 ${checkResult.found ? "text-green-600 dark:text-green-400" : "text-destructive"}`}>
              {checkResult.found
                ? <><Check className="h-3 w-3" />{t("form.binaryFound", { path: checkResult.path })}</>
                : <><AlertCircle className="h-3 w-3" />{checkResult.error || t("form.binaryNotFound")}</>}
            </p>
          )}
          {checking && <p className="text-xs text-muted-foreground">{t("form.checking")}</p>}
        </div>

        <div className="grid gap-1.5">
          <Label htmlFor="cc-path">
            {t("form.binaryPath")}{" "}
            <span className="text-xs text-muted-foreground">({tc("optional")})</span>
          </Label>
          <Input
            id="cc-path"
            {...register("binaryPath")}
            placeholder={t("placeholders.binaryPath")}
            className="text-base md:text-sm"
          />
          <p className="text-xs text-muted-foreground">{t("form.binaryPathHint")}</p>
        </div>
      </div>

      <div className="grid gap-1.5">
        <Label htmlFor="cc-desc">{tc("description")}</Label>
        <Textarea
          id="cc-desc"
          {...register("description")}
          placeholder={t("placeholders.description")}
          rows={2}
          className="text-base md:text-sm"
        />
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="grid gap-1.5">
          <Label htmlFor="cc-deny-args">
            {t("form.denyArgs")}{" "}
            <span className="text-xs text-muted-foreground">({t("form.commaSeparated")})</span>
          </Label>
          <Input
            id="cc-deny-args"
            {...register("denyArgs")}
            placeholder={t("placeholders.denyArgs")}
            className="text-base md:text-sm"
          />
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor="cc-timeout">{t("form.timeout")}</Label>
          <Input
            id="cc-timeout"
            type="number"
            min={1}
            {...register("timeout", { valueAsNumber: true })}
            className="text-base md:text-sm"
          />
        </div>
      </div>

      <div className="grid gap-1.5">
        <Label htmlFor="cc-deny-verbose">
          {t("form.denyVerbose")}{" "}
          <span className="text-xs text-muted-foreground">({t("form.commaSeparated")})</span>
        </Label>
        <Input
          id="cc-deny-verbose"
          {...register("denyVerbose")}
          placeholder={t("placeholders.denyVerbose")}
          className="text-base md:text-sm"
        />
      </div>

      <div className="grid gap-1.5">
        <Label htmlFor="cc-tips">{t("form.tips")}</Label>
        <Textarea
          id="cc-tips"
          {...register("tips")}
          placeholder={t("placeholders.tips")}
          rows={2}
          className="text-base md:text-sm"
        />
      </div>
    </>
  );
}

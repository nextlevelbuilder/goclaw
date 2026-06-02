import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { MultiUserPicker } from "@/components/shared/multi-user-picker";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ToolNameSelect } from "@/components/shared/tool-name-select";
import { SkillNameSelect } from "@/components/shared/skill-name-select";
import { BitrixPortalSelect } from "./bitrix24/bitrix-portal-select";
import type { FieldDef } from "./channel-schemas";

const INHERIT = "__inherit__";

interface ChannelFieldsProps {
  fields: FieldDef[];
  values: Record<string, unknown>;
  onChange: (key: string, value: unknown) => void;
  idPrefix: string;
  isEdit?: boolean; // for credentials: show "leave blank to keep" hint
  /** Extra values for showWhen checks (e.g. config values visible to credential fields) */
  contextValues?: Record<string, unknown>;
  /** Channel type — drives special-case renderers (e.g. bitrix24.portal dropdown) */
  channelType?: string;
  /** Callback when the bitrix24 portal dropdown's "+ Create new" item is picked.
   *  Parent owns the modal lifecycle. */
  onPortalCreateRequest?: () => void;
  /** Callback when user clicks a pending (not-yet-authorized) portal in the
   *  dropdown. Parent opens the create modal directly at the authorize step. */
  onPortalResumeAuthorize?: (portalName: string) => void;
}

export function ChannelFields({ fields, values, onChange, idPrefix, isEdit, contextValues, channelType, onPortalCreateRequest, onPortalResumeAuthorize }: ChannelFieldsProps) {
  const allValues = contextValues ? { ...contextValues, ...values } : values;
  return (
    <div className="grid gap-3">
      {fields.map((field) => {
        // Conditional visibility: skip field if showWhen condition is not met
        if (field.showWhen) {
          const depValue = allValues[field.showWhen.key] ?? fields.find((f) => f.key === field.showWhen!.key)?.defaultValue;
          const depStr = depValue !== undefined && depValue !== null ? String(depValue) : "";
          const match = Array.isArray(field.showWhen.value)
            ? field.showWhen.value.includes(depStr)
            : depStr === field.showWhen.value;
          if (!match) return null;
        }
        // Check disabledWhen condition
        let disabled = false;
        let disabledHint: string | undefined;
        if (field.disabledWhen) {
          const depValue = allValues[field.disabledWhen.key] ?? fields.find((f) => f.key === field.disabledWhen!.key)?.defaultValue;
          if (String(depValue) === field.disabledWhen.value) {
            disabled = true;
            disabledHint = field.disabledWhen.hint;
          }
        }
        return (
          <FieldRenderer
            key={field.key}
            field={field}
            value={values[field.key]}
            onChange={(v) => onChange(field.key, v)}
            id={`${idPrefix}-${field.key}`}
            isEdit={isEdit}
            disabled={disabled}
            disabledHint={disabledHint}
            channelType={channelType}
            onPortalCreateRequest={onPortalCreateRequest}
            onPortalResumeAuthorize={onPortalResumeAuthorize}
          />
        );
      })}
    </div>
  );
}

function FieldRenderer({
  field,
  value,
  onChange,
  id,
  isEdit,
  disabled,
  disabledHint,
  channelType,
  onPortalCreateRequest,
  onPortalResumeAuthorize,
}: {
  field: FieldDef;
  value: unknown;
  onChange: (v: unknown) => void;
  id: string;
  isEdit?: boolean;
  disabled?: boolean;
  disabledHint?: string;
  channelType?: string;
  onPortalCreateRequest?: () => void;
  onPortalResumeAuthorize?: (portalName: string) => void;
}) {
  const { t } = useTranslation("channels");
  // i18n: try "fieldConfig.<key>.label" / "fieldConfig.<key>.help", fall back to hardcoded schema string
  const label = t(`fieldConfig.${field.key}.label`, { defaultValue: field.label });
  const help = field.help ? t(`fieldConfig.${field.key}.help`, { defaultValue: field.help }) : "";
  const resolvedHint = disabledHint ? t(disabledHint, { defaultValue: disabledHint }) : undefined;
  const labelSuffix = field.required && !isEdit ? " *" : "";
  const editHint = isEdit && field.type === "password" ? ` ${t("form.credentialsHint")}` : "";

  switch (field.type) {
    case "text":
    case "password":
      // Special case: Bitrix24 portal field renders as a dynamic dropdown
      // populated from bitrix.portals.list, not a free-text input. Hard-coded
      // for one use case — generalising FieldDef to support runtime-loaded
      // options is deferred until a second channel needs it (YAGNI).
      if (channelType === "bitrix24" && field.key === "portal" && field.type === "text") {
        return (
          <div className="grid gap-1.5">
            <Label htmlFor={id}>
              {label}{labelSuffix}
            </Label>
            <BitrixPortalSelect
              value={(value as string) ?? ""}
              onChange={onChange}
              onCreateRequest={onPortalCreateRequest ?? (() => {})}
              onResumeAuthorize={onPortalResumeAuthorize}
            />
            {help && <p className="text-xs text-muted-foreground">{help}</p>}
          </div>
        );
      }
      return (
        <div className="grid gap-1.5">
          <Label htmlFor={id}>
            {label}{labelSuffix}{editHint}
          </Label>
          <Input
            id={id}
            type={field.type}
            value={(value as string) ?? ""}
            onChange={(e) => onChange(e.target.value)}
            placeholder={field.placeholder}
          />
          {help && <p className="text-xs text-muted-foreground">{help}</p>}
        </div>
      );

    case "number":
      return (
        <div className="grid gap-1.5">
          <Label htmlFor={id}>{label}{labelSuffix}</Label>
          <Input
            id={id}
            type="number"
            value={value !== undefined && value !== null ? String(value) : ""}
            onChange={(e) => onChange(e.target.value ? Number(e.target.value) : undefined)}
            placeholder={field.defaultValue !== undefined ? String(field.defaultValue) : undefined}
          />
          {help && <p className="text-xs text-muted-foreground">{help}</p>}
        </div>
      );

    case "boolean": {
      const boolHint = resolvedHint || help;
      return (
        <div className={`grid gap-1${disabled ? " opacity-50" : ""}`}>
          <div className="flex items-center gap-2">
            <Switch
              id={id}
              checked={(value as boolean) ?? (field.defaultValue as boolean) ?? false}
              onCheckedChange={(v) => onChange(v)}
              disabled={disabled}
            />
            <Label htmlFor={id}>{label}</Label>
          </div>
          {boolHint && <p className="text-xs text-muted-foreground ml-9">{boolHint}</p>}
        </div>
      );
    }

    case "select":
      return (
        <div className="grid gap-1.5">
          <Label>{label}{labelSuffix}</Label>
          <Select
            value={(value as string) ?? (field.defaultValue as string) ?? ""}
            onValueChange={(v) => onChange(v)}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {field.options?.map((opt) => (
                <SelectItem key={opt.value} value={opt.value}>
                  {t(`fieldOptions.${field.key}.${opt.value}`, { defaultValue: opt.label })}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {help && <p className="text-xs text-muted-foreground">{help}</p>}
        </div>
      );

    case "tristate": {
      // Tri-state: undefined = inherit, value = override.
      // With options: select with Inherit + custom options (string value).
      // Without options: select with Inherit/Yes/No (boolean value).
      const inheritLabel = t("groupOverrides.fields.inherit", { defaultValue: "Inherit" });

      if (field.options) {
        // String tri-state (e.g. group_policy)
        const allOptions = [{ value: INHERIT, label: inheritLabel }, ...field.options];
        const selectValue = (value as string) || INHERIT;
        return (
          <div className="grid gap-1.5">
            <Label>{label}</Label>
            <Select
              value={selectValue}
              onValueChange={(v) => onChange(v === INHERIT ? undefined : v)}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {allOptions.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {opt.value === INHERIT ? inheritLabel : t(`fieldOptions.${field.key}.${opt.value}`, { defaultValue: opt.label })}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {help && <p className="text-xs text-muted-foreground">{help}</p>}
          </div>
        );
      }

      // Boolean tri-state (e.g. require_mention, enabled)
      const yesLabel = t("groupOverrides.fields.yes", { defaultValue: "Yes" });
      const noLabel = t("groupOverrides.fields.no", { defaultValue: "No" });
      const triOptions = [
        { value: INHERIT, label: inheritLabel },
        { value: "true", label: yesLabel },
        { value: "false", label: noLabel },
      ];
      const boolToStr = (v: unknown): string => {
        if (v === undefined || v === null) return INHERIT;
        return v ? "true" : "false";
      };
      const strToBool = (v: string): boolean | undefined => {
        if (v === INHERIT) return undefined;
        return v === "true";
      };

      return (
        <div className={`grid gap-1.5${disabled ? " opacity-50" : ""}`}>
          <Label>{label}</Label>
          <Select value={boolToStr(value)} onValueChange={(v) => onChange(strToBool(v))} disabled={disabled}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {triOptions.map((opt) => (
                <SelectItem key={opt.value} value={opt.value}>{opt.label}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          {resolvedHint && <p className="text-xs text-muted-foreground">{resolvedHint}</p>}
          {!resolvedHint && help && <p className="text-xs text-muted-foreground">{help}</p>}
        </div>
      );
    }

    case "textarea":
      return (
        <div className="grid gap-1.5">
          <Label htmlFor={id}>{label}</Label>
          <Textarea
            id={id}
            value={(value as string) ?? ""}
            onChange={(e) => onChange(e.target.value || undefined)}
            placeholder={field.placeholder}
            rows={3}
          />
          {help && <p className="text-xs text-muted-foreground">{help}</p>}
        </div>
      );

    case "tool-select":
      return (
        <div className="grid gap-1.5">
          <Label>{label}</Label>
          <ToolNameSelect
            value={(value as string[]) ?? []}
            onChange={(v) => onChange(v.length > 0 ? v : undefined)}
            placeholder={field.placeholder}
          />
          {help && <p className="text-xs text-muted-foreground">{help}</p>}
        </div>
      );

    case "skill-select":
      return (
        <div className="grid gap-1.5">
          <Label>{label}</Label>
          <SkillNameSelect
            value={(value as string[]) ?? []}
            onChange={(v) => onChange(v.length > 0 ? v : undefined)}
            placeholder={field.placeholder}
          />
          {help && <p className="text-xs text-muted-foreground">{help}</p>}
        </div>
      );

    case "tags":
      return (
        <div className="grid gap-1.5">
          <Label htmlFor={id}>{label}</Label>
          <MultiUserPicker
            value={(value as string[]) ?? []}
            onChange={(v) => onChange(v.length > 0 ? v : undefined)}
            placeholder={field.placeholder ?? t("groupOverrides.fields.allowedUsersPlaceholder")}
          />
          {help && <p className="text-xs text-muted-foreground">{help}</p>}
        </div>
      );

    default:
      return null;
  }
}

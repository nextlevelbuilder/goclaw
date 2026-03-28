import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ToolNameSelect } from "@/components/shared/tool-name-select";
import { SkillNameSelect } from "@/components/shared/skill-name-select";
import { useGroups } from "@/pages/contacts/hooks/use-groups";
import { AllowFromPicker } from "./allow-from-picker";
import { ZaloContactsPicker } from "./zalo/zalo-contacts-picker";
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
  /** Channel type — used to resolve group names in tags fields */
  channelType?: string;
  /** Instance ID — used for Zalo contacts picker (fetches from Zalo API) */
  instanceId?: string;
}

export function ChannelFields({ fields, values, onChange, idPrefix, isEdit, contextValues, channelType, instanceId }: ChannelFieldsProps) {
  const allValues = contextValues ? { ...contextValues, ...values } : values;
  return (
    <div className="grid gap-3">
      {fields.map((field) => {
        // Conditional visibility: skip field if showWhen condition is not met
        if (field.showWhen) {
          const depValue = allValues[field.showWhen.key] ?? fields.find((f) => f.key === field.showWhen!.key)?.defaultValue;
          if (String(depValue) !== field.showWhen.value) return null;
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
            instanceId={instanceId}
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
  instanceId,
}: {
  field: FieldDef;
  value: unknown;
  onChange: (v: unknown) => void;
  id: string;
  isEdit?: boolean;
  disabled?: boolean;
  disabledHint?: string;
  channelType?: string;
  instanceId?: string;
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

    case "boolean":
      return (
        <div className={`flex items-center gap-2${disabled ? " opacity-50" : ""}`}>
          <Switch
            id={id}
            checked={(value as boolean) ?? (field.defaultValue as boolean) ?? false}
            onCheckedChange={(v) => onChange(v)}
            disabled={disabled}
          />
          <Label htmlFor={id}>{label}</Label>
          {resolvedHint && <span className="text-xs text-muted-foreground ml-1">— {resolvedHint}</span>}
          {!resolvedHint && help && <span className="text-xs text-muted-foreground ml-1">— {help}</span>}
        </div>
      );

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
      // Use ZaloContactsPicker for Zalo (fetches fresh from Zalo API)
      if (ACCESS_TAG_KEYS.has(field.key) && channelType === "zalo_personal" && instanceId) {
        const tags = Array.isArray(value) ? (value as string[]) : [];
        return (
          <ZaloContactsPicker
            instanceId={instanceId}
            hasCredentials={true}
            value={tags}
            onChange={(ids) => onChange(ids.length > 0 ? ids : undefined)}
          />
        );
      }
      // Use AllowFromPicker for other channels (queries DB)
      // allow_from → users only, group_allow_from → groups only
      if (ACCESS_TAG_KEYS.has(field.key) && channelType) {
        const tags = Array.isArray(value) ? (value as string[]) : [];
        const mode = field.key === "group_allow_from" ? "groups" as const : "users" as const;
        return (
          <AllowFromPicker
            channelType={channelType}
            value={tags}
            onChange={(ids) => onChange(ids.length > 0 ? ids : undefined)}
            label={`${label}${labelSuffix}`}
            help={help}
            mode={mode}
            instanceId={instanceId}
          />
        );
      }
      return (
        <TagsField
          id={id}
          label={`${label}${labelSuffix}`}
          help={help}
          value={value}
          onChange={onChange}
          placeholder={field.placeholder}
          channelType={channelType}
          fieldKey={field.key}
        />
      );

    default:
      return null;
  }
}

// --- Tags field with group name resolution ---

const ACCESS_TAG_KEYS = new Set(["allow_from", "group_allow_from"]);

function TagsField({
  id,
  label,
  help,
  value,
  onChange,
  placeholder,
  channelType,
  fieldKey,
}: {
  id: string;
  label: string;
  help: string;
  value: unknown;
  onChange: (v: unknown) => void;
  placeholder?: string;
  channelType?: string;
  fieldKey: string;
}) {
  const { t } = useTranslation("channels");
  // Only fetch groups for access-control tags fields when channel type is known
  const shouldResolve = ACCESS_TAG_KEYS.has(fieldKey) && !!channelType;
  const { groups } = useGroups(shouldResolve ? channelType : undefined);

  const tags = Array.isArray(value) ? (value as string[]) : [];

  // Build a map of group_id -> group_name for quick lookup
  const groupNameMap = useMemo(() => {
    if (!shouldResolve || groups.length === 0) return new Map<string, string>();
    const map = new Map<string, string>();
    for (const g of groups) {
      if (g.group_name) map.set(g.group_id, g.group_name);
    }
    return map;
  }, [shouldResolve, groups]);

  // Find tags that match known groups
  const resolvedTags = useMemo(() => {
    if (groupNameMap.size === 0 || tags.length === 0) return [];
    return tags
      .filter((tag) => groupNameMap.has(tag))
      .map((tag) => ({ id: tag, name: groupNameMap.get(tag)! }));
  }, [groupNameMap, tags]);

  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <Textarea
        id={id}
        value={tags.join("\n")}
        onChange={(e) => {
          const lines = e.target.value.split(/[\n,]/).map((l) => l.trim()).filter(Boolean);
          onChange(lines.length > 0 ? lines : undefined);
        }}
        placeholder={placeholder ?? t("groupOverrides.fields.allowedUsersPlaceholder")}
        rows={3}
        className="font-mono text-sm"
      />
      {resolvedTags.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {resolvedTags.map(({ id: gid, name }) => (
            <span
              key={gid}
              className="inline-flex items-center gap-1 rounded-md bg-muted px-2 py-0.5 text-xs text-muted-foreground"
              title={gid}
            >
              <span className="font-medium text-foreground">{name}</span>
              <span className="opacity-60">{gid}</span>
            </span>
          ))}
        </div>
      )}
      {help && <p className="text-xs text-muted-foreground">{help}</p>}
    </div>
  );
}

import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
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
import type { ParamField } from "./media-provider-params-schema";

/** Renders a single provider param field based on its type declaration. */
export function ParamFieldControl({
  field,
  value,
  onChange,
}: {
  field: ParamField;
  value: unknown;
  onChange: (v: unknown) => void;
}) {
  const { t } = useTranslation("tools");
  const [jsonStatus, setJsonStatus] = useState<string>("");
  const [jsonStatusError, setJsonStatusError] = useState(false);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  const isWorkflowJSON = field.key === "workflow_api_json";
  const textValue = String(value ?? "");

  const parseWorkflowJSON = (raw: string): { ok: true; value: string } | { ok: false; error: string } => {
    try {
      const parsed = JSON.parse(raw);
      if (parsed && typeof parsed === "object" && "prompt" in (parsed as Record<string, unknown>)) {
        const prompt = (parsed as Record<string, unknown>).prompt;
        if (!prompt || typeof prompt !== "object") {
          return { ok: false, error: t("builtin.mediaChain.workflowInvalidWrapper") };
        }
        return { ok: true, value: JSON.stringify(prompt, null, 2) };
      }
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
        return { ok: false, error: t("builtin.mediaChain.workflowMustBeObject") };
      }
      return { ok: true, value: JSON.stringify(parsed, null, 2) };
    } catch {
      return { ok: false, error: t("builtin.mediaChain.workflowInvalidJson") };
    }
  };

  const validateWorkflowJSON = (raw: string) => {
    const parsed = parseWorkflowJSON(raw);
    if (!parsed.ok) {
      setJsonStatus(parsed.error);
      setJsonStatusError(true);
      return;
    }
    if (!parsed.value.includes("{{prompt}}")) {
      setJsonStatus(t("builtin.mediaChain.workflowMissingPromptToken"));
      setJsonStatusError(true);
      return;
    }
    setJsonStatus(t("builtin.mediaChain.workflowValid"));
    setJsonStatusError(false);
  };

  const formatWorkflowJSON = () => {
    const parsed = parseWorkflowJSON(textValue);
    if (!parsed.ok) {
      setJsonStatus(parsed.error);
      setJsonStatusError(true);
      return;
    }
    onChange(parsed.value);
    setJsonStatus(t("builtin.mediaChain.workflowFormatted"));
    setJsonStatusError(false);
  };

  const handleUploadFile = async (file: File) => {
    const raw = await file.text();
    const parsed = parseWorkflowJSON(raw);
    if (!parsed.ok) {
      setJsonStatus(parsed.error);
      setJsonStatusError(true);
      return;
    }
    onChange(parsed.value);
    validateWorkflowJSON(parsed.value);
  };

  return (
    <div className="space-y-1">
      <Label className="text-xs">{field.label}</Label>
      {field.type === "select" && field.options && (
        <Select value={String(value ?? "")} onValueChange={onChange}>
          <SelectTrigger className="h-8 text-sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {field.options.map((opt) => (
              <SelectItem key={opt.value} value={opt.value}>
                {opt.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      {field.type === "toggle" && (
        <div className="flex items-center h-8">
          <Switch
            size="sm"
            checked={Boolean(value)}
            onCheckedChange={onChange}
          />
        </div>
      )}
      {field.type === "number" && (
        <Input
          type="number"
          min={field.min}
          max={field.max}
          step={field.step}
          value={Number(value ?? 0)}
          onChange={(e) => onChange(Number(e.target.value))}
          className="h-8 text-sm"
        />
      )}
      {field.type === "text" && (
        <div className="space-y-1">
          {isWorkflowJSON ? (
            <>
              <Textarea
                value={textValue}
                onChange={(e) => onChange(e.target.value)}
                placeholder={field.description}
                className="min-h-36 font-mono text-xs"
              />
              <div className="flex flex-wrap gap-2 pt-1">
                <Button type="button" variant="outline" size="sm" onClick={formatWorkflowJSON}>
                  {t("builtin.mediaChain.workflowFormat")}
                </Button>
                <Button type="button" variant="outline" size="sm" onClick={() => validateWorkflowJSON(textValue)}>
                  {t("builtin.mediaChain.workflowValidate")}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => fileInputRef.current?.click()}
                >
                  {t("builtin.mediaChain.workflowUpload")}
                </Button>
                <input
                  ref={fileInputRef}
                  type="file"
                  accept="application/json,.json"
                  className="hidden"
                  onChange={(e) => {
                    const file = e.target.files?.[0];
                    if (file) {
                      void handleUploadFile(file);
                    }
                    e.currentTarget.value = "";
                  }}
                />
              </div>
              {jsonStatus && (
                <p className={`text-xs ${jsonStatusError ? "text-destructive" : "text-muted-foreground"}`}>
                  {jsonStatus}
                </p>
              )}
            </>
          ) : (
            <Input
              value={textValue}
              onChange={(e) => onChange(e.target.value)}
              placeholder={field.description}
              className="h-8 text-sm"
            />
          )}
          {field.description && (
            <p className="text-xs text-muted-foreground">{field.description}</p>
          )}
        </div>
      )}
    </div>
  );
}

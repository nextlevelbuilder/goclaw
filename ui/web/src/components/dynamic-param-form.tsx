import { useState } from "react"
import { ChevronRight } from "lucide-react"
import { useTranslation } from "react-i18next"
import type { ParamSchema } from "@/api/tts-capabilities"
import {
  fieldRenderers,
  DefaultField,
  type FieldProps,
  type ParamValue,
} from "./dynamic-param-form-fields"

export type { ParamValue, FieldProps }

export interface DynamicParamFormProps {
  /** Ordered list of param schemas from ProviderCapabilities.params */
  schema: ParamSchema[]
  /** Current values keyed by ParamSchema.key */
  value: Record<string, ParamValue>
  /** Called when a field changes. Not fired when readonly=true. */
  onChange?: (key: string, val: ParamValue) => void
  /** When true all inputs are rendered in read-only / display mode. */
  readonly?: boolean
}

/**
 * Returns true when all DependsOn constraints are satisfied by formState (AND semantics).
 * An empty DependsOn array means always visible.
 */
export function evaluateDependsOn(
  deps: ParamSchema["depends_on"],
  formState: Record<string, ParamValue>,
): boolean {
  if (!deps || deps.length === 0) return true
  return deps.every((d) => String(formState[d.field]) === String(d.value))
}

/**
 * Partitions schema into basic and advanced groups.
 * group === "advanced" → advanced bucket; anything else (undefined/absent/unknown) → basic.
 * Stable order preserved within each group (source array order).
 */
export function partitionSchema(schema: ParamSchema[]): {
  basic: ParamSchema[]
  advanced: ParamSchema[]
} {
  const basic: ParamSchema[] = []
  const advanced: ParamSchema[] = []
  for (const param of schema) {
    if (param.group === "advanced") {
      advanced.push(param)
    } else {
      basic.push(param)
    }
  }
  return { basic, advanced }
}

/**
 * Renders a single param field row (label + description + input widget).
 * Used by both Basic and Advanced sections to avoid duplication.
 */
function ParamFieldRow({
  param,
  value,
  onChange,
  readonly,
}: {
  param: ParamSchema
  value: Record<string, ParamValue>
  onChange?: (key: string, val: ParamValue) => void
  readonly: boolean
}) {
  if (!evaluateDependsOn(param.depends_on, value)) return null

  const Renderer = fieldRenderers[param.type] ?? DefaultField
  const currentVal: ParamValue =
    value[param.key] !== undefined
      ? value[param.key]!
      : ((param.default as ParamValue) ?? "")

  const handleChange = (val: ParamValue) => {
    if (!readonly && onChange) {
      onChange(param.key, val)
    }
  }

  return (
    <div key={param.key} className="space-y-1">
      <label className="text-sm font-medium" htmlFor={`param-${param.key}`}>
        {param.label}
      </label>
      {param.description && (
        <p className="text-xs text-muted-foreground">{param.description}</p>
      )}
      <Renderer
        schema={param}
        value={currentVal}
        onChange={handleChange}
        readonly={readonly}
      />
    </div>
  )
}

/**
 * Renders a list of TTS provider params from a ParamSchema array.
 * Splits params into Basic (always open) and Advanced (collapsed by default).
 * Basic: params without group or group !== "advanced".
 * Advanced: params with group === "advanced", gated by toggle.
 * evaluateDependsOn always runs against full shared value state.
 *
 * NOT mounted in tts-page.tsx in Phase A — exported for Phase C wiring.
 */
export function DynamicParamForm({
  schema,
  value,
  onChange,
  readonly = false,
}: DynamicParamFormProps) {
  const { t } = useTranslation("tts")
  const [advancedOpen, setAdvancedOpen] = useState(false)

  if (!schema || schema.length === 0) return null

  const { basic, advanced } = partitionSchema(schema)

  // Count visible advanced params (DependsOn resolves against shared value state)
  const visibleAdvancedCount = advanced.filter((p) =>
    evaluateDependsOn(p.depends_on, value),
  ).length

  return (
    <div className="space-y-4">
      {/* Basic section — always visible */}
      <section data-section="basic">
        <div className="space-y-4">
          {basic.map((param) => (
            <ParamFieldRow
              key={param.key}
              param={param}
              value={value}
              onChange={onChange}
              readonly={readonly}
            />
          ))}
        </div>
      </section>

      {/* Advanced section — toggle + collapsible */}
      {advanced.length > 0 && (
        <section data-section="advanced">
          <button
            type="button"
            onClick={() => setAdvancedOpen((prev) => !prev)}
            className="flex w-full items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors py-1"
            aria-expanded={advancedOpen}
          >
            <ChevronRight
              className={`h-4 w-4 shrink-0 transition-transform ${advancedOpen ? "rotate-90" : ""}`}
            />
            <span>{t("advanced.title")}</span>
            {visibleAdvancedCount > 0 && (
              <span className="ml-1 text-xs text-muted-foreground">
                ({t("advanced.count", { count: visibleAdvancedCount })})
              </span>
            )}
          </button>

          {advancedOpen && (
            <div className="mt-3 space-y-4">
              {advanced.map((param) => (
                <ParamFieldRow
                  key={param.key}
                  param={param}
                  value={value}
                  onChange={onChange}
                  readonly={readonly}
                />
              ))}
            </div>
          )}
        </section>
      )}
    </div>
  )
}

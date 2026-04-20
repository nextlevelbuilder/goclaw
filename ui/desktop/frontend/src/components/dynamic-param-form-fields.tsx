import * as React from "react"
import type { ParamSchema, ParamType } from "@/api/tts-capabilities"
import { Slider } from "@/components/ui/slider"
import { Switch } from "@/components/common/Switch"

export type ParamValue = string | number | boolean

export interface FieldProps {
  schema: ParamSchema
  value: ParamValue
  onChange: (val: ParamValue) => void
  readonly: boolean
}

function RangeField({ schema, value, onChange, readonly }: FieldProps) {
  const num = typeof value === "number" ? value : Number(value ?? schema.default ?? 0)
  return (
    <div className="flex items-center gap-3">
      <Slider
        min={schema.min ?? 0}
        max={schema.max ?? 1}
        step={schema.step ?? 0.01}
        value={[num]}
        onValueChange={readonly ? undefined : ([v = 0]) => onChange(v)}
        disabled={readonly}
        aria-label={schema.label}
        className="flex-1"
      />
      <span className="text-sm tabular-nums w-10 text-right">{num.toFixed(2)}</span>
    </div>
  )
}

function NumberField({ schema, value, onChange, readonly }: FieldProps) {
  return (
    <input
      type="number"
      className="border border-surface-secondary rounded px-2 py-1 text-sm w-full bg-surface"
      min={schema.min}
      max={schema.max}
      step={schema.step ?? 1}
      value={value as number}
      readOnly={readonly}
      onChange={readonly ? undefined : (e) => onChange(Number(e.target.value))}
      aria-label={schema.label}
    />
  )
}

function IntegerField({ schema, value, onChange, readonly }: FieldProps) {
  return (
    <input
      type="number"
      className="border border-surface-secondary rounded px-2 py-1 text-sm w-full bg-surface"
      min={schema.min}
      max={schema.max}
      step={1}
      value={value as number}
      readOnly={readonly}
      onChange={readonly ? undefined : (e) => onChange(Math.round(Number(e.target.value)))}
      aria-label={schema.label}
    />
  )
}

function EnumField({ schema, value, onChange, readonly }: FieldProps) {
  return (
    <select
      className="border border-surface-secondary rounded px-2 py-1 text-sm w-full bg-surface"
      value={value as string}
      disabled={readonly}
      onChange={readonly ? undefined : (e) => onChange(e.target.value)}
      aria-label={schema.label}
    >
      {(schema.enum ?? []).map((opt) => (
        <option key={opt.value} value={opt.value}>
          {opt.label}
        </option>
      ))}
    </select>
  )
}

function BooleanField({ schema, value, onChange, readonly }: FieldProps) {
  // Desktop Switch requires a non-optional handler; readonly is enforced via disabled + noop.
  const handler = readonly ? () => {} : (v: boolean) => onChange(v)
  return (
    <Switch
      checked={Boolean(value)}
      onCheckedChange={handler}
      disabled={readonly}
      id={`param-bool-${schema.key}`}
    />
  )
}

function StringField({ schema, value, onChange, readonly }: FieldProps) {
  return (
    <input
      type="text"
      className="border border-surface-secondary rounded px-2 py-1 text-sm w-full bg-surface"
      value={value as string}
      readOnly={readonly}
      onChange={readonly ? undefined : (e) => onChange(e.target.value)}
      aria-label={schema.label}
    />
  )
}

function TextField({ schema, value, onChange, readonly }: FieldProps) {
  return (
    <textarea
      className="border border-surface-secondary rounded px-2 py-1 text-sm w-full bg-surface resize-y min-h-[4rem]"
      value={value as string}
      readOnly={readonly}
      onChange={readonly ? undefined : (e) => onChange(e.target.value)}
      aria-label={schema.label}
    />
  )
}

export const fieldRenderers: Record<ParamType, React.FC<FieldProps>> = {
  range: RangeField,
  number: NumberField,
  integer: IntegerField,
  enum: EnumField,
  boolean: BooleanField,
  string: StringField,
  text: TextField,
}

export { StringField as DefaultField }

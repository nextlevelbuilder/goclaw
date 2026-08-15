/**
 * TtsOverrideFineTune — per-agent TTS fine-tune param editor for the desktop.
 *
 * Renders a DynamicParamForm filtered to agent-overridable params only.
 * The generic↔native key mapping is derived from param.agent_overridable_as
 * (Finding #9: single source of truth in capabilities API — no hard-coded table).
 *
 * Props:
 *   globalProvider — the globally configured TTS provider id (e.g. "openai")
 *   allCaps        — full provider capabilities list (from useTtsCapabilities)
 *   ttsParams      — current generic params state (e.g. {speed: 1.2})
 *   onChange       — called with updated generic params on user change
 */
import { useTranslation } from 'react-i18next'
import { DynamicParamForm } from '../dynamic-param-form'
import type { ParamValue } from '../dynamic-param-form'
import type { ProviderCapabilities } from '../../api/tts-capabilities'

interface Props {
  globalProvider: string
  allCaps: ProviderCapabilities[] | undefined
  ttsParams: Record<string, ParamValue>
  onChange: (updated: Record<string, ParamValue>) => void
}

/** Build generic→native and native→generic maps from an overridable params slice. */
function buildAdapterMaps(params: Array<{ key: string; agent_overridable_as?: string }>) {
  const genericToNative: Record<string, string> = {}
  const nativeToGeneric: Record<string, string> = {}
  for (const p of params) {
    if (p.agent_overridable_as) {
      genericToNative[p.agent_overridable_as] = p.key
      nativeToGeneric[p.key] = p.agent_overridable_as
    }
  }
  return { genericToNative, nativeToGeneric }
}

export function TtsOverrideFineTune({ globalProvider, allCaps, ttsParams, onChange }: Props) {
  const { t } = useTranslation('tts')

  const providerCaps = allCaps?.find((c) => c.provider === globalProvider)
  const overridableParams = (providerCaps?.params ?? []).filter(
    (p) => (p.agent_overridable_as ?? '') !== '',
  )

  if (overridableParams.length === 0) return null

  const { genericToNative, nativeToGeneric } = buildAdapterMaps(overridableParams)

  // Convert stored generic params → native form state for DynamicParamForm.
  const nativeFormState: Record<string, ParamValue> = {}
  for (const [generic, val] of Object.entries(ttsParams)) {
    const native = genericToNative[generic]
    if (native) nativeFormState[native] = val as ParamValue
  }

  const handleParamChange = (nativeKey: string, val: ParamValue) => {
    const updated = { ...nativeFormState, [nativeKey]: val }
    // Convert native form state → generic keys for storage.
    const generic: Record<string, ParamValue> = {}
    for (const [n, v] of Object.entries(updated)) {
      const g = nativeToGeneric[n]
      if (g) generic[g] = v
    }
    onChange(generic)
  }

  return (
    <div className="mt-2 space-y-2 border-t border-border pt-2">
      <p className="text-xs font-medium text-text-muted uppercase tracking-wide">
        {t('override.params.title')}
      </p>
      <DynamicParamForm
        schema={overridableParams}
        value={nativeFormState}
        onChange={handleParamChange}
      />
    </div>
  )
}

import { useTranslation } from 'react-i18next'
import type { MemoryConfig } from '../../types/agent'
import { Field, Input, Switch } from '../ui'

interface MemorySectionProps {
  config: MemoryConfig | null | undefined
  onChange: (config: MemoryConfig | null) => void
}

const defaults: Required<MemoryConfig> = {
  enabled: false,
  embedding_provider: '',
  embedding_model: '',
  max_results: 5,
  max_chunk_len: 512,
  chunk_overlap: 50,
  vector_weight: 0.7,
  text_weight: 0.3,
  min_score: 0.3,
}

export function MemorySection({ config, onChange }: MemorySectionProps) {
  const { t } = useTranslation('agents')
  const enabled = config?.enabled ?? false

  const update = (field: keyof MemoryConfig, value: unknown) => {
    onChange({ ...defaults, ...config, [field]: value })
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-ui font-semibold text-ink">{t('screen.memory.title')}</h3>
          <p className="text-caption text-ink-muted">{t('screen.memory.subtitle')}</p>
        </div>
        <Switch
          checked={enabled}
          aria-label={t('screen.memory.title')}
          onCheckedChange={(on) => {
            if (on) onChange({ ...defaults, ...config, enabled: true })
            else onChange(null)
          }}
        />
      </div>

      {enabled && (
        <div className="rounded-lg border border-border-subtle p-3 space-y-4">
          {/* Search results */}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <NumberField
              label={t('screen.memory.maxResults')}
              hint={t('screen.memory.maxResultsHint')}
              value={config?.max_results ?? defaults.max_results}
              onChange={(v) => update('max_results', v)}
            />
            <NumberField
              label={t('screen.memory.minScore')}
              hint={t('screen.memory.minScoreHint')}
              value={config?.min_score ?? defaults.min_score}
              onChange={(v) => update('min_score', v)}
              step={0.05}
            />
          </div>

          {/* Chunking */}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <NumberField
              label={t('screen.memory.maxChunkLen')}
              hint={t('screen.memory.maxChunkLenHint')}
              value={config?.max_chunk_len ?? defaults.max_chunk_len}
              onChange={(v) => update('max_chunk_len', v)}
            />
            <NumberField
              label={t('screen.memory.chunkOverlap')}
              hint={t('screen.memory.chunkOverlapHint')}
              value={config?.chunk_overlap ?? defaults.chunk_overlap}
              onChange={(v) => update('chunk_overlap', v)}
            />
          </div>

          {/* Weights */}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <NumberField
              label={t('screen.memory.vectorWeight')}
              hint={t('screen.memory.vectorWeightHint')}
              value={config?.vector_weight ?? defaults.vector_weight}
              onChange={(v) => update('vector_weight', v)}
              step={0.1}
            />
            <NumberField
              label={t('screen.memory.textWeight')}
              hint={t('screen.memory.textWeightHint')}
              value={config?.text_weight ?? defaults.text_weight}
              onChange={(v) => update('text_weight', v)}
              step={0.1}
            />
          </div>
        </div>
      )}

      {!enabled && (
        <p className="text-caption text-ink-muted italic">{t('screen.memory.globalDefaults')}</p>
      )}
    </div>
  )
}

function NumberField({ label, hint, value, onChange, step }: {
  label: string; hint: string; value: number
  onChange: (v: number) => void; step?: number
}) {
  return (
    <Field label={label} helper={hint}>
      <Input
        type="number"
        value={value}
        step={step}
        onChange={(e) => onChange(Number(e.target.value) || 0)}
        className="font-mono"
      />
    </Field>
  )
}

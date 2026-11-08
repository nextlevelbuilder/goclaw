// Stacked input/output tokens per UTC day as inline SVG (no chart library).
import { useTranslation } from 'react-i18next'
import { formatCompactNumber, formatDayLabel, formatDayLong, formatInteger } from '../providers/provider-usage-format'
import type { UsageRange, UsageTimeSeriesPoint } from '../../types/provider'

const DAY_MS = 86_400_000
const W = 640
const H = 200
const PAD = { top: 12, right: 8, bottom: 24, left: 44 }

interface DayBar { day: string; input: number; output: number }

function buildBars(points: UsageTimeSeriesPoint[], range: UsageRange, now: Date): DayBar[] {
  const byDay = new Map<string, { input: number; output: number }>()
  for (const p of points) {
    const day = new Date(p.bucket_time).toISOString().slice(0, 10)
    const cur = byDay.get(day) ?? { input: 0, output: 0 }
    cur.input += p.input_tokens
    cur.output += p.output_tokens
    byDay.set(day, cur)
  }
  const days = range === '24h' ? 1 : range === '7d' ? 7 : 30
  const endUtc = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate())
  const out: DayBar[] = []
  for (let i = days; i >= 0; i--) {
    if (range === '24h' && i > 1) continue
    const day = new Date(endUtc - i * DAY_MS).toISOString().slice(0, 10)
    const v = byDay.get(day) ?? { input: 0, output: 0 }
    out.push({ day, ...v })
  }
  return out
}

/** Rounds `max` up to a tidy axis ceiling (1, 2, 5 × 10ⁿ). */
function niceCeil(max: number): number {
  if (max <= 0) return 1
  const exp = 10 ** Math.floor(Math.log10(max))
  for (const m of [1, 2, 5, 10]) if (max <= m * exp) return m * exp
  return 10 * exp
}

export function UsageTokenChart({ points, range, locale }: { points: UsageTimeSeriesPoint[]; range: UsageRange; locale: string }) {
  const { t } = useTranslation('usage')
  const bars = buildBars(points, range, new Date())
  const top = niceCeil(Math.max(0, ...bars.map((b) => b.input + b.output)))
  const innerW = W - PAD.left - PAD.right
  const innerH = H - PAD.top - PAD.bottom
  const slot = innerW / bars.length
  const barW = Math.min(28, slot * 0.64)
  const y = (v: number) => PAD.top + innerH - (v / top) * innerH
  const ticks = [0, 0.5, 1].map((f) => f * top)
  const labelEvery = bars.length > 14 ? 5 : 1

  return (
    <figure className="m-0 rounded-lg border border-border-subtle bg-surface-raised p-4">
      <figcaption className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <span className="text-ui text-ink">{t('chart.title')}</span>
        <span className="flex items-center gap-3 text-caption text-ink-secondary">
          <span className="inline-flex items-center gap-1.5"><span aria-hidden className="size-2 rounded-sm bg-accent" />{t('chart.input')}</span>
          <span className="inline-flex items-center gap-1.5"><span aria-hidden className="size-2 rounded-sm bg-info" />{t('chart.output')}</span>
        </span>
      </figcaption>
      <svg viewBox={`0 0 ${W} ${H}`} className="h-auto w-full" role="img" aria-label={t('chart.title')}>
        {ticks.map((v) => (
          <g key={v}>
            <line x1={PAD.left} x2={W - PAD.right} y1={y(v)} y2={y(v)} className="stroke-border-subtle" strokeWidth={1} />
            <text x={PAD.left - 6} y={y(v) + 4} textAnchor="end" className="fill-ink-muted" fontSize={11}>{formatCompactNumber(v, locale)}</text>
          </g>
        ))}
        {bars.map((b, i) => {
          const x = PAD.left + slot * i + (slot - barW) / 2
          const total = b.input + b.output
          const label = t('chart.barLabel', { date: formatDayLong(b.day, locale), input: formatInteger(b.input, locale), output: formatInteger(b.output, locale) })
          return (
            <g key={b.day}>
              <title>{label}</title>
              {total > 0 && (
                <>
                  <rect x={x} y={y(b.input)} width={barW} height={Math.max(1, PAD.top + innerH - y(b.input))} rx={2} className="fill-accent" />
                  {b.output > 0 && <rect x={x} y={y(total)} width={barW} height={Math.max(1, y(b.input) - y(total))} rx={2} className="fill-info" />}
                </>
              )}
              {i % labelEvery === 0 && (
                <text x={x + barW / 2} y={H - 6} textAnchor="middle" className="fill-ink-muted" fontSize={11}>{formatDayLabel(b.day, range, locale)}</text>
              )}
            </g>
          )
        })}
      </svg>
    </figure>
  )
}

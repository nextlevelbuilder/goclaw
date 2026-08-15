import { useOrchestration } from '../../hooks/use-orchestration'

interface OrchestrationSectionProps {
  agentId: string
}

const MODE_COLORS: Record<string, string> = {
  spawn: 'bg-surface-tertiary text-text-muted',
  delegate: 'bg-accent/10 text-accent',
  team: 'bg-purple-500/10 text-purple-600',
}

export function OrchestrationSection({ agentId }: OrchestrationSectionProps) {
  const { mode, delegateTargets, team, loading } = useOrchestration(agentId)

  if (loading) return null

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <svg className="w-4 h-4 text-indigo-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
            <circle cx="12" cy="5" r="3" /><circle cx="5" cy="19" r="3" /><circle cx="19" cy="19" r="3" />
            <path d="M12 8v4M9.5 14.5L6.5 17M14.5 14.5l3 2.5" />
          </svg>
          <h3 className="text-sm font-semibold text-text-primary">Orchestration</h3>
        </div>
        <span className={`text-[10px] px-2 py-0.5 rounded-full font-medium ${MODE_COLORS[mode] ?? MODE_COLORS.spawn}`}>
          {mode}
        </span>
      </div>

      {delegateTargets.length > 0 && (
        <div className="space-y-1.5">
          <p className="text-[11px] text-text-muted">Delegate Targets</p>
          <div className="flex flex-wrap gap-1.5">
            {delegateTargets.map((dt) => (
              <span key={dt.agent_key} className="text-[10px] px-2 py-0.5 rounded-md bg-surface-tertiary text-text-secondary">
                {dt.display_name || dt.agent_key}
              </span>
            ))}
          </div>
        </div>
      )}

      {team && (
        <div className="space-y-1">
          <p className="text-[11px] text-text-muted">Team</p>
          <span className="text-[10px] px-2 py-0.5 rounded-md bg-purple-500/10 text-purple-600">{team}</span>
        </div>
      )}

      {delegateTargets.length === 0 && !team && mode === 'spawn' && (
        <p className="text-[11px] text-text-muted italic">Standard spawn mode — no delegation configured</p>
      )}
    </div>
  )
}

// Dialog for "Edit with AI" — sends prompt to regenerate agent context files endpoint.

import { useState } from 'react'
import { useTranslation } from 'react-i18next'

interface RegenerateDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onRegenerate: (prompt: string) => Promise<void>
}

export function RegenerateDialog({ open, onOpenChange, onRegenerate }: RegenerateDialogProps) {
  const { t } = useTranslation('agents')
  const [prompt, setPrompt] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async () => {
    if (!prompt.trim()) return
    setLoading(true)
    try {
      await onRegenerate(prompt.trim())
      onOpenChange(false)
      setPrompt('')
    } finally {
      setLoading(false)
    }
  }

  if (!open) return null

  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center bg-black/40 backdrop-blur-sm">
      <div className="bg-surface-secondary border border-border rounded-xl shadow-xl max-w-lg w-full mx-4 p-5 space-y-4">
        <h3 className="text-sm font-semibold text-text-primary flex items-center gap-2">
          <svg className="h-4 w-4 text-accent" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <path d="m12 3-1.912 5.813a2 2 0 0 1-1.275 1.275L3 12l5.813 1.912a2 2 0 0 1 1.275 1.275L12 21l1.912-5.813a2 2 0 0 1 1.275-1.275L21 12l-5.813-1.912a2 2 0 0 1-1.275-1.275L12 3Z" />
          </svg>
          Edit with AI
        </h3>
        <p className="text-xs text-text-muted">
          Describe how you want to modify the agent's personality, knowledge, or behavior. The AI will regenerate the context files accordingly.
        </p>
        <textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          placeholder="e.g. Make the agent more formal and add expertise in data analysis..."
          rows={4}
          autoFocus
          className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent resize-none"
        />
        <div className="flex justify-end gap-2">
          <button
            onClick={() => onOpenChange(false)}
            disabled={loading}
            className="px-3 py-1.5 text-xs border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={handleSubmit}
            disabled={!prompt.trim() || loading}
            className="px-4 py-1.5 text-xs bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors disabled:opacity-50 flex items-center gap-1.5"
          >
            <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="m12 3-1.912 5.813a2 2 0 0 1-1.275 1.275L3 12l5.813 1.912a2 2 0 0 1 1.275 1.275L12 21l1.912-5.813a2 2 0 0 1 1.275-1.275L21 12l-5.813-1.912a2 2 0 0 1-1.275-1.275L12 3Z" />
            </svg>
            {loading ? t('fileEditor.sending') : t('fileEditor.regenerate')}
          </button>
        </div>
      </div>
    </div>
  )
}

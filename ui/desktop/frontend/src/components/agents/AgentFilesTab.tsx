import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { agentService } from '../../services/agent-service'
import { RegenerateDialog } from './agent-regenerate-dialog'
import type { BootstrapFile } from '../../types/agent'

/** Files hidden from desktop UI — USER.md is per-user (managed by bootstrap), HEARTBEAT.md needs cron config */
const HIDDEN_FILES = ['MEMORY.json', 'BOOTSTRAP.md', 'USER.md', 'USER_PREDEFINED.md', 'HEARTBEAT.md']

interface AgentFilesTabProps {
  agentId: string
  agentKey: string
  agentType: string
}

export function AgentFilesTab({ agentId, agentKey, agentType }: AgentFilesTabProps) {
  const { t } = useTranslation('agents')
  const [files, setFiles] = useState<BootstrapFile[]>([])
  const [selectedFile, setSelectedFile] = useState<string | null>(null)
  const [content, setContent] = useState('')
  const [loading, setLoading] = useState(true)
  const [fileLoading, setFileLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [dirty, setDirty] = useState(false)
  const [regenerateOpen, setRegenerateOpen] = useState(false)

  useEffect(() => {
    setLoading(true)
    agentService.listFiles(agentKey)
      .then((res) => {
        const fileList = (res?.files ?? []).filter((f) => !HIDDEN_FILES.includes(f.name))
        setFiles(fileList)
        if (fileList.length > 0 && !selectedFile) {
          setSelectedFile(fileList[0].name)
        }
      })
      .catch(console.error)
      .finally(() => setLoading(false))
  }, [agentKey, agentType, selectedFile])

  const loadFile = useCallback(async (name: string) => {
    setFileLoading(true)
    try {
      const res = await agentService.getFile(agentKey, name)
      setContent(res?.file?.content ?? '')
      setDirty(false)
    } catch {
      setContent('')
    } finally {
      setFileLoading(false)
    }
  }, [agentKey])

  useEffect(() => {
    if (selectedFile) loadFile(selectedFile)
  }, [selectedFile, loadFile])

  const handleSave = async () => {
    if (!selectedFile) return
    setSaving(true)
    try {
      await agentService.setFile(agentKey, selectedFile, content)
      setDirty(false)
    } catch (err) {
      console.error('Failed to save file:', err)
    } finally {
      setSaving(false)
    }
  }

  const handleRegenerate = async (prompt: string) => {
    try {
      await agentService.regenerate(agentId, prompt)
      if (selectedFile) {
        setTimeout(() => loadFile(selectedFile), 2000)
      }
    } catch (err) {
      console.error('Regenerate failed:', err)
    }
  }

  if (loading) {
    return <p className="text-xs text-text-muted py-4">Loading files...</p>
  }

  return (
    <div className="space-y-3">
      {/* Toolbar */}
      <div className="flex items-center justify-between">
        <p className="text-xs text-text-muted">Agent context files — SOUL.md, IDENTITY.md, AGENTS.md</p>
        <button
          onClick={() => setRegenerateOpen(true)}
          className="px-3 py-1.5 text-xs border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors flex items-center gap-1.5"
        >
          <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <path d="m12 3-1.912 5.813a2 2 0 0 1-1.275 1.275L3 12l5.813 1.912a2 2 0 0 1 1.275 1.275L12 21l1.912-5.813a2 2 0 0 1 1.275-1.275L21 12l-5.813-1.912a2 2 0 0 1-1.275-1.275L12 3Z" />
          </svg>
          Edit with AI
        </button>
      </div>

      {/* File editor layout */}
      <div className="flex h-[500px] gap-3">
        {/* File sidebar */}
        <div className="w-48 shrink-0 rounded-lg border border-border overflow-y-auto">
          {files.map((file) => (
            <button
              key={file.name}
              onClick={() => setSelectedFile(file.name)}
              className={[
                'w-full text-left px-3 py-2.5 text-xs border-b border-border last:border-b-0 transition-colors',
                selectedFile === file.name
                  ? 'bg-accent/10 text-accent font-medium'
                  : 'text-text-secondary hover:bg-surface-tertiary',
                file.missing ? 'opacity-50' : '',
              ].join(' ')}
            >
              <div className="flex items-center gap-2">
                <svg className="w-3.5 h-3.5 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                  <path d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5L14.5 2z" />
                  <polyline points="14 2 14 8 20 8" />
                </svg>
                <span className="truncate">{file.name}</span>
              </div>
              {file.missing && (
                <span className="text-[10px] text-text-muted ml-5.5 block">Not created</span>
              )}
            </button>
          ))}
        </div>

        {/* Editor */}
        <div className="flex-1 flex flex-col min-w-0 rounded-lg border border-border overflow-hidden">
          {selectedFile ? (
            <>
              <div className="flex items-center justify-between px-3 py-2 border-b border-border bg-surface-tertiary/30">
                <div className="flex items-center gap-2">
                  <span className="text-xs font-medium text-text-primary">{selectedFile}</span>
                  {dirty && <span className="text-[10px] text-warning font-medium">Unsaved</span>}
                </div>
                <button
                  onClick={handleSave}
                  disabled={!dirty || saving}
                  className="px-3 py-1 text-[11px] bg-accent text-white rounded-md font-medium hover:bg-accent-hover transition-colors disabled:opacity-40 flex items-center gap-1.5"
                >
                  {saving && <span className="w-3 h-3 border-2 border-white border-t-transparent rounded-full animate-spin" />}
                  {saving ? t('files.saving') : t('files.save')}
                </button>
              </div>
              {fileLoading ? (
                <div className="flex-1 flex items-center justify-center">
                  <span className="w-5 h-5 border-2 border-accent border-t-transparent rounded-full animate-spin" />
                </div>
              ) : (
                <textarea
                  value={content}
                  onChange={(e) => { setContent(e.target.value); setDirty(true) }}
                  className="flex-1 bg-surface-primary px-3 py-3 text-xs font-mono text-text-primary leading-relaxed resize-none focus:outline-none"
                  spellCheck={false}
                />
              )}
            </>
          ) : (
            <div className="flex-1 flex items-center justify-center">
              <p className="text-xs text-text-muted">Select a file to edit</p>
            </div>
          )}
        </div>
      </div>

      <RegenerateDialog
        open={regenerateOpen}
        onOpenChange={setRegenerateOpen}
        onRegenerate={handleRegenerate}
      />
    </div>
  )
}

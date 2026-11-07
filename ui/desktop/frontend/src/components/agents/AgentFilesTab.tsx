import { useState, useEffect, useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { agentService } from '../../services/agent-service'
import { cn } from '../../lib/cn'
import { Button, ErrorState, SkeletonList, StatusChip, Textarea, toast } from '../ui'
import { IconDocument } from '../common/Icons'
import { RegenerateDialog } from './agent-regenerate-dialog'
import type { BootstrapFile } from '../../types/agent'

/** Files hidden from desktop UI — USER.md is per-user (managed by bootstrap), HEARTBEAT.md needs cron config */
const HIDDEN_FILES = ['MEMORY.json', 'BOOTSTRAP.md', 'USER.md', 'USER_PREDEFINED.md', 'HEARTBEAT.md']

interface AgentFilesTabProps {
  agentId: string
  agentKey: string
  agentType: string
  readOnly?: boolean
}

interface FileText {
  saved: string
  draft: string
}

const SparkleIcon = (
  <svg className="size-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="m12 3-1.912 5.813a2 2 0 0 1-1.275 1.275L3 12l5.813 1.912a2 2 0 0 1 1.275 1.275L12 21l1.912-5.813a2 2 0 0 1 1.275-1.275L21 12l-5.813-1.912a2 2 0 0 1-1.275-1.275L12 3Z" />
  </svg>
)

export function AgentFilesTab({ agentId, agentKey, readOnly = false }: AgentFilesTabProps) {
  const { t } = useTranslation('agents')
  const [files, setFiles] = useState<BootstrapFile[]>([])
  const [selectedFile, setSelectedFile] = useState<string | null>(null)
  // Edits are kept per file so switching files never drops unsaved text.
  const [texts, setTexts] = useState<Record<string, FileText>>({})
  const [loading, setLoading] = useState(true)
  const [loadFailed, setLoadFailed] = useState(false)
  const [fileLoading, setFileLoading] = useState(false)
  const [fileFailed, setFileFailed] = useState(false)
  const [saving, setSaving] = useState(false)
  const [regenerateOpen, setRegenerateOpen] = useState(false)
  const gutterRef = useRef<HTMLDivElement>(null)

  const loadList = useCallback(async () => {
    setLoading(true)
    setLoadFailed(false)
    try {
      const res = await agentService.listFiles(agentKey)
      const list = (res?.files ?? []).filter((f) => !HIDDEN_FILES.includes(f.name))
      setFiles(list)
      setSelectedFile((cur) => cur ?? list[0]?.name ?? null)
    } catch (err) {
      console.error('Failed to list agent files:', err)
      setLoadFailed(true)
    } finally {
      setLoading(false)
    }
  }, [agentKey])

  useEffect(() => { void loadList() }, [loadList])

  const loadFile = useCallback(async (name: string) => {
    setFileLoading(true)
    setFileFailed(false)
    try {
      const res = await agentService.getFile(agentKey, name)
      const content = res?.file?.content ?? ''
      setTexts((prev) => ({ ...prev, [name]: { saved: content, draft: content } }))
    } catch (err) {
      console.error('Failed to load agent file:', err)
      setFileFailed(true)
    } finally {
      setFileLoading(false)
    }
  }, [agentKey])

  useEffect(() => {
    if (selectedFile && !texts[selectedFile]) void loadFile(selectedFile)
    // texts is read only to skip files already loaded
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedFile, loadFile])

  const current = selectedFile ? texts[selectedFile] : undefined
  const dirty = current ? current.draft !== current.saved : false

  const setDraft = (value: string) => {
    if (!selectedFile) return
    setTexts((prev) => ({ ...prev, [selectedFile]: { saved: prev[selectedFile]?.saved ?? '', draft: value } }))
  }

  const handleSave = async () => {
    if (!selectedFile || !current) return
    setSaving(true)
    try {
      await agentService.setFile(agentKey, selectedFile, current.draft)
      setTexts((prev) => ({ ...prev, [selectedFile]: { saved: current.draft, draft: current.draft } }))
      setFiles((prev) => prev.map((f) => (f.name === selectedFile ? { ...f, missing: false } : f)))
      toast.success(t('screen.files.savedToast', { name: selectedFile }))
    } catch (err) {
      console.error('Failed to save file:', err)
      toast.error(t('screen.files.saveFailed', { name: selectedFile }), err instanceof Error ? err.message : undefined)
    } finally {
      setSaving(false)
    }
  }

  const handleRegenerate = async (prompt: string) => {
    try {
      await agentService.regenerate(agentId, prompt)
      toast.info(t('screen.files.regenerateStarted'))
      if (selectedFile) setTimeout(() => { void loadFile(selectedFile) }, 2000)
    } catch (err) {
      console.error('Regenerate failed:', err)
      toast.error(t('screen.files.regenerateFailed'), err instanceof Error ? err.message : undefined)
      throw err
    }
  }

  if (loading) {
    return <SkeletonList rows={4} avatar={false} label={t('screen.files.title')} className="rounded-lg border border-border-subtle" />
  }

  if (loadFailed) {
    return <ErrorState title={t('screen.files.loadError')} description={t('screen.files.loadErrorHint')} onRetry={() => { void loadList() }} />
  }

  const text = current?.draft ?? ''
  const lineCount = text === '' ? 0 : text.split('\n').length

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-ui font-semibold text-ink">{t('screen.files.title')}</h2>
        <Button icon={SparkleIcon} onClick={() => setRegenerateOpen(true)} disabled={readOnly}>
          {t('screen.files.editWithAi')}
        </Button>
      </div>

      {files.length === 0 ? (
        <p className="rounded-lg border border-border-subtle bg-surface-raised px-4 py-8 text-center text-ui font-normal text-ink-muted">
          {t('screen.files.empty')}
        </p>
      ) : (
        <div className="grid min-h-[480px] grid-cols-1 gap-4 md:grid-cols-[220px_minmax(0,1fr)]">
          <nav aria-label={t('screen.files.title')} className="flex flex-row gap-1 overflow-x-auto md:flex-col md:overflow-visible">
            {files.map((file) => {
              const selected = selectedFile === file.name
              const fileDirty = texts[file.name] ? texts[file.name].draft !== texts[file.name].saved : false
              return (
                <button
                  key={file.name}
                  type="button"
                  aria-current={selected ? 'true' : undefined}
                  onClick={() => setSelectedFile(file.name)}
                  className={cn(
                    'flex h-row shrink-0 items-center gap-2 rounded-md px-3 text-left text-ui transition-colors',
                    selected ? 'bg-accent-subtle text-accent-subtle-ink' : 'text-ink-secondary hover:bg-surface-hover hover:text-ink',
                  )}
                >
                  <IconDocument size={14} className="shrink-0" />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-mono text-code">{file.name}</span>
                    {file.missing && <span className="block text-caption font-normal text-ink-muted">{t('screen.files.notCreated')}</span>}
                  </span>
                  {fileDirty && <span role="img" aria-label={t('screen.files.unsaved')} className="size-2 shrink-0 rounded-pill bg-warning" />}
                </button>
              )
            })}
          </nav>

          <div className="flex min-w-0 flex-col overflow-hidden rounded-lg border border-border-subtle bg-surface-raised">
            {selectedFile && (
              <>
                <div className="flex items-center gap-3 border-b border-border-subtle px-4 py-2">
                  <span className="truncate font-mono text-code text-ink">{selectedFile}</span>
                  {current && <StatusChip tone={dirty ? 'warning' : 'success'} label={dirty ? t('screen.files.unsaved') : t('screen.files.saved')} />}
                  <div className="ml-auto flex items-center gap-2">
                    <Button size="sm" onClick={() => setDraft(current?.saved ?? '')} disabled={!dirty || saving}>{t('screen.files.revert')}</Button>
                    <Button size="sm" variant="primary" onClick={handleSave} disabled={!dirty || readOnly} loading={saving}>{t('screen.files.save')}</Button>
                  </div>
                </div>
                <p className="border-b border-border-subtle bg-surface-base px-4 py-2 text-caption font-normal text-ink-muted">{t('screen.files.note')}</p>

                {fileLoading ? (
                  <SkeletonList rows={6} avatar={false} label={selectedFile} className="flex-1" />
                ) : fileFailed ? (
                  <ErrorState
                    title={t('screen.files.fileLoadError', { name: selectedFile })}
                    description={t('screen.files.loadErrorHint')}
                    onRetry={() => { void loadFile(selectedFile) }}
                  />
                ) : (
                  <div className="flex min-h-0 flex-1">
                    <div
                      ref={gutterRef}
                      aria-hidden="true"
                      className="w-12 shrink-0 overflow-hidden border-r border-border-subtle bg-surface-base py-3 pr-2 text-right font-mono text-code leading-6 text-ink-muted"
                    >
                      {Array.from({ length: Math.max(lineCount, 1) }, (_, i) => <div key={i}>{i + 1}</div>)}
                    </div>
                    <Textarea
                      value={text}
                      onChange={(e) => setDraft(e.target.value)}
                      onScroll={(e) => { if (gutterRef.current) gutterRef.current.scrollTop = e.currentTarget.scrollTop }}
                      aria-label={t('screen.files.editorLabel', { name: selectedFile })}
                      spellCheck={false}
                      readOnly={readOnly}
                      wrap="off"
                      className="min-h-[360px] flex-1 resize-none rounded-none border-0 bg-surface-raised py-3 font-mono text-code leading-6"
                    />
                  </div>
                )}

                <div className="flex items-center gap-4 border-t border-border-subtle px-4 py-2 text-caption font-normal text-ink-muted">
                  <span>{t('screen.files.lines', { count: lineCount })}</span>
                  <span>{t('screen.files.chars', { count: text.length })}</span>
                  <span className="ml-auto">{t('screen.files.markdown')}</span>
                </div>
              </>
            )}
          </div>
        </div>
      )}

      <RegenerateDialog open={regenerateOpen} onOpenChange={setRegenerateOpen} onRegenerate={handleRegenerate} />
    </div>
  )
}

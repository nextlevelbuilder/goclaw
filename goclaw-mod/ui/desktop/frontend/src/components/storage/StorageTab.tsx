// Main Storage tab for Settings — composes file browser, upload dialog, delete confirm.
// Calls /v1/storage/* REST endpoints via useStorage + useStorageSize hooks.

import { useState, useEffect, useCallback, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { useStorage, useStorageSize } from '../../hooks/use-storage'
import { buildTree, mergeSubtree, setNodeLoading, formatSize, isTextFile } from '../../lib/file-helpers'
import { getApiClient } from '../../lib/api'
import { wails } from '../../lib/wails'
import { ConfirmDialog } from '../common/ConfirmDialog'
import { RefreshButton } from '../common/RefreshButton'
import { StorageFileBrowser } from './StorageFileBrowser'
import { StorageUploadDialog } from './StorageUploadDialog'

export function StorageTab() {
  const { t } = useTranslation('storage')
  const { t: tc } = useTranslation('common')
  const {
    files, baseDir, loading,
    listFiles, loadSubtree, readFile, deleteFile, uploadFile, moveFile, fetchRawBlob,
  } = useStorage()
  const { totalSize, loading: sizeLoading, refreshSize } = useStorageSize()

  const [tree, setTree] = useState(buildTree(files))
  const [activePath, setActivePath] = useState<string | null>(null)
  const [fileContent, setFileContent] = useState<{ content: string; path: string; size: number } | null>(null)
  const [contentLoading, setContentLoading] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<{ path: string; isDir: boolean } | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [uploadOpen, setUploadOpen] = useState(false)
  const [uploadFolder, setUploadFolder] = useState('')

  // Rebuild tree when files change
  useEffect(() => { setTree(buildTree(files)) }, [files])

  // Load on mount
  useEffect(() => { listFiles(); refreshSize() }, [listFiles, refreshSize])

  // File size map for non-text files
  const fileSizeMap = useMemo(() => {
    const m = new Map<string, number>()
    for (const f of files) if (!f.isDir) m.set(f.path, f.size)
    return m
  }, [files])

  const handleLoadMore = useCallback(async (path: string) => {
    setTree((prev) => setNodeLoading(prev, path, true))
    try {
      const children = await loadSubtree(path)
      setTree((prev) => mergeSubtree(prev, path, children))
    } catch {
      setTree((prev) => setNodeLoading(prev, path, false))
    }
  }, [loadSubtree])

  const handleSelect = useCallback(async (path: string) => {
    setActivePath(path)
    if (isTextFile(path)) {
      setContentLoading(true)
      try {
        const res = await readFile(path)
        setFileContent(res)
      } catch {
        setFileContent(null)
      } finally {
        setContentLoading(false)
      }
    } else {
      const size = fileSizeMap.get(path) ?? 0
      setFileContent({ content: '', path, size })
    }
  }, [readFile, fileSizeMap])

  const handleDeleteRequest = useCallback((path: string, isDir: boolean) => {
    setDeleteTarget({ path, isDir })
  }, [])

  const handleDeleteConfirm = useCallback(async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await deleteFile(deleteTarget.path)
      if (activePath === deleteTarget.path || (deleteTarget.isDir && activePath?.startsWith(deleteTarget.path + '/'))) {
        setActivePath(null)
        setFileContent(null)
      }
      await listFiles()
    } finally {
      setDeleting(false)
      setDeleteTarget(null)
    }
  }, [deleteTarget, deleteFile, listFiles, activePath])

  const handleDownload = useCallback(async (path: string) => {
    try {
      const api = getApiClient()
      const fileName = path.split('/').pop() ?? 'download'
      const url = `${api.getBaseUrl()}/v1/storage/files/${encodeURIComponent(path)}?raw=true&download=true`
      await wails.downloadURL(url, fileName)
    } catch { /* silent */ }
  }, [])

  const handleFetchBlob = useCallback(async (path: string) => {
    return fetchRawBlob(path, false)
  }, [fetchRawBlob])

  const handleRefresh = useCallback(async () => {
    await Promise.all([listFiles(), refreshSize()])
  }, [listFiles, refreshSize])

  const handleMove = useCallback(async (fromPath: string, toFolder: string) => {
    try {
      await moveFile(fromPath, toFolder)
      if (activePath === fromPath || activePath?.startsWith(fromPath + '/')) {
        setActivePath(null)
        setFileContent(null)
      }
      listFiles({ silent: true })
    } catch { /* toast handled in hook */ }
  }, [moveFile, listFiles, activePath])

  // Active folder for scoped uploads
  const activeFolder = useMemo(() => {
    if (!activePath) return ''
    const idx = activePath.lastIndexOf('/')
    return idx > 0 ? activePath.slice(0, idx) : ''
  }, [activePath])

  const handleUploadFile = useCallback(async (file: File) => {
    await uploadFile(file, uploadFolder || undefined)
  }, [uploadFile, uploadFolder])

  const handleUploadClose = useCallback((v: boolean) => {
    setUploadOpen(v)
    if (!v) handleRefresh()
  }, [handleRefresh])

  // Size description
  const sizeStr = sizeLoading ? `${formatSize(totalSize)}...` : formatSize(totalSize)
  const deleteName = deleteTarget?.path.split('/').pop() ?? ''

  return (
    <div className="flex flex-col h-full space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-sm font-semibold text-text-primary">{t('title')}</h2>
          <p className="text-[11px] text-text-muted mt-0.5">
            {baseDir ? t('descriptionWithPath', { path: baseDir, size: sizeStr }) : t('description')}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => { setUploadFolder(activeFolder); setUploadOpen(true) }}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors"
          >
            <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
              <polyline points="17 8 12 3 7 8" />
              <line x1="12" y1="3" x2="12" y2="15" />
            </svg>
            {tc('uploadLabel', 'Upload')}
          </button>
          <RefreshButton onRefresh={handleRefresh} />
        </div>
      </div>

      {/* File browser */}
      <div className="flex-1 flex flex-col min-h-0">
        <StorageFileBrowser
          tree={tree}
          filesLoading={loading}
          activePath={activePath}
          onSelect={handleSelect}
          contentLoading={contentLoading}
          fileContent={fileContent}
          onDelete={handleDeleteRequest}
          onLoadMore={handleLoadMore}
          onMove={handleMove}
          onDownload={handleDownload}
          fetchBlob={handleFetchBlob}
          showSize
        />
      </div>

      {/* Upload dialog */}
      <StorageUploadDialog
        open={uploadOpen}
        onOpenChange={handleUploadClose}
        onUpload={handleUploadFile}
        title={t('upload.title')}
        description={uploadFolder ? `${t('upload.description')} → ${uploadFolder}/` : t('upload.description')}
      />

      {/* Delete confirmation */}
      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => { if (!open) setDeleteTarget(null) }}
        title={deleteTarget?.isDir ? t('delete.folderTitle') : t('delete.fileTitle')}
        description={
          t('delete.description', { name: deleteName })
          + (deleteTarget?.isDir ? t('delete.folderWarning') : '')
          + t('delete.undone')
        }
        variant="destructive"
        confirmLabel={deleting ? t('delete.deleting') : t('delete.confirmLabel')}
        onConfirm={handleDeleteConfirm}
        loading={deleting}
      />
    </div>
  )
}

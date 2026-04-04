// Storage API hooks for file browsing, upload, delete, move, and size streaming.
// Adapted from ui/web/src/pages/storage/hooks/use-storage.ts for desktop edition.

import { useState, useCallback, useEffect, useRef } from 'react'
import { storageService } from '../services/storage-service'

export type { StorageFile, StorageFileContent } from '../services/storage-service'
import type { StorageFile, StorageFileContent } from '../services/storage-service'

export function useStorage() {
  const [files, setFiles] = useState<StorageFile[]>([])
  const [baseDir, setBaseDir] = useState('')
  const [loading, setLoading] = useState(false)

  const listFiles = useCallback(async (opts?: { silent?: boolean }) => {
    if (!opts?.silent) setLoading(true)
    try {
      const res = await storageService.listFiles()
      setFiles(res.files ?? [])
      setBaseDir(res.baseDir ?? '')
    } finally {
      if (!opts?.silent) setLoading(false)
    }
  }, [])

  const loadSubtree = useCallback(async (path: string): Promise<import('../services/storage-service').StorageFile[]> => {
    const res = await storageService.loadSubtree(path)
    return res.files ?? []
  }, [])

  const readFile = useCallback(async (path: string): Promise<StorageFileContent> => {
    return storageService.readFile(path)
  }, [])

  const deleteFile = useCallback(async (path: string) => {
    await storageService.deleteFile(path)
  }, [])

  const uploadFile = useCallback(async (file: File, folder?: string) => {
    await storageService.uploadFile(file, folder)
  }, [])

  const moveFile = useCallback(async (fromPath: string, toFolder: string) => {
    await storageService.moveFile(fromPath, toFolder)
  }, [])

  const fetchRawBlob = useCallback((path: string, download?: boolean): Promise<Blob> => {
    const params: Record<string, string> = { raw: 'true' }
    if (download) params.download = 'true'
    return storageService.fetchBlob(path, params)
  }, [])

  return { files, baseDir, loading, listFiles, loadSubtree, readFile, deleteFile, uploadFile, moveFile, fetchRawBlob }
}

interface SizeState {
  totalSize: number
  fileCount: number
  loading: boolean
  cached: boolean
}

export function useStorageSize() {
  const [state, setState] = useState<SizeState>({ totalSize: 0, fileCount: 0, loading: false, cached: false })
  const abortRef = useRef<AbortController | null>(null)

  const fetchSize = useCallback(async () => {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller

    setState((s) => ({ ...s, loading: true, totalSize: 0, fileCount: 0 }))

    try {
      const res = await storageService.streamSize(controller.signal)
      if (!res.body) {
        setState((s) => ({ ...s, loading: false }))
        return
      }

      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n')
        buffer = lines.pop() ?? ''

        for (const line of lines) {
          if (!line.startsWith('data: ')) continue
          try {
            const data = JSON.parse(line.slice(6))
            if (data.done) {
              setState({ totalSize: data.total, fileCount: data.files, loading: false, cached: !!data.cached })
            } else {
              setState((s) => ({ ...s, totalSize: data.current, fileCount: data.files }))
            }
          } catch { /* skip malformed */ }
        }
      }
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setState((s) => ({ ...s, loading: false }))
    }
  }, [])

  useEffect(() => {
    return () => abortRef.current?.abort()
  }, [])

  return { ...state, refreshSize: fetchSize }
}

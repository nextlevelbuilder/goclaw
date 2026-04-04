import { useState, useCallback } from 'react'

/** A file queued for upload or a direct filesystem path (desktop). */
export interface AttachedFile {
  id: string
  /** Browser File object — present for drag/drop and file picker uploads. */
  file?: File
  /** Direct filesystem path — present for pasted paths (desktop only, skip HTTP upload). */
  localPath?: string
  name: string
  /** Image thumbnail data URL (only for browser File images). */
  preview?: string
}

/** Human-readable file size. */
export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

/** Detect if text looks like one or more absolute file paths (one per line). */
const FILE_PATH_RE = /^(\/[\w.\-/ ]+(?:\.\w+)?|[A-Z]:\\[\w.\-\\ ]+(?:\.\w+)?)$/

export function extractFilePaths(text: string): string[] {
  return text.split('\n').map((l) => l.trim()).filter((l) => FILE_PATH_RE.test(l))
}

const IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp', 'image/svg+xml']

export function useFileAttachments() {
  const [files, setFiles] = useState<AttachedFile[]>([])

  const addFiles = useCallback((incoming: FileList | File[]) => {
    const arr = Array.from(incoming)
    const newFiles: AttachedFile[] = arr.map((f) => ({
      id: crypto.randomUUID().slice(0, 8),
      file: f,
      name: f.name,
    }))

    // Generate image previews
    for (const af of newFiles) {
      if (af.file && IMAGE_TYPES.includes(af.file.type)) {
        const fileObj = af.file
        const reader = new FileReader()
        reader.onload = () => {
          setFiles((prev) => prev.map((f) => f.id === af.id ? { ...f, preview: reader.result as string } : f))
        }
        reader.readAsDataURL(fileObj)
      }
    }

    setFiles((prev) => [...prev, ...newFiles])
  }, [])

  /** Add files by filesystem path (desktop paste). */
  const addLocalPaths = useCallback((paths: string[]) => {
    const newFiles: AttachedFile[] = paths.map((p) => ({
      id: crypto.randomUUID().slice(0, 8),
      localPath: p,
      name: p.split('/').pop() || p.split('\\').pop() || p,
    }))
    setFiles((prev) => [...prev, ...newFiles])
  }, [])

  const removeFile = useCallback((id: string) => {
    setFiles((prev) => prev.filter((f) => f.id !== id))
  }, [])

  const clearFiles = useCallback(() => setFiles([]), [])

  return { files, addFiles, addLocalPaths, removeFile, clearFiles }
}

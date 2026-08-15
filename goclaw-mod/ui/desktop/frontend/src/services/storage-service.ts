// Storage service — wraps HTTP /v1/storage/* calls
import { getApiClient } from '../lib/api'

export interface StorageFile {
  path: string
  name: string
  isDir: boolean
  size: number
  hasChildren?: boolean
  protected: boolean
}

export interface StorageListResponse {
  files: StorageFile[]
  baseDir: string
}

export interface StorageFileContent {
  content: string
  path: string
  size: number
}

export const storageService = {
  listFiles(): Promise<StorageListResponse> {
    return getApiClient().get<StorageListResponse>('/v1/storage/files')
  },

  loadSubtree(path: string): Promise<StorageListResponse> {
    return getApiClient().get<StorageListResponse>(`/v1/storage/files?path=${encodeURIComponent(path)}`)
  },

  readFile(path: string): Promise<StorageFileContent> {
    return getApiClient().get<StorageFileContent>(`/v1/storage/files/${encodeURIComponent(path)}`)
  },

  deleteFile(path: string): Promise<void> {
    return getApiClient().delete<void>(`/v1/storage/files/${encodeURIComponent(path)}`)
  },

  uploadFile(file: File, folder?: string): Promise<unknown> {
    const params = folder ? `?path=${encodeURIComponent(folder)}` : ''
    return getApiClient().uploadFile(`/v1/storage/files${params}`, file)
  },

  moveFile(fromPath: string, toFolder: string): Promise<void> {
    const fileName = fromPath.split('/').pop() ?? fromPath
    const newPath = toFolder ? `${toFolder}/${fileName}` : fileName
    if (fromPath === newPath) return Promise.resolve()
    return getApiClient().putRaw(
      `/v1/storage/move?from=${encodeURIComponent(fromPath)}&to=${encodeURIComponent(newPath)}`,
    )
  },

  fetchBlob(path: string, params?: Record<string, string>): Promise<Blob> {
    return getApiClient().fetchBlob(`/v1/storage/files/${encodeURIComponent(path)}`, params)
  },

  streamSize(signal?: AbortSignal): Promise<Response> {
    return getApiClient().streamFetch('/v1/storage/size', signal)
  },
}

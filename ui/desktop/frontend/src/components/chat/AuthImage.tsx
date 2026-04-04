import { useMediaUrl } from '../../hooks/use-media-url'
import { SaveFile } from '../../../wailsjs/go/main/App'

interface AuthImageProps {
  src: string
  alt?: string
  className?: string
  onClick?: () => void
}

/** Image with blob-cached src to prevent flickering on session switch. */
export function AuthImage({ src, alt, className, onClick }: AuthImageProps) {
  const cachedSrc = useMediaUrl(src)

  if (!cachedSrc) {
    return <div className={`bg-surface-tertiary/50 animate-pulse rounded-lg ${className ?? 'h-40 w-full'}`} />
  }

  return <img src={cachedSrc} alt={alt ?? ''} className={className} loading="lazy" onClick={onClick} />
}

/** Save file via native Save As dialog. Desktop — files are local on disk.
 *  Extracts absolute path from /v1/files/{path} URL and opens Save dialog via Go binding. */
export function downloadFile(url: string, _filename: string) {
  const match = url.match(/\/v1\/files\/(.+?)(?:\?|$)/)
  if (match) {
    const absPath = '/' + decodeURIComponent(match[1])
    SaveFile(absPath)
  }
}

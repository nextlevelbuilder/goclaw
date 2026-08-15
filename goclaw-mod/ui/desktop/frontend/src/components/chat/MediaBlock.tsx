import { useState } from 'react'
import { ImageLightbox } from './ImageLightbox'
import type { LightboxImage } from './ImageLightbox'
import { FileButton } from './FileButton'
import { AuthImage } from './AuthImage'
import { useMediaUrl } from '../../hooks/use-media-url'

interface MediaBlockProps {
  items: { type: string; url: string }[]
}

// Strip timestamp from filename: "file.1774537056.md" → "file.md"
function cleanFilename(url: string): string {
  const basename = url.split('/').pop()?.split('?')[0] ?? 'file'
  return basename.replace(/\.\d{9,}(\.\w+)$/, '$1')
}

// Audio/video with authenticated blob URL
function AuthAudio({ src }: { src: string }) {
  const blobUrl = useMediaUrl(src)
  if (!blobUrl) return <div className="h-10 w-48 bg-surface-tertiary/50 animate-pulse rounded-lg" />
  return <audio src={blobUrl} controls className="max-w-xs" />
}

function AuthVideo({ src }: { src: string }) {
  const blobUrl = useMediaUrl(src)
  if (!blobUrl) return <div className="h-40 w-64 bg-surface-tertiary/50 animate-pulse rounded-lg" />
  return <video src={blobUrl} controls className="max-w-sm rounded-lg" />
}

export function MediaBlock({ items }: MediaBlockProps) {
  const [lightboxIndex, setLightboxIndex] = useState<number | null>(null)

  const images = items.filter((i) => i.type.startsWith('image/'))
  const others = items.filter((i) => !i.type.startsWith('image/'))

  const galleryImages: LightboxImage[] = images.map((i) => ({ src: i.url }))

  return (
    <div className="space-y-2 my-2">
      {/* Image grid */}
      {images.length > 0 && (
        <div className={`grid gap-2 ${images.length > 1 ? 'grid-cols-2' : 'grid-cols-1'}`}>
          {images.map((item, i) => (
            <button
              key={i}
              type="button"
              onClick={() => setLightboxIndex(i)}
              className="group relative overflow-hidden rounded-lg border border-border cursor-pointer"
            >
              <AuthImage src={item.url} alt="" className="max-h-72 w-full object-contain" />
              <div className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/40 via-transparent to-transparent opacity-0 transition-opacity group-hover:opacity-100" />
            </button>
          ))}
        </div>
      )}

      {/* Non-image media */}
      {others.map((item, i) => {
        if (item.type.startsWith('audio/')) {
          return <AuthAudio key={`a-${i}`} src={item.url} />
        }
        if (item.type.startsWith('video/')) {
          return <AuthVideo key={`v-${i}`} src={item.url} />
        }
        return (
          <FileButton
            key={`f-${i}`}
            url={item.url}
            filename={cleanFilename(item.url)}
            mimeType={item.type}
          />
        )
      })}

      {/* Lightbox */}
      {lightboxIndex !== null && galleryImages[lightboxIndex] && (
        <ImageLightbox
          src={galleryImages[lightboxIndex].src}
          onClose={() => setLightboxIndex(null)}
          images={galleryImages}
          currentIndex={lightboxIndex}
          onNavigate={setLightboxIndex}
        />
      )}
    </div>
  )
}

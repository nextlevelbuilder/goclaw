import { useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { AuthImage, downloadFile } from './AuthImage'

export interface LightboxImage {
  src: string
  alt?: string
}

interface ImageLightboxProps {
  src: string
  alt?: string
  onClose: () => void
  /** Gallery mode */
  images?: LightboxImage[]
  currentIndex?: number
  onNavigate?: (index: number) => void
}

const btnClass =
  'rounded-full bg-white/90 dark:bg-neutral-800/90 p-2.5 text-neutral-700 dark:text-neutral-200 shadow-md ring-1 ring-black/10 dark:ring-white/10 hover:bg-white dark:hover:bg-neutral-700 transition-colors cursor-pointer'

export function ImageLightbox(props: ImageLightboxProps) {
  const { t } = useTranslation('common')
  const { onClose, images, currentIndex, onNavigate } = props
  const isGallery = images && images.length > 1 && onNavigate && currentIndex != null
  const current = isGallery ? images[currentIndex] : { src: props.src, alt: props.alt }
  const canPrev = isGallery && currentIndex > 0
  const canNext = isGallery && currentIndex < images.length - 1

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') { e.preventDefault(); onClose() }
      if (isGallery) {
        if (e.key === 'ArrowLeft' && canPrev) { e.preventDefault(); onNavigate(currentIndex - 1) }
        if (e.key === 'ArrowRight' && canNext) { e.preventDefault(); onNavigate(currentIndex + 1) }
      }
    },
    [onClose, isGallery, canPrev, canNext, onNavigate, currentIndex],
  )

  useEffect(() => {
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [handleKeyDown])

  return (
    <div
      className="fixed inset-0 z-[100] flex flex-col items-center justify-center bg-black/80 backdrop-blur-sm"
      onClick={onClose}
    >
      {/* Close + Download toolbar */}
      <div className="absolute top-4 right-4 flex items-center gap-2">
        <button
          type="button"
          onClick={(e) => { e.stopPropagation(); downloadFile(current.src, current.src.split('/').pop()?.split('?')[0] ?? 'image') }}
          className={btnClass}
          title={t('download')}
        >
          <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><polyline points="7 10 12 15 17 10" /><line x1="12" y1="15" x2="12" y2="3" />
          </svg>
        </button>
        <button type="button" onClick={onClose} className={btnClass}>
          {/* X icon */}
          <svg className="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
          </svg>
        </button>
      </div>

      {/* Prev arrow */}
      {canPrev && (
        <button
          type="button"
          onClick={(e) => { e.stopPropagation(); onNavigate(currentIndex - 1) }}
          className={`absolute left-4 top-1/2 -translate-y-1/2 ${btnClass}`}
        >
          <svg className="h-6 w-6" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <polyline points="15 18 9 12 15 6" />
          </svg>
        </button>
      )}

      {/* Next arrow */}
      {canNext && (
        <button
          type="button"
          onClick={(e) => { e.stopPropagation(); onNavigate(currentIndex + 1) }}
          className={`absolute right-4 top-1/2 -translate-y-1/2 ${btnClass}`}
        >
          <svg className="h-6 w-6" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <polyline points="9 18 15 12 9 6" />
          </svg>
        </button>
      )}

      {/* Image */}
      <span onClick={(e) => e.stopPropagation()}>
        <AuthImage
          src={current.src}
          alt={current.alt ?? 'image'}
          className="max-h-[85vh] max-w-[90vw] rounded-lg object-contain"
        />
      </span>

      {/* Gallery counter */}
      {isGallery && (
        <div
          className="mt-3 rounded-full bg-black/60 px-4 py-1.5 text-sm text-white/90 tabular-nums"
          onClick={(e) => e.stopPropagation()}
        >
          {currentIndex + 1} / {images.length}
        </div>
      )}
    </div>
  )
}

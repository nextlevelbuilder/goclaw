import { useState, useCallback } from 'react'

const MIN_SPIN_MS = 500

interface RefreshButtonProps {
  onRefresh: () => Promise<void>
  className?: string
}

export function RefreshButton({ onRefresh, className }: RefreshButtonProps) {
  const [spinning, setSpinning] = useState(false)

  const handleClick = useCallback(async () => {
    if (spinning) return
    setSpinning(true)
    const min = new Promise((r) => setTimeout(r, MIN_SPIN_MS))
    try {
      await Promise.all([onRefresh(), min])
    } finally {
      setSpinning(false)
    }
  }, [onRefresh, spinning])

  return (
    <button
      onClick={handleClick}
      className={`p-1.5 text-text-muted hover:text-text-primary transition-colors ${className ?? ''}`}
      title="Refresh"
    >
      <svg
        className={`h-4 w-4 ${spinning ? 'animate-spin' : ''}`}
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth={2}
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8" />
        <path d="M21 3v5h-5" />
        <path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16" />
        <path d="M3 21v-5h5" />
      </svg>
    </button>
  )
}

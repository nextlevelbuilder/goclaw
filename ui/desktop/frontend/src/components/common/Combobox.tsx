import { useState, useRef, useEffect, useCallback, useLayoutEffect, type KeyboardEvent } from 'react'
import { createPortal } from 'react-dom'

interface ComboboxOption {
  value: string
  label: string
}

interface ComboboxProps {
  value: string
  onChange: (value: string) => void
  options: ComboboxOption[]
  placeholder?: string
  loading?: boolean
  allowCustom?: boolean
  disabled?: boolean
}

export function Combobox({ value, onChange, options, placeholder, loading, allowCustom = true, disabled }: ComboboxProps) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [highlightIdx, setHighlightIdx] = useState(-1)
  const [pos, setPos] = useState<{ top: number; left: number; width: number }>({ top: 0, left: 0, width: 0 })
  const inputRef = useRef<HTMLInputElement>(null)
  const dropdownRef = useRef<HTMLDivElement>(null)
  const itemRefs = useRef<(HTMLButtonElement | null)[]>([])

  const filtered = options.filter((o) =>
    o.label.toLowerCase().includes(search.toLowerCase()) ||
    o.value.toLowerCase().includes(search.toLowerCase())
  )

  const displayValue = options.find((o) => o.value === value)?.label || value

  useEffect(() => { setHighlightIdx(-1) }, [search])

  // Calculate dropdown position — track scroll/resize
  useLayoutEffect(() => {
    if (!open || !inputRef.current) return
    const update = () => {
      const rect = inputRef.current!.getBoundingClientRect()
      setPos({ top: rect.bottom + 4, left: rect.left, width: rect.width })
    }
    update()
    window.addEventListener('scroll', update, true)
    window.addEventListener('resize', update)
    return () => {
      window.removeEventListener('scroll', update, true)
      window.removeEventListener('resize', update)
    }
  }, [open])

  // Close on outside click
  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (dropdownRef.current?.contains(e.target as Node)) return
      if (inputRef.current?.contains(e.target as Node)) return
      setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  const handleSelect = useCallback((val: string) => {
    onChange(val)
    setSearch('')
    setOpen(false)
    inputRef.current?.blur()
  }, [onChange])

  const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (!open) return

    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        setHighlightIdx((prev) => {
          const next = prev < filtered.length - 1 ? prev + 1 : 0
          itemRefs.current[next]?.scrollIntoView({ block: 'nearest' })
          return next
        })
        break
      case 'ArrowUp':
        e.preventDefault()
        setHighlightIdx((prev) => {
          const next = prev > 0 ? prev - 1 : filtered.length - 1
          itemRefs.current[next]?.scrollIntoView({ block: 'nearest' })
          return next
        })
        break
      case 'Enter':
        e.preventDefault()
        if (highlightIdx >= 0 && filtered[highlightIdx]) {
          handleSelect(filtered[highlightIdx].value)
        }
        break
      case 'Escape':
        e.preventDefault()
        setOpen(false)
        inputRef.current?.blur()
        break
    }
  }

  return (
    <div className="relative">
      <input
        ref={inputRef}
        type="text"
        value={open ? search : displayValue}
        onChange={(e) => { setSearch(e.target.value); if (allowCustom) onChange(e.target.value) }}
        onFocus={() => { setOpen(true); setSearch('') }}
        onKeyDown={handleKeyDown}
        placeholder={loading ? 'Loading...' : placeholder}
        disabled={disabled || loading}
        className="w-full bg-surface-tertiary border border-border rounded-lg px-2.5 py-1.5 pr-7 text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
      />
      {/* Dropdown chevron */}
      <div className="absolute right-2.5 top-1/2 -translate-y-1/2 pointer-events-none text-text-muted">
        <svg width="12" height="12" viewBox="0 0 12 12" fill="none"><path d="M3 4.5l3 3 3-3" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" /></svg>
      </div>

      {/* Portal dropdown — escapes overflow:hidden parents */}
      {open && filtered.length > 0 && createPortal(
        <div
          ref={dropdownRef}
          className="fixed z-[80] max-h-48 overflow-y-auto bg-surface-secondary border border-border rounded-lg shadow-lg py-1 pointer-events-auto"
          style={{ top: pos.top, left: pos.left, width: pos.width }}
        >
          {filtered.map((o, idx) => (
            <button
              key={o.value}
              ref={(el) => { itemRefs.current[idx] = el }}
              onClick={() => handleSelect(o.value)}
              className={[
                'w-full text-left px-3 py-1.5 text-sm transition-colors',
                idx === highlightIdx ? 'bg-surface-tertiary' : '',
                o.value === value ? 'text-accent font-medium' : 'text-text-primary',
                idx !== highlightIdx ? 'hover:bg-surface-tertiary' : '',
              ].filter(Boolean).join(' ')}
            >
              {o.label}
            </button>
          ))}
        </div>,
        document.body,
      )}
    </div>
  )
}

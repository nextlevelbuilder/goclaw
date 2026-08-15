/**
 * Shared SVG icon components — replaces inline SVGs across the desktop UI.
 * All icons use currentColor and accept className for sizing/color overrides.
 */

interface IconProps {
  size?: number
  className?: string
}

function svg(size: number, className: string | undefined, children: React.ReactNode, extra?: Record<string, unknown>) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      {...extra}
    >
      {children}
    </svg>
  )
}

export function IconClose({ size = 18, className }: IconProps) {
  return svg(size, className, <>
    <line x1="18" y1="6" x2="6" y2="18" />
    <line x1="6" y1="6" x2="18" y2="18" />
  </>)
}

export function IconChevronDown({ size = 14, className }: IconProps) {
  return svg(size, className, <polyline points="6 9 12 15 18 9" />)
}

export function IconChevronLeft({ size = 16, className }: IconProps) {
  return svg(size, className, <polyline points="15 18 9 12 15 6" />)
}

export function IconPlus({ size = 14, className }: IconProps) {
  return svg(size, className, <>
    <line x1="12" y1="5" x2="12" y2="19" />
    <line x1="5" y1="12" x2="19" y2="12" />
  </>, { strokeWidth: 2.5 })
}

export function IconGear({ size = 14, className }: IconProps) {
  return svg(size, className, <>
    <circle cx="12" cy="12" r="3" />
    <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
  </>)
}

export function IconTrash({ size = 14, className }: IconProps) {
  return svg(size, className, <>
    <polyline points="3 6 5 6 21 6" />
    <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
  </>)
}

export function IconDocument({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
    <polyline points="14 2 14 8 20 8" />
    <line x1="16" y1="13" x2="8" y2="13" />
    <line x1="16" y1="17" x2="8" y2="17" />
    <polyline points="10 9 9 9 8 9" />
  </>)
}

export function IconCheckCircle({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" />
    <polyline points="22 4 12 14.01 9 11.01" />
  </>)
}

export function IconCheck({ size = 14, className }: IconProps) {
  return svg(size, className, <polyline points="20 6 9 17 4 12" />)
}

export function IconBlocked({ size = 14, className }: IconProps) {
  return svg(size, className, <>
    <circle cx="12" cy="12" r="10" />
    <line x1="4.93" y1="4.93" x2="19.07" y2="19.07" />
  </>)
}

export function IconChat({ size = 20, className }: IconProps) {
  return svg(size, className, <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />)
}

export function IconUser({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <rect x="3" y="11" width="18" height="11" rx="2" />
    <circle cx="12" cy="5" r="4" />
  </>)
}

export function IconPaperclip({ size = 10, className }: IconProps) {
  return svg(size, className, <path d="M21.44 11.05l-9.19 9.19a6 6 0 0 1-8.49-8.49l9.19-9.19a4 4 0 0 1 5.66 5.66l-9.2 9.19a2 2 0 0 1-2.83-2.83l8.49-8.48" />)
}

export function IconSpinner({ size = 14, className }: IconProps) {
  return (
    <div
      className={`border-2 border-current border-t-transparent rounded-full animate-spin ${className ?? ''}`}
      style={{ width: size, height: size }}
    />
  )
}

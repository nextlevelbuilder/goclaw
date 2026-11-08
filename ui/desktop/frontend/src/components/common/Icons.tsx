/**
 * Shared SVG icon components — replaces inline SVGs across the desktop UI.
 * Lucide-style 24px paths drawn with a 1.5 stroke in currentColor. Icons are
 * decorative (aria-hidden); the control that holds them carries the label.
 */

export interface IconProps {
  size?: number
  className?: string
}

function svg(size: number, className: string | undefined, children: React.ReactNode) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      className={className}
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
  </>)
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

export function IconAlertTriangle({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3" />
    <path d="M12 9v4" />
    <path d="M12 17h.01" />
  </>)
}

export function IconXCircle({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <circle cx="12" cy="12" r="10" />
    <path d="m15 9-6 6" />
    <path d="m9 9 6 6" />
  </>)
}

export function IconInfo({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <circle cx="12" cy="12" r="10" />
    <path d="M12 16v-4" />
    <path d="M12 8h.01" />
  </>)
}

export function IconDot({ size = 16, className }: IconProps) {
  return svg(size, className, <circle cx="12" cy="12" r="5" />)
}

export function IconMore({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <circle cx="5" cy="12" r="1" />
    <circle cx="12" cy="12" r="1" />
    <circle cx="19" cy="12" r="1" />
  </>)
}

export function IconSearch({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <circle cx="11" cy="11" r="7" />
    <path d="m21 21-4.3-4.3" />
  </>)
}

export function IconMinus({ size = 16, className }: IconProps) {
  return svg(size, className, <path d="M5 12h14" />)
}

export function IconSquare({ size = 16, className }: IconProps) {
  return svg(size, className, <rect x="4" y="4" width="16" height="16" rx="2" />)
}

export function IconRestore({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <rect x="8" y="8" width="12" height="12" rx="2" />
    <path d="M4 16V6a2 2 0 0 1 2-2h10" />
  </>)
}

export function IconPanelLeft({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <rect x="3" y="3" width="18" height="18" rx="2" />
    <path d="M9 3v18" />
  </>)
}

export function IconChevronsUpDown({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <path d="m7 15 5 5 5-5" />
    <path d="m7 9 5-5 5 5" />
  </>)
}

export function IconHome({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <path d="M15 21v-8a1 1 0 0 0-1-1h-4a1 1 0 0 0-1 1v8" />
    <path d="M3 10a2 2 0 0 1 .709-1.528l7-5.999a2 2 0 0 1 2.582 0l7 5.999A2 2 0 0 1 21 10v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
  </>)
}

export function IconUsers({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
    <circle cx="9" cy="7" r="4" />
    <path d="M22 21v-2a4 4 0 0 0-3-3.87" />
    <path d="M16 3.13a4 4 0 0 1 0 7.75" />
  </>)
}

export function IconBot({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <path d="M12 8V4H8" />
    <rect x="4" y="8" width="16" height="12" rx="2" />
    <path d="M2 14h2" />
    <path d="M20 14h2" />
    <path d="M15 13v2" />
    <path d="M9 13v2" />
  </>)
}

export function IconStar({ size = 16, className }: IconProps) {
  return svg(size, className, <path d="M11.525 2.295a.53.53 0 0 1 .95 0l2.31 4.679a2.123 2.123 0 0 0 1.595 1.16l5.166.756a.53.53 0 0 1 .294.904l-3.736 3.638a2.123 2.123 0 0 0-.611 1.878l.882 5.14a.53.53 0 0 1-.771.56l-4.618-2.428a2.122 2.122 0 0 0-1.973 0L6.396 21.01a.53.53 0 0 1-.77-.56l.881-5.139a2.122 2.122 0 0 0-.611-1.879L2.16 9.795a.53.53 0 0 1 .294-.906l5.165-.755a2.122 2.122 0 0 0 1.597-1.16z" />)
}

export function IconWrench({ size = 16, className }: IconProps) {
  return svg(size, className, <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" />)
}

export function IconActivity({ size = 16, className }: IconProps) {
  return svg(size, className, <path d="M22 12h-2.48a2 2 0 0 0-1.93 1.46l-2.35 8.36a.25.25 0 0 1-.48 0L9.24 2.18a.25.25 0 0 0-.48 0l-2.35 8.36A2 2 0 0 1 4.49 12H2" />)
}

export function IconPlug({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <path d="M12 22v-5" />
    <path d="M9 8V2" />
    <path d="M15 8V2" />
    <path d="M18 8v5a4 4 0 0 1-4 4h-4a4 4 0 0 1-4-4V8Z" />
  </>)
}

export function IconRadio({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <path d="M4.9 19.1C1 15.2 1 8.8 4.9 4.9" />
    <path d="M7.8 16.2c-2.3-2.3-2.3-6.1 0-8.5" />
    <circle cx="12" cy="12" r="2" />
    <path d="M16.2 7.8c2.3 2.3 2.3 6.1 0 8.5" />
    <path d="M19.1 4.9C23 8.8 23 15.1 19.1 19" />
  </>)
}

export function IconHardDrive({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <path d="M22 12H2" />
    <path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z" />
    <path d="M6 16h.01" />
    <path d="M10 16h.01" />
  </>)
}

export function IconTrendingUp({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <polyline points="22 7 13.5 15.5 8.5 10.5 2 17" />
    <polyline points="16 7 22 7 22 13" />
  </>)
}

export function IconBarChart({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <path d="M3 3v16a2 2 0 0 0 2 2h16" />
    <path d="M18 17V9" />
    <path d="M13 17V5" />
    <path d="M8 17v-3" />
  </>)
}

export function IconClock({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <circle cx="12" cy="12" r="10" />
    <polyline points="12 6 12 12 16 14" />
  </>)
}

export function IconSun({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <circle cx="12" cy="12" r="4" />
    <path d="M12 2v2" />
    <path d="M12 20v2" />
    <path d="m4.93 4.93 1.41 1.41" />
    <path d="m17.66 17.66 1.41 1.41" />
    <path d="M2 12h2" />
    <path d="M20 12h2" />
    <path d="m6.34 17.66-1.41 1.41" />
    <path d="m19.07 4.93-1.41 1.41" />
  </>)
}

export function IconMoon({ size = 16, className }: IconProps) {
  return svg(size, className, <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />)
}

export function IconRefresh({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8" />
    <path d="M21 3v5h-5" />
    <path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16" />
    <path d="M8 16H3v5" />
  </>)
}

export function IconWifiOff({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <path d="M12 20h.01" />
    <path d="M8.5 16.429a5 5 0 0 1 7 0" />
    <path d="M5 12.859a10 10 0 0 1 5.17-2.69" />
    <path d="M19 12.859a10 10 0 0 0-2.007-1.523" />
    <path d="M2 8.82a15 15 0 0 1 4.177-2.643" />
    <path d="M22 8.82a15 15 0 0 0-11.288-3.764" />
    <path d="m2 2 20 20" />
  </>)
}

export function IconDownload({ size = 16, className }: IconProps) {
  return svg(size, className, <>
    <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
    <polyline points="7 10 12 15 17 10" />
    <path d="M12 15V3" />
  </>)
}

export function IconInbox({ size = 20, className }: IconProps) {
  return svg(size, className, <>
    <polyline points="22 12 16 12 14 15 10 15 8 12 2 12" />
    <path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z" />
  </>)
}

export function IconSpinner({ size = 14, className }: IconProps) {
  return (
    <span
      aria-hidden="true"
      className={`inline-block shrink-0 border-2 border-current border-t-transparent rounded-full animate-spin ${className ?? ''}`}
      style={{ width: size, height: size }}
    />
  )
}

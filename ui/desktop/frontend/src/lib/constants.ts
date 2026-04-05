// Supported languages for the desktop app
export const LANGUAGES = [
  { value: 'en', label: 'EN', flag: '🇺🇸' },
  { value: 'vi', label: 'VI', flag: '🇻🇳' },
  { value: 'zh', label: 'ZH', flag: '🇨🇳' },
] as const

// Fallback timezone list if Intl.supportedValuesOf is unavailable
export const COMMON_TIMEZONES = [
  'UTC', 'America/New_York', 'America/Chicago', 'America/Denver', 'America/Los_Angeles',
  'America/Sao_Paulo', 'Europe/London', 'Europe/Paris', 'Europe/Berlin', 'Europe/Moscow',
  'Asia/Dubai', 'Asia/Kolkata', 'Asia/Bangkok', 'Asia/Saigon', 'Asia/Ho_Chi_Minh',
  'Asia/Shanghai', 'Asia/Tokyo', 'Asia/Seoul', 'Asia/Singapore', 'Asia/Hong_Kong',
  'Australia/Sydney', 'Pacific/Auckland',
]

// Returns all IANA timezones if browser supports it, otherwise fallback list
export function getAllTimezones(): string[] {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const intlAny = Intl as any
  return typeof intlAny.supportedValuesOf === 'function'
    ? intlAny.supportedValuesOf('timeZone')
    : COMMON_TIMEZONES
}

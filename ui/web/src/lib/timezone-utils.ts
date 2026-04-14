/** "auto" = browser's local timezone. */
export const TIMEZONE_OPTIONS = [
  { value: "auto", label: "Auto (Local)" },
  { value: "UTC", label: "UTC" },
  { value: "America/New_York", label: "New York (ET)" },
  { value: "America/Chicago", label: "Chicago (CT)" },
  { value: "America/Los_Angeles", label: "Los Angeles (PT)" },
  { value: "Europe/London", label: "London (GMT/BST)" },
  { value: "Europe/Paris", label: "Paris (CET)" },
  { value: "Asia/Tokyo", label: "Tokyo (JST)" },
  { value: "Asia/Shanghai", label: "Shanghai (CST)" },
  { value: "Asia/Ho_Chi_Minh", label: "Ho Chi Minh (ICT)" },
  { value: "Asia/Singapore", label: "Singapore (SGT)" },
  { value: "Australia/Sydney", label: "Sydney (AEST)" },
] as const;

/** Compute UTC offset string for a timezone, e.g. "UTC+7", "UTC-5:30" */
function formatUtcOffset(tz: string): string {
  const parts = new Intl.DateTimeFormat("en-US", { timeZone: tz, timeZoneName: "shortOffset" }).formatToParts(new Date());
  const raw = parts.find((p) => p.type === "timeZoneName")?.value ?? "GMT";
  return raw.replace("GMT", "UTC").replace(/^UTC$/, "UTC+0");
}

function parseOffsetMinutes(offset: string): number {
  const m = offset.match(/UTC([+-]?)(\d+)(?::(\d+))?/);
  if (!m) return 0;
  const sign = m[1] === "-" ? -1 : 1;
  return sign * (parseInt(m[2]!, 10) * 60 + parseInt(m[3] ?? "0", 10));
}

type TzOption = { value: string; label: string };
let _cachedTimezones: TzOption[] | undefined;

/** Minimal fallback when Intl.supportedValuesOf is unavailable. */
const FALLBACK_TIMEZONES: string[] = [
  "UTC", "America/New_York", "America/Chicago", "America/Denver", "America/Los_Angeles",
  "America/Halifax", "America/Sao_Paulo", "Europe/London", "Europe/Paris", "Europe/Berlin",
  "Europe/Moscow", "Asia/Dubai", "Asia/Kolkata", "Asia/Bangkok", "Asia/Ho_Chi_Minh",
  "Asia/Shanghai", "Asia/Tokyo", "Asia/Seoul", "Asia/Singapore", "Australia/Sydney",
  "Pacific/Auckland",
];

/** All IANA timezones with dynamic UTC offsets, sorted by offset then name. */
export function getAllIanaTimezones(): TzOption[] {
  if (_cachedTimezones) return _cachedTimezones;
  let tzNames: string[];
  try {
     
    tzNames = (Intl as any).supportedValuesOf("timeZone") as string[];
  } catch {
    tzNames = FALLBACK_TIMEZONES;
  }
  // Chrome may omit "UTC" from the list
  if (!tzNames.includes("UTC")) tzNames = ["UTC", ...tzNames];
  const entries = tzNames.map((tz: string) => {
    const offset = formatUtcOffset(tz);
    return { value: tz, label: `${tz} (${offset})`, _offset: parseOffsetMinutes(offset) };
  });
  entries.sort((a: { _offset: number; value: string }, b: { _offset: number; value: string }) =>
    a._offset !== b._offset ? a._offset - b._offset : a.value.localeCompare(b.value),
  );
  _cachedTimezones = entries.map(({ value, label }: { value: string; label: string }) => ({ value, label }));
  return _cachedTimezones;
}

let _cachedTzSet: Set<string> | undefined;

/** Check if a value is a valid IANA timezone from the dynamic list. */
export function isValidIanaTimezone(tz: string): boolean {
  if (!_cachedTzSet) {
    _cachedTzSet = new Set(getAllIanaTimezones().map((t) => t.value));
  }
  return _cachedTzSet.has(tz);
}

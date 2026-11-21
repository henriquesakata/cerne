// The one place the UI formats an instant with a time of day (F-0493).
// Automatic follows the computer — the browser's first usable locale and its
// time zone — and a choice made by hand in Settings overrides each one. The
// interface language (PT/EN) only decides the texts, never the date format.
// UTC-pinned dates (Costs, the dashboard windows) do not come through here.

export type InstantStyle =
  | 'short' // day, month, hour, minute: the channel keeps 24h
  | 'datetime' // short date and time, the year included
  | 'time' // hour and minute
  | 'full' // whole date and time, with the zone (titles, the preview)

// timeZone undefined = the zone the browser assumes.
export type DatePrefs = { locale: string; timeZone?: string }
// What the person picked, already validated; a missing field is automatic.
export type StoredDatePrefs = { locale?: string; timeZone?: string }

// Next to cerne-lang (i18n.tsx): a viewer's choice, kept in this browser.
export const DATE_LOCALE_KEY = 'cerne-date-locale'
export const TIME_ZONE_KEY = 'cerne-time-zone'

const OPTIONS: Record<InstantStyle, Intl.DateTimeFormatOptions> = {
  short: { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' },
  datetime: { dateStyle: 'short', timeStyle: 'short' },
  time: { hour: '2-digit', minute: '2-digit' },
  full: {
    year: 'numeric',
    month: 'numeric',
    day: 'numeric',
    hour: 'numeric',
    minute: 'numeric',
    second: 'numeric',
    timeZoneName: 'short',
  },
}

export function isValidLocale(locale: string): boolean {
  try {
    return Intl.DateTimeFormat.supportedLocalesOf([locale]).length > 0
  } catch {
    return false
  }
}

export function isValidTimeZone(timeZone: string): boolean {
  try {
    new Intl.DateTimeFormat('en', { timeZone })
    return true
  } catch {
    return false
  }
}

// The computer's locale: the first usable entry of navigator.languages, else
// the interface language (fallback) when the browser gives none that works.
export function detectLocale(fallback: string): string {
  const asked = typeof navigator === 'undefined' ? [] : (navigator.languages ?? [])
  return asked.find(isValidLocale) ?? fallback
}

// The computer's zone; undefined when the browser does not say (the formatter
// then assumes its own).
export function detectTimeZone(): string | undefined {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || undefined
  } catch {
    return undefined
  }
}

export function resolvePrefs(stored: StoredDatePrefs, lang: string): DatePrefs {
  return { locale: stored.locale ?? detectLocale(lang), timeZone: stored.timeZone ?? detectTimeZone() }
}

// What localStorage holds, validated: an unknown locale or zone, or a storage
// that throws, reads as automatic.
export function readStored(): StoredDatePrefs {
  const read = (key: string, valid: (v: string) => boolean) => {
    try {
      const v = localStorage.getItem(key)
      return v !== null && valid(v) ? v : undefined
    } catch {
      return undefined
    }
  }
  return {
    locale: read(DATE_LOCALE_KEY, isValidLocale),
    timeZone: read(TIME_ZONE_KEY, isValidTimeZone),
  }
}

// Writes a choice, or clears the key for undefined (back to automatic).
// False when the storage refuses: the caller stays on what it had.
export function writeStored(key: string, value: string | undefined): boolean {
  try {
    if (value === undefined) localStorage.removeItem(key)
    else localStorage.setItem(key, value)
    return true
  } catch {
    return false
  }
}

// One formatter per locale, zone and style: lists format many rows.
const formatters = new Map<string, Intl.DateTimeFormat>()

// '' for an empty or invalid instant: the caller shows nothing.
export function formatInstant(iso: string, style: InstantStyle, prefs: DatePrefs): string {
  const at = new Date(iso)
  if (iso === '' || Number.isNaN(at.getTime())) return ''
  const key = `${prefs.locale}|${prefs.timeZone ?? ''}|${style}`
  let f = formatters.get(key)
  if (!f) {
    f = new Intl.DateTimeFormat(prefs.locale, { ...OPTIONS[style], timeZone: prefs.timeZone })
    formatters.set(key, f)
  }
  return f.format(at)
}

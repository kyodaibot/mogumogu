// Date helpers for the product time zone, Asia/Tokyo (UTC+09:00, no DST).
//
// Calendar dates are plain "YYYY-MM-DD" strings. Arithmetic on them uses UTC
// so the result never depends on the browser's own time zone.

import { locale, type Locale } from '../i18n'

export const TOKYO = 'Asia/Tokyo'
const TOKYO_OFFSET = '+09:00'
const TOKYO_OFFSET_MS = 9 * 60 * 60 * 1000
const DAY_MS = 24 * 60 * 60 * 1000

export type Ymd = string

const ymdFormatter = new Intl.DateTimeFormat('en-CA', {
  timeZone: TOKYO,
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
})

/** Today's date in Tokyo. */
export function todayTokyo(now: Date = new Date()): Ymd {
  return ymdFormatter.format(now)
}

function parts(ymd: Ymd): [number, number, number] {
  const [y, m, d] = ymd.split('-').map(Number)
  return [y, m, d]
}

function fromUtc(t: number): Ymd {
  return new Date(t).toISOString().slice(0, 10)
}

function utcMidnight(ymd: Ymd): number {
  const [y, m, d] = parts(ymd)
  return Date.UTC(y, m - 1, d)
}

export function addDays(ymd: Ymd, days: number): Ymd {
  return fromUtc(utcMidnight(ymd) + days * DAY_MS)
}

/** 0 = Monday … 6 = Sunday. */
export function weekdayMon0(ymd: Ymd): number {
  return (new Date(utcMidnight(ymd)).getUTCDay() + 6) % 7
}

export function ymd(year: number, month: number, day: number): Ymd {
  return fromUtc(Date.UTC(year, month - 1, day))
}

export function yearMonthOf(date: Ymd): { year: number; month: number } {
  const [year, month] = parts(date)
  return { year, month }
}

export function shiftMonth(year: number, month: number, delta: number): { year: number; month: number } {
  const d = new Date(Date.UTC(year, month - 1 + delta, 1))
  return { year: d.getUTCFullYear(), month: d.getUTCMonth() + 1 }
}

export interface MonthGrid {
  /** First and last date shown (Monday of the first week, Sunday of the last). */
  start: Ymd
  end: Ymd
  /** 4–6 rows of 7 dates, Monday first. */
  weeks: Ymd[][]
}

/** Month view grid: weeks start on Monday and cover the whole month. */
export function monthGrid(year: number, month: number): MonthGrid {
  const first = ymd(year, month, 1)
  const last = addDays(ymd(year, month + 1, 1), -1)
  const start = addDays(first, -weekdayMon0(first))
  const end = addDays(last, 6 - weekdayMon0(last))

  const weeks: Ymd[][] = []
  for (let day = start; day <= end; day = addDays(day, 7)) {
    weeks.push(Array.from({ length: 7 }, (_, i) => addDays(day, i)))
  }
  return { start, end, weeks }
}

/** The instant a Tokyo date begins. */
export function dayStart(date: Ymd): Date {
  return new Date(`${date}T00:00:00${TOKYO_OFFSET}`)
}

/** True if [startsAt, endsAt) overlaps the Tokyo day (same rule as the API). */
export function overlapsDay(startsAt: string, endsAt: string, date: Ymd): boolean {
  return new Date(startsAt) < dayStart(addDays(date, 1)) && new Date(endsAt) > dayStart(date)
}

/** "YYYY-MM-DDTHH:mm" in Tokyo, the value format of <input type="datetime-local">. */
export function toTokyoInput(instant: string): string {
  return new Date(new Date(instant).getTime() + TOKYO_OFFSET_MS).toISOString().slice(0, 16)
}

/** Interprets a datetime-local value as Tokyo time and returns RFC 3339. */
export function fromTokyoInput(value: string): string {
  return `${value}:00${TOKYO_OFFSET}`
}

/** "HH:mm" in Tokyo, prefixed with "M/D " when not on `onDate`. */
export function formatTime(instant: string, onDate: Ymd): string {
  const local = toTokyoInput(instant) // YYYY-MM-DDTHH:mm
  const time = local.slice(11, 16)
  if (local.slice(0, 10) === onDate) return time
  const [, m, d] = parts(local.slice(0, 10))
  return `${m}/${d} ${time}`
}

// Display formats follow the interface language. Dates are formatted as UTC
// midnight in a UTC formatter, so the browser's time zone never shifts them.

function displayFormat(options: Intl.DateTimeFormatOptions): (date: Ymd) => string {
  const cache = new Map<Locale, Intl.DateTimeFormat>()
  return (date) => {
    let f = cache.get(locale.value)
    if (!f) {
      f = new Intl.DateTimeFormat(locale.value, { timeZone: 'UTC', ...options })
      cache.set(locale.value, f)
    }
    return f.format(new Date(utcMidnight(date)))
  }
}

/** "10月23日(金)" / "Fri, Oct 23" */
export const formatDayTitle = displayFormat({ month: 'short', day: 'numeric', weekday: 'short' })

/** "10月23日" / "Oct 23" */
export const formatShortDate = displayFormat({ month: 'short', day: 'numeric' })

const formatYearMonth = displayFormat({ year: 'numeric', month: 'long' })
const formatMonth = displayFormat({ month: 'short' })
const formatYearOnly = displayFormat({ year: 'numeric' })
const formatWeekday = displayFormat({ weekday: 'short' })

/** "2026年10月" / "October 2026" */
export const formatMonthTitle = (year: number, month: number) => formatYearMonth(ymd(year, month, 1))

/** "10月" / "Oct": a month button in the month picker. */
export const formatMonthShort = (month: number) => formatMonth(ymd(2026, month, 1))

/** "2026年" / "2026" */
export const formatYear = (year: number) => formatYearOnly(ymd(year, 1, 1))

/** Weekday column headers, Monday first: "月"… / "Mon"… */
export function weekdayHeaders(): string[] {
  // 2024-01-01 was a Monday.
  return Array.from({ length: 7 }, (_, i) => formatWeekday(addDays('2024-01-01', i)))
}

export function dayOfMonth(date: Ymd): number {
  return parts(date)[2]
}

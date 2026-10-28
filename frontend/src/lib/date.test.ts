import { describe, expect, it } from 'vitest'
import { setLocale } from '../i18n'
import {
  addDays,
  formatDayTitle,
  formatMonthShort,
  formatMonthTitle,
  formatTime,
  fromTokyoInput,
  monthGrid,
  overlapsDay,
  shiftMonth,
  toTokyoInput,
  todayTokyo,
  weekdayHeaders,
  weekdayMon0,
} from './date'

// These tests must pass whatever the machine's time zone is; `make test` runs
// them with the default zone and again with TZ=UTC.

describe('todayTokyo', () => {
  it('uses Tokyo, not the local zone', () => {
    // 2026-10-23 15:30 UTC is 2026-10-24 00:30 in Tokyo.
    expect(todayTokyo(new Date('2026-10-23T15:30:00Z'))).toBe('2026-10-24')
    expect(todayTokyo(new Date('2026-10-23T14:59:59Z'))).toBe('2026-10-23')
  })
})

describe('date arithmetic', () => {
  it('adds days across month, year and leap boundaries', () => {
    expect(addDays('2026-10-31', 1)).toBe('2026-11-01')
    expect(addDays('2026-12-31', 1)).toBe('2027-01-01')
    expect(addDays('2024-02-28', 1)).toBe('2024-02-29')
    expect(addDays('2025-02-28', 1)).toBe('2025-03-01')
    expect(addDays('2026-01-01', -1)).toBe('2025-12-31')
  })

  it('numbers weekdays from Monday', () => {
    expect(weekdayMon0('2026-10-19')).toBe(0) // Monday
    expect(weekdayMon0('2026-10-25')).toBe(6) // Sunday
  })

  it('shifts months across years', () => {
    expect(shiftMonth(2026, 12, 1)).toEqual({ year: 2027, month: 1 })
    expect(shiftMonth(2026, 1, -1)).toEqual({ year: 2025, month: 12 })
  })
})

describe('monthGrid (Monday first)', () => {
  function check(year: number, month: number, start: string, end: string, rows: number) {
    const g = monthGrid(year, month)
    expect(g.start).toBe(start)
    expect(g.end).toBe(end)
    expect(g.weeks).toHaveLength(rows)
    for (const week of g.weeks) {
      expect(week).toHaveLength(7)
      expect(weekdayMon0(week[0])).toBe(0)
      expect(weekdayMon0(week[6])).toBe(6)
    }
    const flat = g.weeks.flat()
    expect(flat.length).toBeLessThanOrEqual(42)
    expect(flat).toContain(`${year}-${String(month).padStart(2, '0')}-01`)
  }

  it('October 2026 starts on a Thursday', () => {
    check(2026, 10, '2026-09-28', '2026-11-01', 5)
  })
  it('a month starting on Monday has no leading days (June 2026)', () => {
    check(2026, 6, '2026-06-01', '2026-07-05', 5)
  })
  it('a month starting on Sunday needs six rows (March 2026)', () => {
    check(2026, 3, '2026-02-23', '2026-04-05', 6)
  })
  it('February in a common year can fill exactly four rows (Feb 2027)', () => {
    check(2027, 2, '2027-02-01', '2027-02-28', 4)
  })
  it('February in a leap year includes the 29th (Feb 2024)', () => {
    const g = monthGrid(2024, 2)
    expect(g.weeks.flat()).toContain('2024-02-29')
    check(2024, 2, '2024-01-29', '2024-03-03', 5)
  })
})

describe('overlapsDay (same rule as the API)', () => {
  it('shows an overnight event on both days', () => {
    const s = '2026-10-23T23:00:00+09:00'
    const e = '2026-10-24T01:00:00+09:00'
    expect(overlapsDay(s, e, '2026-10-23')).toBe(true)
    expect(overlapsDay(s, e, '2026-10-24')).toBe(true)
    expect(overlapsDay(s, e, '2026-10-25')).toBe(false)
  })
  it('an event ending exactly at midnight is not on the next day', () => {
    expect(overlapsDay('2026-10-23T22:00:00+09:00', '2026-10-24T00:00:00+09:00', '2026-10-24')).toBe(false)
  })
  it('handles timestamps given in UTC', () => {
    // 15:00Z on the 23rd is 00:00 on the 24th in Tokyo.
    expect(overlapsDay('2026-10-23T15:00:00Z', '2026-10-23T16:00:00Z', '2026-10-24')).toBe(true)
    expect(overlapsDay('2026-10-23T15:00:00Z', '2026-10-23T16:00:00Z', '2026-10-23')).toBe(false)
  })
})

describe('datetime-local conversion', () => {
  it('round-trips through Tokyo time', () => {
    expect(toTokyoInput('2026-10-23T00:30:00Z')).toBe('2026-10-23T09:30')
    expect(fromTokyoInput('2026-10-23T09:30')).toBe('2026-10-23T09:30:00+09:00')
    expect(toTokyoInput(fromTokyoInput('2026-12-31T23:59'))).toBe('2026-12-31T23:59')
  })
})

describe('formatting', () => {
  it('formats times, adding the date when on another day', () => {
    expect(formatTime('2026-10-23T23:00:00+09:00', '2026-10-23')).toBe('23:00')
    expect(formatTime('2026-10-24T01:00:00+09:00', '2026-10-23')).toBe('10/24 01:00')
  })
  it('formats day titles with the weekday, per language', () => {
    setLocale('ja')
    expect(formatDayTitle('2026-10-23')).toBe('10月23日(金)')
    expect(formatDayTitle('2026-10-25')).toBe('10月25日(日)')
    setLocale('en')
    expect(formatDayTitle('2026-10-23')).toBe('Fri, Oct 23')
  })
  it('formats months and weekday headers, per language', () => {
    setLocale('ja')
    expect(formatMonthTitle(2026, 10)).toBe('2026年10月')
    expect(weekdayHeaders()).toEqual(['月', '火', '水', '木', '金', '土', '日'])
    setLocale('en')
    expect(formatMonthTitle(2026, 10)).toBe('October 2026')
    expect(formatMonthShort(1)).toBe('Jan')
    expect(weekdayHeaders()[0]).toBe('Mon')
  })
})

// The browser's IANA time zone, which metric series buckets follow (the
// tz/timeZone parameter of series requests), so a day bar is a local day and
// tick labels, formatted in the browser's zone, match the buckets. UTC when
// the browser does not tell.
export function browserTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
}

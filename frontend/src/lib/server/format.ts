/** "3d 4h", "5h 12m", "7m": the two biggest units of a time span. */
export function formatUptime(sec: number): string {
  const d = Math.floor(sec / 86400), h = Math.floor((sec % 86400) / 3600), m = Math.floor((sec % 3600) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

/** bytes per second as "120 KB/s". */
export function formatRate(bps: number): string {
  if (bps < 1024) return `${Math.round(bps)} B/s`
  const units = ['KB/s', 'MB/s', 'GB/s']
  let v = bps / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++ }
  return `${v >= 100 ? v.toFixed(0) : v.toFixed(1)} ${units[i]}`
}

/** percent of a whole, 0 when there is no whole. */
export function pct(part: number, whole: number): number {
  return whole > 0 ? Math.min(100, Math.max(0, Math.round((part / whole) * 100))) : 0
}

/** how worried to be about a usage figure: drives the colour of a bar. */
export function level(p: number): 'ok' | 'warn' | 'bad' {
  return p >= 90 ? 'bad' : p >= 75 ? 'warn' : 'ok'
}

const PROBLEM = /\b(error|fail(ed|ure)?|fatal|crit(ical)?|panic|denied|refused|timeout|timed out)\b/i
const WARNING = /\b(warn(ing)?)\b/i

/** the kind of a log line, to colour it. */
export function logLevel(line: string): 'err' | 'warn' | '' {
  return PROBLEM.test(line) ? 'err' : WARNING.test(line) ? 'warn' : ''
}

/** how worried to be about a certificate: it should be renewed long before the last week. */
export function certLevel(daysLeft: number): 'ok' | 'warn' | 'bad' {
  return daysLeft < 7 ? 'bad' : daysLeft < 14 ? 'warn' : 'ok'
}

/** how old the newest backup is, as a colour: a daily job that missed two nights is a warning, a week is a problem. */
export function backupLevel(ageSec: number): 'ok' | 'warn' | 'bad' {
  return ageSec > 8 * 86400 ? 'bad' : ageSec > 2 * 86400 ? 'warn' : 'ok'
}

/** the part of a path a cleanup or a backup check may be asked about: absolute, no "..", only plain characters. */
export const PATH_RE = /^\/[A-Za-z0-9_./-]{0,200}$/
export const validPath = (p: string) => PATH_RE.test(p) && !p.split('/').includes('..')

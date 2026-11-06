/** Times of a log, between what a person types (the time on the server), RFC 3339 (what the backend takes) and a log line. */

const pad = (n: number) => String(n).padStart(2, '0')

/** "+07:00" for 25200 seconds east of UTC */
export function offsetText(seconds: number): string {
  const a = Math.abs(seconds)
  return (seconds < 0 ? '-' : '+') + pad(Math.floor(a / 3600)) + ':' + pad(Math.floor((a % 3600) / 60))
}

/** a time typed as "2026-11-06T03:12" (a datetime-local box) on a server `offset` seconds east of UTC, as RFC 3339 */
export function fromServerLocal(local: string, offset: number): string | null {
  if (!/^\d{4}-\d\d-\d\dT\d\d:\d\d(:\d\d)?$/.test(local)) return null
  const iso = local.length === 16 ? local + ':00' : local
  const full = iso + offsetText(offset)
  return Number.isNaN(Date.parse(full)) ? null : full
}

/** an RFC 3339 time as the "2026-11-06T03:12" a datetime-local box shows, in the time of a server `offset` seconds east of UTC */
export function toServerLocal(iso: string, offset: number): string {
  const ms = Date.parse(iso)
  return Number.isNaN(ms) ? '' : new Date(ms + offset * 1000).toISOString().slice(0, 16)
}

/** "2026-11-06 03:12:44" of an RFC 3339 time, in the time of the server */
export function serverClockText(iso: string, offset: number): string {
  const l = toServerLocal(iso, offset)
  const ms = Date.parse(iso)
  return l ? l.replace('T', ' ') + ':' + pad(new Date(ms + offset * 1000).getUTCSeconds()) : ''
}

const LINE_TIME = /^(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:?\d\d))\s/

/** when a log line was written, as RFC 3339: journalctl's short-iso (+0700) and the Z times of docker and kubectl; null for a line with no time */
export function lineTime(line: string): string | null {
  const m = LINE_TIME.exec(line)
  if (!m) return null
  const t = m[1].replace(/([+-]\d\d)(\d\d)$/, '$1:$2')
  const ms = Date.parse(t)
  return Number.isNaN(ms) ? null : new Date(ms).toISOString().replace(/\.\d+Z$/, 'Z')
}

/** the range of `minutes` before and after an RFC 3339 time */
export function around(iso: string, minutes: number): {from: string; until: string} {
  const ms = Date.parse(iso)
  const at = (d: number) => new Date(ms + d).toISOString().replace(/\.\d+Z$/, 'Z')
  return {from: at(-minutes * 60000), until: at(minutes * 60000)}
}

/** the lines that match, each with `n` lines before and after it; a hole between two groups is a null */
export function withContext(lines: string[], hit: (l: string) => boolean, n: number): (string | null)[] {
  const keep = new Array<boolean>(lines.length).fill(false)
  lines.forEach((l, i) => { if (hit(l)) for (let j = Math.max(0, i - n); j <= Math.min(lines.length - 1, i + n); j++) keep[j] = true })
  const out: (string | null)[] = []
  lines.forEach((l, i) => {
    if (keep[i]) out.push(l)
    else if (i > 0 && keep[i - 1]) out.push(null)
  })
  while (out.length > 0 && out[out.length - 1] === null) out.pop() // a hole at the end says nothing
  return out
}

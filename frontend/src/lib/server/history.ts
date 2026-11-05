/**
 * What the Overview remembers of a server beyond the half hour it keeps in memory: one point for each minute, for a day, kept in this
 * computer (localStorage) for each host. A point is the average of the readings of its minute. There is nothing to read while the tab is
 * closed, so those minutes are gaps in the chart, not made up.
 */
export interface HistPoint {t: number; n: number; cpu: number; mem: number; disk: number; net: number}
export type HistSample = Pick<HistPoint, 'cpu' | 'mem' | 'disk' | 'net'>
export type Series = {cpu: (number | null)[]; mem: (number | null)[]; disk: (number | null)[]; net: (number | null)[]}

export const MINUTE = 60_000
export const KEEP_MS = 24 * 60 * MINUTE
const key = (hostId: string) => 'vutty.panel.history.' + hostId

export function loadHistory(hostId: string, now = Date.now()): HistPoint[] {
  try {
    const raw = JSON.parse(localStorage.getItem(key(hostId)) || '[]')
    if (!Array.isArray(raw)) return []
    return raw.filter((p): p is HistPoint => p && [p.t, p.n, p.cpu, p.mem, p.disk, p.net].every(Number.isFinite) && p.t > now - KEEP_MS)
  } catch { return [] }
}

export function saveHistory(hostId: string, pts: HistPoint[]) {
  try { localStorage.setItem(key(hostId), JSON.stringify(pts)) } catch { /* storage full or blocked: the charts of this session are still there */ }
}

/** adds a reading to the point of its minute (the average moves), and forgets what is older than a day */
export function addSample(pts: HistPoint[], s: HistSample, now = Date.now()): HistPoint[] {
  const t = Math.floor(now / MINUTE) * MINUTE
  const out = pts.filter(p => p.t > now - KEEP_MS)
  const last = out[out.length - 1]
  if (last && last.t === t) {
    const n = last.n + 1
    const avg = (a: number, b: number) => a + (b - a) / n
    out[out.length - 1] = {t, n, cpu: avg(last.cpu, s.cpu), mem: avg(last.mem, s.mem), disk: avg(last.disk, s.disk), net: avg(last.net, s.net)}
  } else out.push({t, n: 1, ...s})
  return out
}

/** the last `spanMs` cut into `buckets` equal parts, each the average of its points; null where nothing was read */
export function bucketSeries(pts: HistPoint[], spanMs: number, buckets: number, now = Date.now()): Series {
  const from = now - spanMs
  const size = spanMs / buckets
  const sum = Array.from({length: buckets}, () => ({n: 0, cpu: 0, mem: 0, disk: 0, net: 0}))
  for (const p of pts) {
    if (p.t < from || p.t > now) continue
    const i = Math.min(buckets - 1, Math.floor((p.t - from) / size))
    const b = sum[i]
    b.n++; b.cpu += p.cpu; b.mem += p.mem; b.disk += p.disk; b.net += p.net
  }
  const pick = (f: 'cpu' | 'mem' | 'disk' | 'net') => sum.map(b => (b.n ? b[f] / b.n : null))
  return {cpu: pick('cpu'), mem: pick('mem'), disk: pick('disk'), net: pick('net')}
}

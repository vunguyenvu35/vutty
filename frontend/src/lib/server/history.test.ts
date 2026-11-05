import {addSample, bucketSeries, KEEP_MS, loadHistory, MINUTE, saveHistory} from './history'

const s = (cpu: number) => ({cpu, mem: 50, disk: 70, net: 100})

describe('the history of a server', () => {
  it('averages the readings of one minute into one point and starts another for the next minute', () => {
    const t0 = 10 * 60 * MINUTE
    let pts = addSample([], s(10), t0 + 1000)
    pts = addSample(pts, s(30), t0 + 6000)
    expect(pts).toHaveLength(1)
    expect(pts[0]).toMatchObject({t: t0, n: 2, cpu: 20})
    pts = addSample(pts, s(40), t0 + MINUTE + 1)
    expect(pts.map(p => p.t)).toEqual([t0, t0 + MINUTE])
  })
  it('forgets what is older than a day', () => {
    const now = 3 * KEEP_MS
    const pts = addSample(addSample([], s(1), now - KEEP_MS - 5 * MINUTE), s(2), now)
    expect(pts).toHaveLength(1)
    expect(pts[0].cpu).toBe(2)
  })
  it('cuts a span into buckets and leaves a gap where the tab was closed', () => {
    const now = 100 * 60 * MINUTE
    let pts = addSample([], s(10), now - 50 * MINUTE)
    pts = addSample(pts, s(30), now - 10 * MINUTE)
    const r = bucketSeries(pts, 60 * MINUTE, 6, now) // 10-minute buckets
    expect(r.cpu).toEqual([null, 10, null, null, null, 30])
  })
  it('keeps a history for each host and survives a broken store', () => {
    const now = Date.now()
    saveHistory('h1', addSample([], s(5), now))
    expect(loadHistory('h1', now)).toHaveLength(1)
    expect(loadHistory('h2', now)).toEqual([])
    localStorage.setItem('vutty.panel.history.h3', '{not json')
    expect(loadHistory('h3', now)).toEqual([])
  })
})

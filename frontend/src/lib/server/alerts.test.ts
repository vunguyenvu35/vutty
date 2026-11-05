import {alertsOf} from './alerts'
import type {ServerCerts, ServerOverview, ServerUnit} from '../types'

const t = (s: string, v?: Record<string, string | number>) => s.replace(/\{(\w+)\}/g, (_, k) => String(v?.[k] ?? ''))
const o = (used: number) => ({disk_used: used, disk_total: 100} as ServerOverview)
const unit = (name: string, active: string) => ({name, active} as ServerUnit)
const cert = (name: string, days: number) => ({found: true, certs: [{name, domains: [name + '.org'], not_after: 0, days_left: days}]} as ServerCerts)

describe('what is worth an alert', () => {
  it('says nothing while the server is fine', () => {
    expect(alertsOf(t, o(50), [unit('a', 'active')], cert('x', 40))).toEqual([])
    expect(alertsOf(t, null, null, null)).toEqual([])
  })
  it('names a full disk, a failed service and a certificate about to expire, each with a key of its own', () => {
    const a = alertsOf(t, o(93), [unit('api', 'failed'), unit('db', 'active')], cert('shop', 3))
    expect(a.map(x => x.key)).toEqual(['disk', 'unit:api', 'cert:shop'])
    expect(a[0].text).toBe('The disk is 93% full')
    expect(a[2].text).toBe('The certificate of shop.org expires in 3 days')
    expect(alertsOf(t, null, null, cert('shop', -2))[0].text).toBe('The certificate of shop.org has expired')
  })
  it('leaves a warning (a disk at 80%, a certificate with 10 days) to the Overview', () => {
    expect(alertsOf(t, o(80), null, cert('x', 10))).toEqual([])
  })
})

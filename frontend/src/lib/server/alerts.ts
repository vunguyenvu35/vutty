import {certLevel} from './format'
import type {ServerCerts, ServerOverview, ServerUnit} from '../types'

/** a problem worth interrupting for: the disk nearly full, a service that failed, a certificate about to run out or already out */
export interface Alert {key: string; text: string}

export const DISK_ALERT = 90

/** what is wrong on a server now, from the last readings; every key stands for one problem, so a problem is told once */
export function alertsOf(t: (s: string, v?: Record<string, string | number>) => string, o: ServerOverview | null, units: ServerUnit[] | null, certs: ServerCerts | null): Alert[] {
  const out: Alert[] = []
  if (o && o.disk_total > 0) {
    const disk = Math.round((o.disk_used / o.disk_total) * 100)
    if (disk >= DISK_ALERT) out.push({key: 'disk', text: t('The disk is {n}% full', {n: disk})})
  }
  for (const u of units ?? []) if (u.active === 'failed') out.push({key: 'unit:' + u.name, text: t('{name} has failed', {name: u.name})})
  for (const c of certs?.certs ?? []) {
    if (certLevel(c.days_left) !== 'bad') continue
    out.push({key: 'cert:' + c.name, text: c.days_left < 0 ? t('The certificate of {name} has expired', {name: c.domains[0] ?? c.name}) : t('The certificate of {name} expires in {n} days', {name: c.domains[0] ?? c.name, n: c.days_left})})
  }
  return out
}

import {useSyncExternalStore} from 'react'
import type {TFn} from '../i18n'
import type {ServerFleetHost} from '../types'
import {pct} from './format'

/**
 * What needs a look across the fleet: a server that does not answer, a disk or memory over its limit, a failed service, a certificate
 * close to its end. Every answer of every check goes through reportFleet; the alerts that are new since the last answer of that host
 * are handed to the notifier (a desktop notification), once each, until they clear.
 */
export type AlertKind = 'down' | 'disk' | 'mem' | 'service' | 'cert'

export interface FleetAlert {
  key: string
  hostId: string
  label: string
  kind: AlertKind
  /** the percentage, the number of failed services, or the days left */
  value: number
}

export interface AlertConfig {
  /** show a desktop notification for a new alert, and keep checking while the Fleet page is not shown */
  notify: boolean
  disk: number
  mem: number
  certDays: number
}

export const DEFAULT_ALERT_CONFIG: AlertConfig = {notify: false, disk: 90, mem: 95, certDays: 14}
const STORE_KEY = 'vutty.fleet.alerts'
/** a host must fail twice in a row to count as down: one lost packet is no alarm */
const DOWN_AFTER = 2

type Notifier = (label: string, alerts: FleetAlert[]) => void

let config: AlertConfig = load()
let alerts: FleetAlert[] = []
let byHost = new Map<string, FleetAlert[]>()
const fails = new Map<string, number>()
let notifier: Notifier | null = null
const listeners = new Set<() => void>()

function load(): AlertConfig {
  try {
    const raw = JSON.parse(localStorage.getItem(STORE_KEY) ?? '{}') as Partial<AlertConfig>
    const n = (v: unknown, min: number, max: number, d: number) => (typeof v === 'number' && Number.isFinite(v) ? Math.min(max, Math.max(min, Math.round(v))) : d)
    return {notify: raw.notify === true, disk: n(raw.disk, 1, 100, DEFAULT_ALERT_CONFIG.disk), mem: n(raw.mem, 1, 100, DEFAULT_ALERT_CONFIG.mem), certDays: n(raw.certDays, 1, 90, DEFAULT_ALERT_CONFIG.certDays)}
  } catch { return {...DEFAULT_ALERT_CONFIG} }
}

const emit = () => listeners.forEach(l => l())
const subscribe = (l: () => void) => { listeners.add(l); return () => { listeners.delete(l) } }
const flatten = () => { alerts = [...byHost.values()].flat() }

/** The alerts one answer of a host gives. */
export function evaluate(cfg: AlertConfig, host: {id: string; label: string}, r: ServerFleetHost): FleetAlert[] {
  const mk = (kind: AlertKind, value: number): FleetAlert => ({key: host.id + ':' + kind, hostId: host.id, label: host.label, kind, value})
  const out: FleetAlert[] = []
  const o = r.overview
  const disk = pct(o.disk_used, o.disk_total)
  const mem = pct(o.mem_used, o.mem_total)
  if (disk >= cfg.disk) out.push(mk('disk', disk))
  if (mem >= cfg.mem) out.push(mk('mem', mem))
  if (r.failed_units > 0) out.push(mk('service', r.failed_units))
  if (typeof r.cert_days === 'number' && r.cert_days < cfg.certDays) out.push(mk('cert', r.cert_days))
  return out
}

/** One answer of a host (down or up). */
export function reportFleet(host: {id: string; label: string}, r: ServerFleetHost) {
  let next: FleetAlert[]
  if (r.error) {
    const n = (fails.get(host.id) ?? 0) + 1
    fails.set(host.id, n)
    // below the limit nothing changes: what was known about the host stays
    if (n < DOWN_AFTER) return
    next = [{key: host.id + ':down', hostId: host.id, label: host.label, kind: 'down', value: 0}]
  } else {
    fails.set(host.id, 0)
    next = evaluate(config, host, r)
  }
  const before = new Set((byHost.get(host.id) ?? []).map(a => a.key))
  byHost.set(host.id, next)
  flatten()
  const fresh = next.filter(a => !before.has(a.key))
  if (fresh.length > 0 && config.notify && notifier) notifier(host.label, fresh)
  emit()
}

/** Hosts that are not saved any more have no alerts. */
export function keepFleetHosts(ids: string[]) {
  const keep = new Set(ids)
  let changed = false
  for (const id of [...byHost.keys()]) if (!keep.has(id)) { byHost.delete(id); fails.delete(id); changed = true }
  if (changed) { flatten(); emit() }
}

export const setFleetNotifier = (n: Notifier | null) => { notifier = n }
export const fleetAlertConfig = () => config

export function setFleetAlertConfig(next: AlertConfig) {
  config = next
  try { localStorage.setItem(STORE_KEY, JSON.stringify(next)) } catch { /* not remembered */ }
  // the limits changed: what was found stays until the host answers again
  emit()
}

/** For tests: forget everything and read the stored settings again. */
export function resetFleetAlerts() {
  byHost = new Map(); fails.clear(); alerts = []; notifier = null
  config = load()
  emit()
}

export const useFleetAlerts = (): FleetAlert[] => useSyncExternalStore(subscribe, () => alerts)
export const useFleetAlertConfig = (): AlertConfig => useSyncExternalStore(subscribe, () => config)

/** The words of one alert. */
export function alertText(t: TFn, a: FleetAlert): string {
  switch (a.kind) {
    case 'down': return t('does not answer')
    case 'disk': return t('disk {n}% full', {n: a.value})
    case 'mem': return t('memory {n}% used', {n: a.value})
    case 'service': return t('failed services: {n}', {n: a.value})
    case 'cert': return a.value < 0 ? t('a certificate has expired') : t('a certificate ends in {n} days', {n: a.value})
  }
}

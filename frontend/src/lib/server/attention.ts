import {useCallback} from 'react'
import {useBackend} from '../backend'
import {useT, useTn} from '../i18n'
import type {Host, ServerBackup, ServerCerts, ServerOverview, ServerUnit, ServerUpdates} from '../types'
import {backupLevel, certLevel, formatUptime, pct} from './format'
import type {Section} from './sections'
import {useLoad} from './shared'

export interface Attention {key: string; tone: 'bad' | 'warn'; text: string; go: Section}

/**
 * What may need attention on the server, from what the overview says and a few things asked once (services, certificates,
 * updates, the backup folder); a login that cannot ask leaves that part out. The Overview lists it, and the section list marks
 * the section each item belongs to.
 */
export function useAttention(sid: string, host: Host, o: ServerOverview | null) {
  const be = useBackend()
  const t = useT()
  const tn = useTn()
  const {data: units, reload: r1} = useLoad<ServerUnit[]>(useCallback(() => be.server.services(sid), [be, sid]), null)
  const {data: certs, reload: r2} = useLoad<ServerCerts>(useCallback(() => be.server.certificates(sid), [be, sid]), null)
  const {data: updates, reload: r3} = useLoad<ServerUpdates>(useCallback(() => be.server.updates(sid), [be, sid]), null)
  const backupPath = host.backup_path ?? ''
  const {data: backup, reload: r4} = useLoad<ServerBackup | null>(useCallback(() => (backupPath ? be.server.backup(sid, backupPath) : Promise.resolve(null)), [be, sid, backupPath]), null)
  const reload = () => { r1(); r2(); r3(); r4() }
  const list: Attention[] = []
  if (o) {
    const ram = pct(o.mem_used, o.mem_total), disk = pct(o.disk_used, o.disk_total)
    for (const u of (units ?? []).filter(u => u.active === 'failed')) list.push({key: 'u' + u.name, tone: 'bad', text: t('{name} has failed', {name: u.name}), go: 'services'})
    if (disk >= 75) list.push({key: 'disk', tone: disk >= 90 ? 'bad' : 'warn', text: t('The disk is {n}% full', {n: disk}), go: 'maintenance'})
    if (ram >= 90) list.push({key: 'ram', tone: 'warn', text: t('Memory is {n}% used', {n: ram}), go: 'processes'})
    for (const c of (certs?.certs ?? []).filter(c => certLevel(c.days_left) !== 'ok')) {
      list.push({key: 'c' + c.name, tone: certLevel(c.days_left) as 'bad' | 'warn', text: c.days_left < 0 ? t('The certificate for {name} has expired', {name: c.domains[0] ?? c.name}) : tn(c.days_left, 'The certificate for {name} expires in {n} day', 'The certificate for {name} expires in {n} days', {name: c.domains[0] ?? c.name}), go: 'web'})
    }
    if (updates && updates.security > 0) list.push({key: 'sec', tone: 'warn', text: tn(updates.security, '{n} security update is waiting', '{n} security updates are waiting', {}), go: 'maintenance'})
    if (updates?.reboot_required) list.push({key: 'reboot', tone: 'warn', text: t('The server needs a reboot'), go: 'maintenance'})
    if (backup && (!backup.found || backupLevel(backup.now - backup.mtime) !== 'ok')) {
      list.push({key: 'backup', tone: !backup.found || backupLevel(backup.now - backup.mtime) === 'bad' ? 'bad' : 'warn', text: backup.found ? t('The newest backup is {time} old', {time: formatUptime(backup.now - backup.mtime)}) : t('No backup was found in {path}', {path: backupPath}), go: 'schedule'})
    }
  }
  /** per section: how many items and the worst tone */
  const marks: Partial<Record<Section, {n: number; tone: 'bad' | 'warn'}>> = {}
  for (const a of list) {
    const m = marks[a.go]
    marks[a.go] = {n: (m?.n ?? 0) + 1, tone: m?.tone === 'bad' || a.tone === 'bad' ? 'bad' : 'warn'}
  }
  return {list, marks, loaded: units !== null, reload}
}
export type AttentionState = ReturnType<typeof useAttention>

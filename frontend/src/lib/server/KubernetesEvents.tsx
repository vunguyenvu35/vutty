import {useCallback, useState} from 'react'
import {useT} from '../i18n'
import {Err, Loading, Toolbar} from './shared'
import {age, matches, Pill, useK8s, useKept, useKLoad, useSort} from './KubernetesShared'
import type {K8sEvent} from '../types'

type EventSort = 'age' | 'reason' | 'object' | 'count'
const HOUR = 3600

/** Screen 17: what happened in the last hour, newest first; Warnings on, Normal off to start with */
export function Events() {
  const k = useK8s()
  const t = useT()
  const [warnings, setWarnings] = useKept('events.warnings', true)
  const [normal, setNormal] = useKept('events.normal', false)
  const [filter, setFilter] = useKept('events.filter', '')
  const load = useCallback(() => k.be.server.k8sEvents(k.sid, k.ctx, k.ns), [k.be, k.sid, k.ctx, k.ns])
  const {data, error, busy, reload} = useKLoad<K8sEvent[]>(load, false)
  const {th, apply} = useSort<EventSort>(['reason', 'object'])
  const recent = [...(data ?? [])].filter(e => e.age_sec <= HOUR && ((warnings && e.type === 'Warning') || (normal && e.type === 'Normal')) && matches(filter, e.reason, e.object, e.message, e.namespace)).sort((a, b) => a.age_sec - b.age_sec)
  const rows = apply(recent, (e, key) => key === 'age' ? e.age_sec : key === 'reason' ? e.reason : key === 'object' ? e.object : e.count)
  if (!data && !error) return <div className="sp-view"><Loading kind="table" /></div>
  return (
    <div className="sp-view">
      <Toolbar busy={busy} onReload={reload}>
        <input className="sp-filter" aria-label={t('Filter events')} placeholder={t('Filter events')} value={filter} onChange={e => setFilter(e.target.value)} />
        <button aria-pressed={warnings} className={warnings ? 'primary' : ''} onClick={() => setWarnings(!warnings)}>{t('Warnings')}</button>
        <button aria-pressed={normal} className={normal ? 'primary' : ''} onClick={() => setNormal(!normal)}>{t('Normal')}</button>
        <span className="sp-note">{t('Newest first, last hour')}</span>
      </Toolbar>
      <Err error={error} />
      <div className="sp-table"><table>
        <thead><tr>{th('age', t('Last seen'), 'n')}{th('reason', t('Reason'))}{th('object', t('Object'))}<th>{t('Message')}</th>{th('count', t('Count'), 'n')}</tr></thead>
        <tbody>{rows.map((e, i) => (
          <tr key={i}>
            <td className="n">{age(e.age_sec)}</td>
            <td><Pill tone={e.type === 'Warning' ? (/BackOff|NotReady|Failed(?!Sched)|Evicted|OOM/.test(e.reason) ? 'bad' : 'warn') : ''}>{e.reason}</Pill></td>
            <td className="k8s-name"><b>{e.object}</b><small>{e.namespace || t('cluster')}</small></td>
            <td className="k8s-msg">{e.message}</td><td className="n m">{e.count}</td>
          </tr>))}</tbody>
      </table>{data && rows.length === 0 && <div className="sp-empty">{t('No events.')}</div>}</div>
    </div>
  )
}
